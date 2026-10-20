package privatevault

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const storedPrefix = "vault:v1:"

func (v *Vault) SealStored(ctx context.Context, user int64, kind, id, text string) (string, error) {
	if text == "" {
		return "", nil
	}
	value, e := v.EncryptText(ctx, user, kind, id, text)
	if e != nil {
		return "", e
	}
	return storedPrefix + value, nil
}
func (v *Vault) OpenStored(ctx context.Context, user int64, kind, id, text string) (string, error) {
	if !strings.HasPrefix(text, storedPrefix) {
		return text, nil
	} // bounded compatibility while migrating existing rows
	return v.DecryptText(ctx, user, kind, id, strings.TrimPrefix(text, storedPrefix))
}

var MeetingTextColumns = []string{"transcript", "summary", "action_items", "segments_json", "vocabulary_audit", "speaker_names"}

// Every field has a distinct authenticated context, preventing row/column/account substitution.
func (v *Vault) MeetingFields(ctx context.Context, user, id int64, values map[string]string, seal bool) error {
	for col, value := range values {
		var e error
		if seal {
			value, e = v.SealStored(ctx, user, "meeting-"+col, strconv.FormatInt(id, 10), value)
		} else {
			value, e = v.OpenStored(ctx, user, "meeting-"+col, strconv.FormatInt(id, 10), value)
		}
		if e != nil {
			return e
		}
		values[col] = value
	}
	return nil
}
func (v *Vault) MigrateMeetingText(ctx context.Context) error {
	for _, table := range []string{"meetings", "meeting_jobs"} {
		cols := MeetingTextColumns
		identity := "id"
		kind := "meeting-"
		if table == "meeting_jobs" {
			cols = []string{"mic_result", "system_result", "diar_result"}
			identity = "job_key"
			kind = "job-"
		}
		var exists int
		if e := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			continue
		}
		for _, col := range cols {
			rows, e := v.db.QueryContext(ctx, "SELECT "+identity+",user_id,"+col+" FROM "+table+" WHERE COALESCE("+col+",'')<>'' AND "+col+" NOT LIKE 'vault:v1:%'")
			if e != nil {
				return e
			}
			type row struct {
				id   string
				user int64
				text string
			}
			var batch []row
			for rows.Next() {
				var r row
				if e = rows.Scan(&r.id, &r.user, &r.text); e != nil {
					rows.Close()
					return e
				}
				batch = append(batch, r)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, r := range batch {
				sealed, e := v.SealStored(ctx, r.user, kind+col, r.id, r.text)
				if e != nil {
					return e
				}
				if _, e = v.db.ExecContext(ctx, "UPDATE "+table+" SET "+col+"=? WHERE "+identity+"=? AND user_id=? AND "+col+"=?", sealed, r.id, r.user, r.text); e != nil {
					return fmt.Errorf("meeting encryption migration failed")
				}
			}
		}
	}
	// Completed intermediates no longer serve recovery and need not be retained.
	var exists int
	if e := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='meeting_jobs'").Scan(&exists); e != nil {
		return e
	}
	if exists == 0 {
		return nil
	}
	_, e := v.db.ExecContext(ctx, `UPDATE meeting_jobs SET mic_result='',system_result='',diar_result='' WHERE status='completed'`)
	return e
}
