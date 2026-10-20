package privatevault

import "context"

// BeginAccountDeletion durably prevents new encrypted writes before deleting
// remote objects. A failed remote deletion leaves the key/metadata for retry.
func (v *Vault) BeginAccountDeletion(ctx context.Context, user int64) error {
	if _, e := v.db.ExecContext(ctx, `INSERT INTO private_account_deletions(user_id) VALUES(?) ON CONFLICT DO NOTHING`, user); e != nil {
		return ErrUnavailable
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
