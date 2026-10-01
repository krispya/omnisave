// Package artifact defines content-addressed immutable bytes: the descriptor
// that names them and the store that keeps them. Save history (revision
// files) and the Library (game media) both reference artifacts, so neither
// domain owns this vocabulary.
package artifact

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrNotFound reports bytes the store does not hold.
	ErrNotFound = errors.New("artifact: not found")
	// ErrMismatch reports a malformed descriptor or bytes that disagree with it.
	ErrMismatch = errors.New("artifact: content does not match descriptor")
	// ErrUnavailable reports content that could not be proven available at the
	// moment something tried to reference it.
	ErrUnavailable = errors.New("artifact: unavailable")
)

// Artifact describes immutable bytes by content: SHA256 is their identity, so
// identical content is stored once and never rewritten.
type Artifact struct {
	Format string `json:"format"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Unavailable names the artifacts a reference required but could not prove
// available in the same transaction that attempted to reference them.
type Unavailable struct {
	SHA256 []string
}

func (e *Unavailable) Error() string { return ErrUnavailable.Error() }
func (e *Unavailable) Unwrap() error { return ErrUnavailable }

// Store keeps artifacts. StoreArtifact verifies the payload against the
// descriptor (ErrMismatch otherwise); Open and Stat answer ErrNotFound for
// bytes the store does not hold.
type Store interface {
	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
}
