package privatevault

import (
	"context"
	"encoding/json"
	"io"
)

// Audit emits counts only, never identifiers, ciphertexts, keys or user content.
func (v *Vault) Audit(ctx context.Context, w io.Writer) error {
	counts := map[string]int64{}
	for _, q := range [][2]string{
		{"plaintext_usage_rows", `SELECT COUNT(*) FROM usage_logs WHERE COALESCE(input_transcript,'')<>'' OR COALESCE(final_output,'')<>'' OR COALESCE(vocabulary_audit,'')<>''`},
		{"orphan_usage_rows", `SELECT COUNT(*) FROM usage_logs l LEFT JOIN users u ON u.id=l.user_id WHERE u.id IS NULL`},
		{"encrypted_legacy_results", `SELECT COUNT(*) FROM encrypted_legacy_results`},
		{"account_keys", `SELECT COUNT(*) FROM user_encryption_keys`},
		{"final_history_rows", `SELECT COUNT(*) FROM transcription_history`},
		{"ready_audio_objects", `SELECT COUNT(*) FROM meeting_audio_assets WHERE state='ready'`},
		{"pending_audio_objects", `SELECT COUNT(*) FROM meeting_audio_assets WHERE state<>'ready'`},
		{"active_meeting_jobs", `SELECT COUNT(*) FROM meeting_jobs WHERE admitted=1`},
	} {
		var n int64
		if e := v.db.QueryRowContext(ctx, q[1]).Scan(&n); e != nil {
			return ErrUnavailable
		}
		counts[q[0]] = n
	}
	return json.NewEncoder(w).Encode(counts)
}
