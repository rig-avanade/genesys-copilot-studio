package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/copilot"
)

func Key(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type Store interface {
	WithSession(context.Context, string, func(context.Context, *copilot.State, func() error) error) error
}

type BlobStore struct {
	Blobs *blob.Client
}

func (s *BlobStore) WithSession(ctx context.Context, key string, fn func(context.Context, *copilot.State, func() error) error) error {
	name := "sessions/" + key + ".json"
	return s.Blobs.WithLease(ctx, name, func(ctx context.Context, lease *blob.Lease) error {
		data, err := s.Blobs.Get(ctx, name)
		if err != nil {
			return err
		}
		var state copilot.State
		if json.Unmarshal(data, &state) != nil {
			return errors.New("storage: corrupt conversation state")
		}
		return fn(ctx, &state, func() error {
			data, err := json.Marshal(state)
			if err != nil {
				return errors.New("cannot serialize conversation state")
			}
			return lease.Put(ctx, data)
		})
	})
}
