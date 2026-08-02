package store

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PutObject stores content under its SHA-256, compressed. The hash is verified
// against the bytes as they are written and the object only appears at its
// final name once it matches, so an object in the store is always the content
// its name claims — a store that survived a crash mid-write holds no lie.
//
// Storing content the store already holds reads nothing and writes nothing,
// which is what makes a revision that changes one file of twenty cost one
// object rather than twenty. It reports whether the content was read, because
// a caller measuring the payload as it streams past learns nothing about a
// payload that was never drawn on, and must not mistake that for a measurement.
func (s *Store) PutObject(hash string, content io.Reader) (stored bool, err error) {
	if !ValidHash(hash) {
		return false, fmt.Errorf("%w: %q is not a SHA-256", ErrContentMismatch, hash)
	}
	destination := s.objectPath(hash)
	if _, err := os.Stat(destination); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return false, err
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".tmp-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	digest := sha256.New()
	compressor := gzip.NewWriter(temporary)
	_, copyErr := io.Copy(io.MultiWriter(compressor, digest), content)
	compressErr := compressor.Close()
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	for _, err := range []error{copyErr, compressErr, syncErr, closeErr} {
		if err != nil {
			return false, err
		}
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != hash {
		return false, fmt.Errorf("%w: content hashes to %s, not %s", ErrContentMismatch, actual, hash)
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		// Another writer storing identical content wins the race harmlessly:
		// the object it placed is byte-for-byte the one being placed here.
		// The content was still read, so the caller's measurement is valid.
		if _, statErr := os.Stat(destination); statErr == nil {
			return true, syncDirectory(filepath.Dir(destination))
		}
		return false, err
	}
	return true, syncDirectory(filepath.Dir(destination))
}

// OpenObject reads stored content, decompressed.
func (s *Store) OpenObject(hash string) (io.ReadCloser, error) {
	if !ValidHash(hash) {
		return nil, ErrNotFound
	}
	file, err := os.Open(s.objectPath(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("store: read object %s: %w", hash, err)
	}
	return &object{decompressor: decompressor, file: file}, nil
}

// HasObject reports whether content rests in the store.
func (s *Store) HasObject(hash string) bool {
	if !ValidHash(hash) {
		return false
	}
	_, err := os.Stat(s.objectPath(hash))
	return err == nil
}

// RemoveObject deletes content, and reports no error when it is already gone.
// Callers are responsible for knowing nothing references it: the store keeps no
// reference counts, because the manifests that constitute the references are
// the very things a caller must consult to be sure.
func (s *Store) RemoveObject(hash string) error {
	if !ValidHash(hash) {
		return nil
	}
	if err := os.Remove(s.objectPath(hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// VerifyObject re-reads content and confirms it still hashes to its name,
// returning the logical size. This is how bit rot is found on a medium a store
// was copied to, and it is the only way to learn an object's uncompressed size
// without an index.
func (s *Store) VerifyObject(hash string) (int64, error) {
	reader, err := s.OpenObject(hash)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, reader)
	if err != nil {
		return 0, err
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != hash {
		return 0, fmt.Errorf("%w: object %s hashes to %s", ErrContentMismatch, hash, actual)
	}
	return size, nil
}

// EachObject calls visit with every object hash the store holds.
func (s *Store) EachObject(visit func(hash string) error) error {
	root := filepath.Join(s.root, objectDir)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if filepath.Ext(name) != ".gz" {
			return nil
		}
		hash := name[:len(name)-len(".gz")]
		if !ValidHash(hash) {
			return nil
		}
		return visit(hash)
	})
}

type object struct {
	decompressor *gzip.Reader
	file         *os.File
}

func (o *object) Read(p []byte) (int, error) { return o.decompressor.Read(p) }

func (o *object) Close() error {
	decompressErr := o.decompressor.Close()
	fileErr := o.file.Close()
	if decompressErr != nil {
		return decompressErr
	}
	return fileErr
}
