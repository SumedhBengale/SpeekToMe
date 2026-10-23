package privatevault

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
)

// SpoolKey derives the key that seals one meeting job's spooled audio. It is
// bound to the account key, so deleting the account key also shreds any spool
// chunks that a failed cleanup left behind.
func (v *Vault) SpoolKey(ctx context.Context, user int64, job string) ([]byte, error) {
	accountKey, err := v.UserKey(ctx, user)
	if err != nil {
		return nil, err
	}
	defer clear(accountKey)
	mac := hmac.New(sha256.New, accountKey)
	mac.Write([]byte("speektome-meeting-spool-v1:" + job))
	return mac.Sum(nil), nil
}
