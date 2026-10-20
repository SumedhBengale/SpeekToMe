package privatevault

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/minio/sio"
	"io"
)

// CheckObjects verifies real encrypted PUT, range GET and DELETE using random
// synthetic bytes only. It never reads user recordings or emits key material.
func (v *Vault) CheckObjects(ctx context.Context) error {
	if v.Objects == nil {
		return ErrUnavailable
	}
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return ErrUnavailable
	}
	defer clear(key)
	plain := make([]byte, 200123)
	if _, e := rand.Read(plain); e != nil {
		return ErrUnavailable
	}
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return ErrUnavailable
	}
	name := "checks/v1/" + hex.EncodeToString(id)
	encrypted, e := sio.EncryptReader(bytes.NewReader(plain), streamConfig(key))
	if e != nil {
		return ErrUnavailable
	}
	if e = v.Objects.Put(ctx, name, encrypted); e != nil {
		_ = v.Objects.Delete(ctx, name)
		return ErrUnavailable
	}
	reader, e := sio.DecryptReaderAt(objectReader{ctx, v.Objects, name}, streamConfig(key))
	if e == nil {
		for _, offset := range []int64{0, 65530, 199900} {
			buf := make([]byte, 128)
			_, e = reader.ReadAt(buf, offset)
			if e != nil || !bytes.Equal(buf, plain[offset:offset+128]) {
				e = ErrUnavailable
				break
			}
		}
	}
	if e == nil {
		all, readErr := io.ReadAll(io.NewSectionReader(reader, 0, int64(len(plain))))
		if readErr != nil || !bytes.Equal(all, plain) {
			e = ErrUnavailable
		}
	}
	deletion := v.Objects.Delete(ctx, name)
	if e != nil || deletion != nil {
		return ErrUnavailable
	}
	return nil
}
