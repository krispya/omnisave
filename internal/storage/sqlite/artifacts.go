package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// Artifact content rests in the save store, which verifies every hash it writes
// and reads. The artifacts table beside it is an index of logical sizes: the
// store holds content compressed, so a size that the API answers with on every
// request would otherwise cost a decompression to learn. The index is a
// convenience and never an authority — the manifests in the store carry the
// same sizes, and reconciling rebuilds this table from them.

func (r *Repository) storeArtifact(artifact storage.Artifact, payload io.Reader) error {
	if !store.ValidHash(artifact.SHA256) || artifact.Size < 0 {
		return fmt.Errorf("%w: invalid descriptor", storage.ErrArtifactMismatch)
	}
	counter := &countingReader{reader: payload}
	stored, err := r.store.PutObject(artifact.SHA256, counter)
	if err != nil {
		if errors.Is(err, store.ErrContentMismatch) {
			return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
		}
		return err
	}

	// The size the caller declared is never taken on faith. A matching hash
	// proves the content, but the size is what the API answers a HEAD with
	// without reading anything, so it has to be measured or confirmed against
	// content already held. Deduplicated uploads are exactly where a wrong
	// size would otherwise slip in: nothing was read, so there is nothing to
	// compare the claim against except the store.
	measured := counter.count
	if !stored {
		if measured, err = r.contentSize(artifact.SHA256); err != nil {
			return err
		}
	}
	if measured != artifact.Size {
		return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
	}
	return r.recordArtifact(artifact.SHA256, measured)
}

func (r *Repository) recordArtifact(hash string, size int64) error {
	_, err := r.db.Exec(`INSERT OR REPLACE INTO artifacts(sha256, size) VALUES (?, ?)`, hash, size)
	return err
}

func (r *Repository) openArtifact(hash string) (io.ReadCloser, error) {
	reader, err := r.store.OpenObject(hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, storage.ErrNotFound
	}
	return reader, err
}

func (r *Repository) statArtifact(hash string) (int64, error) {
	size, err := r.contentSize(hash)
	if err != nil {
		return 0, err
	}
	return size, nil
}

// contentSize is the true uncompressed size of content the store holds: the
// indexed size when there is one, and otherwise the object's own, read and
// then written back so it costs once. A store restored without its database
// has no index rows at all, which is the case that makes this a fallback
// rather than an error.
func (r *Repository) contentSize(hash string) (int64, error) {
	var size int64
	err := r.db.QueryRow(`SELECT size FROM artifacts WHERE sha256 = ?`, hash).Scan(&size)
	if err == nil {
		return size, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	size, err = r.store.VerifyObject(hash)
	if errors.Is(err, store.ErrNotFound) {
		return 0, storage.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if err := r.recordArtifact(hash, size); err != nil {
		return 0, err
	}
	return size, nil
}

// removeArtifact deletes unreferenced content and forgets its size. An object
// that cannot be removed is logged and left: it leaks disk rather than data,
// and a later upload of the same content finds it and re-records it.
func (r *Repository) removeArtifact(hash string) error {
	r.noteStoreLag("removal of object "+hash, r.store.RemoveObject(hash))
	_, err := r.db.Exec(`DELETE FROM artifacts WHERE sha256 = ?`, hash)
	return err
}

// countingReader measures a payload as it streams past, so the declared size
// can be checked without buffering the content or reading it twice.
type countingReader struct {
	reader io.Reader
	count  int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.count += int64(n)
	return n, err
}
