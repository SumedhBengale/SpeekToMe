package privatevault

import "context"

// BeginAccountDeletion durably prevents new encrypted writes before deleting
// remote objects. A failed remote deletion leaves the key/metadata for retry.
func (v *Vault) BeginAccountDeletion(ctx context.Context, user int64) error {
	if _, e := v.db.ExecContext(ctx, `INSERT INTO private_account_deletions(user_id) VALUES(?) ON CONFLICT DO NOTHING`, user); e != nil {
		return ErrUnavailable
	}
	var exists int
	if e := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='meeting_jobs'").Scan(&exists); e != nil {
		return ErrUnavailable
	}
	if exists > 0 {
		if _, e := v.db.ExecContext(ctx, "UPDATE meeting_jobs SET status='cancelled',admitted=0 WHERE user_id=?", user); e != nil {
			return ErrUnavailable
		}
		jobs, e := v.db.QueryContext(ctx, "SELECT job_key FROM meeting_jobs WHERE user_id=?", user)
		if e != nil {
			return ErrUnavailable
		}
		var keys []string
		for jobs.Next() {
			var key string
			if e = jobs.Scan(&key); e != nil {
				jobs.Close()
				return ErrUnavailable
			}
			keys = append(keys, key)
		}
		e = jobs.Err()
		jobs.Close()
		if e != nil {
			return ErrUnavailable
		}
		for _, key := range keys {
			if e = v.purgeProcessing(key); e != nil {
				return e
			}
		}
	}
	rows, e := v.db.QueryContext(ctx, `SELECT meeting_id FROM meeting_audio_assets WHERE user_id=?`, user)
	if e != nil {
		return ErrUnavailable
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return ErrUnavailable
	}
	for _, id := range ids {
		if e = v.DeleteAudio(ctx, user, id, false); e != nil {
			return e
		}
	}
	return nil
}
