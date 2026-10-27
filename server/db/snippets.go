package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"speektome-server/snippets"
)

// Snippets are saved text blocks a speaker inserts by saying their name. Names
// are stored in plain text, like vocabulary, so the account's names can be
// kept unique; the content often holds personal details such as an address,
// so it is sealed under the account key.

const (
	FreeSnippetLimit = 50
	ProSnippetLimit  = 1000
)

var (
	ErrSnippetLimit     = errors.New("snippet limit reached")
	ErrSnippetNameTaken = errors.New("snippet name already used")
	ErrSnippetNotFound  = errors.New("snippet not found")
)

const snippetContentKind = "snippet-content"

func (u *UserProfile) SnippetLimit() int {
	if u.PlanTier == "pro" && u.PlanActive == 1 {
		return ProSnippetLimit
	}
	return FreeSnippetLimit
}

// SnippetRecord is one snippet as the account manages it. Active is false for
// snippets beyond the current plan's limit after a downgrade: they are kept,
// but dictation does not insert them.
type SnippetRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// snippetLimitSQL is the account's current limit, including the subscription
// grace period, as one SQL expression taking (grace cutoff, user id).
var snippetLimitSQL = fmt.Sprintf(`(SELECT CASE WHEN plan_tier='pro' AND plan_active=1 AND (COALESCE(razorpay_current_end,0)<=0 OR razorpay_current_end>?) THEN %d ELSE %d END FROM users WHERE id=?)`, ProSnippetLimit, FreeSnippetLimit)

func graceCutoff() int64 { return time.Now().Add(-SubscriptionGracePeriod).Unix() }

func (d *Database) sealSnippet(ctx context.Context, user int64, id, content string) (string, error) {
	if d.Privacy == nil {
		return content, nil
	}
	return d.Privacy.SealStored(ctx, user, snippetContentKind, id, content)
}

// ListSnippets returns every saved snippet with its content, oldest first, and
// the account's current limit.
func (d *Database) ListSnippets(ctx context.Context, user int64) ([]SnippetRecord, int, error) {
	var limit int
	if err := d.db.QueryRowContext(ctx, `SELECT `+snippetLimitSQL, graceCutoff(), user).Scan(&limit); err != nil {
		return nil, 0, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT public_id,name,content,created_at,updated_at FROM snippets WHERE user_id=? ORDER BY id`, user)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := []SnippetRecord{}
	sealed := map[string]string{}
	for rows.Next() {
		var s SnippetRecord
		var content string
		if err := rows.Scan(&s.ID, &s.Name, &content, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, err
		}
		s.Active = len(list) < limit
		sealed[s.ID] = content
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if d.Privacy != nil && len(sealed) > 0 {
		if sealed, err = d.Privacy.OpenStoredAll(ctx, user, snippetContentKind, sealed); err != nil {
			return nil, 0, err
		}
	}
	for i := range list {
		list[i].Content = sealed[list[i].ID]
	}
	return list, limit, nil
}

// ActiveSnippets returns the names dictation listens for: the oldest snippets
// up to the plan's limit. Content stays sealed until a snippet is inserted.
func (d *Database) ActiveSnippets(ctx context.Context, user int64) ([]snippets.Snippet, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT public_id,name FROM snippets WHERE user_id=? ORDER BY id LIMIT `+snippetLimitSQL, user, graceCutoff(), user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []snippets.Snippet
	for rows.Next() {
		var s snippets.Snippet
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// SnippetContents opens the content of the given snippets. Snippets deleted
// since the dictation started are missing from the result.
func (d *Database) SnippetContents(ctx context.Context, user int64, ids []string) (map[string]string, error) {
	sealed := map[string]string{}
	for _, id := range ids {
		if _, done := sealed[id]; done {
			continue
		}
		var content string
		err := d.db.QueryRowContext(ctx, `SELECT content FROM snippets WHERE user_id=? AND public_id=?`, user, id).Scan(&content)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sealed[id] = content
	}
	if d.Privacy == nil || len(sealed) == 0 {
		return sealed, nil
	}
	return d.Privacy.OpenStoredAll(ctx, user, snippetContentKind, sealed)
}

// CreateSnippet saves a new snippet. One conditional write enforces the plan's
// limit, so competing requests cannot exceed it.
func (d *Database) CreateSnippet(ctx context.Context, user int64, name, content string) (SnippetRecord, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return SnippetRecord{}, err
	}
	id := hex.EncodeToString(raw)
	sealed, err := d.sealSnippet(ctx, user, id, content)
	if err != nil {
		return SnippetRecord{}, err
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO snippets(public_id,user_id,name,normalized_name,content)
 SELECT ?,?,?,?,? WHERE (SELECT COUNT(*) FROM snippets WHERE user_id=?) < `+snippetLimitSQL+`
 ON CONFLICT(user_id,normalized_name) DO NOTHING`,
		id, user, name, snippets.Normalize(name), sealed, user, graceCutoff(), user)
	if err != nil {
		return SnippetRecord{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if d.snippetNameTaken(ctx, user, name, "") {
			return SnippetRecord{}, ErrSnippetNameTaken
		}
		return SnippetRecord{}, ErrSnippetLimit
	}
	return d.snippet(ctx, user, id, content)
}

// UpdateSnippet renames a snippet or changes its content. It works whatever
// the plan, so a downgraded account can still edit what it saved.
func (d *Database) UpdateSnippet(ctx context.Context, user int64, id, name, content string) (SnippetRecord, error) {
	if d.snippetNameTaken(ctx, user, name, id) {
		return SnippetRecord{}, ErrSnippetNameTaken
	}
	sealed, err := d.sealSnippet(ctx, user, id, content)
	if err != nil {
		return SnippetRecord{}, err
	}
	res, err := d.db.ExecContext(ctx, `UPDATE snippets SET name=?,normalized_name=?,content=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND public_id=?`, name, snippets.Normalize(name), sealed, user, id)
	if err != nil {
		if d.snippetNameTaken(ctx, user, name, id) {
			return SnippetRecord{}, ErrSnippetNameTaken
		}
		return SnippetRecord{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return SnippetRecord{}, ErrSnippetNotFound
	}
	return d.snippet(ctx, user, id, content)
}

func (d *Database) DeleteSnippet(ctx context.Context, user int64, id string) error {
	res, err := d.db.ExecContext(ctx, `DELETE FROM snippets WHERE user_id=? AND public_id=?`, user, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSnippetNotFound
	}
	return nil
}

// snippetNameTaken reports whether another of the account's snippets, other
// than except, has the same normalized name.
func (d *Database) snippetNameTaken(ctx context.Context, user int64, name, except string) bool {
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets WHERE user_id=? AND normalized_name=? AND public_id<>?`, user, snippets.Normalize(name), except).Scan(&n)
	return err == nil && n > 0
}

func (d *Database) snippet(ctx context.Context, user int64, id, content string) (SnippetRecord, error) {
	s := SnippetRecord{ID: id, Content: content}
	var position, limit int
	err := d.db.QueryRowContext(ctx, `SELECT name,created_at,updated_at,(SELECT COUNT(*) FROM snippets o WHERE o.user_id=s.user_id AND o.id<s.id),`+snippetLimitSQL+` FROM snippets s WHERE user_id=? AND public_id=?`, graceCutoff(), user, user, id).Scan(&s.Name, &s.CreatedAt, &s.UpdatedAt, &position, &limit)
	s.Active = position < limit
	return s, err
}
