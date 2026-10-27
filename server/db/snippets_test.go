package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"speektome-server/privatevault"
)

func TestSnippetLimitsNamesAndEncryption(t *testing.T) {
	d := vocabularyDB(t)
	ctx := context.Background()
	vault, err := privatevault.New(ctx, d.db, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	d.Privacy = vault
	if _, err = d.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(2,'other@example.test','Other','free',0)`); err != nil {
		t.Fatal(err)
	}

	first, err := d.CreateSnippet(ctx, 1, "My address", "12 Baker Street\nLondon")
	if err != nil || first.Name != "My address" || !first.Active || len(first.ID) != 24 {
		t.Fatal(first, err)
	}
	var stored string
	if err = d.db.QueryRow(`SELECT content FROM snippets WHERE public_id=?`, first.ID).Scan(&stored); err != nil || strings.Contains(stored, "Baker") || !strings.HasPrefix(stored, "vault:v1:") {
		t.Fatal("snippet content stored in plain text", stored, err)
	}
	if _, err = d.CreateSnippet(ctx, 1, "my  ADDRESS!", "other"); !errors.Is(err, ErrSnippetNameTaken) {
		t.Fatal("duplicate name:", err)
	}
	if _, err = d.CreateSnippet(ctx, 2, "my address", "theirs"); err != nil {
		t.Fatal("names are per account:", err)
	}

	// Free accounts stop at 50, even with competing requests.
	for i := 1; i < FreeSnippetLimit-1; i++ {
		if _, err = d.CreateSnippet(ctx, 1, fmt.Sprintf("snippet %d", i), "text"); err != nil {
			t.Fatal(i, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.CreateSnippet(ctx, 1, fmt.Sprintf("race %d", i), "text")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrSnippetLimit) {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("expected one remaining slot, created %d", created)
	}

	// Pro raises the limit; a lapsed subscription keeps the snippets but only
	// the oldest 50 are active.
	if _, err = d.db.Exec(`UPDATE users SET plan_tier='pro',plan_active=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	extra, err := d.CreateSnippet(ctx, 1, "pro snippet", "text")
	if err != nil || !extra.Active {
		t.Fatal(extra, err)
	}
	if _, err = d.db.Exec(`UPDATE users SET razorpay_current_end=? WHERE id=1`, time.Now().Add(-72*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	list, limit, err := d.ListSnippets(ctx, 1)
	if err != nil || limit != FreeSnippetLimit || len(list) != 51 || !list[0].Active || list[50].Active || list[0].Content != "12 Baker Street\nLondon" {
		t.Fatal(limit, len(list), err)
	}
	active, err := d.ActiveSnippets(ctx, 1)
	if err != nil || len(active) != 50 || active[0].Name != "My address" {
		t.Fatal(len(active), err)
	}
	if _, err = d.CreateSnippet(ctx, 1, "too many", "text"); !errors.Is(err, ErrSnippetLimit) {
		t.Fatal(err)
	}

	// Editing works after a downgrade; renames keep names unique.
	updated, err := d.UpdateSnippet(ctx, 1, extra.ID, "Pro sign-off", "Best,\nSam")
	if err != nil || updated.Name != "Pro sign-off" || updated.Content != "Best,\nSam" || updated.Active {
		t.Fatal(updated, err)
	}
	if _, err = d.UpdateSnippet(ctx, 1, extra.ID, "MY ADDRESS", "x"); !errors.Is(err, ErrSnippetNameTaken) {
		t.Fatal(err)
	}
	if _, err = d.UpdateSnippet(ctx, 1, first.ID, "my address", "New address"); err != nil {
		t.Fatal("renaming to its own name:", err)
	}
	if _, err = d.UpdateSnippet(ctx, 2, first.ID, "stolen", "x"); !errors.Is(err, ErrSnippetNotFound) {
		t.Fatal("another account's snippet:", err)
	}
	contents, err := d.SnippetContents(ctx, 1, []string{first.ID, extra.ID, "missing"})
	if err != nil || contents[first.ID] != "New address" || contents[extra.ID] != "Best,\nSam" || len(contents) != 2 {
		t.Fatal(contents, err)
	}
	if err = d.DeleteSnippet(ctx, 2, first.ID); !errors.Is(err, ErrSnippetNotFound) {
		t.Fatal(err)
	}
	if err = d.DeleteSnippet(ctx, 1, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.DeleteUserAccount(1); err != nil {
		t.Fatal(err)
	}
	var left int
	if err = d.db.QueryRow(`SELECT COUNT(*) FROM snippets WHERE user_id=1`).Scan(&left); err != nil || left != 0 {
		t.Fatal("snippets survived account deletion", left, err)
	}
}
