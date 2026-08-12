// Package store implements the portable, self-contained save store.
//
//	VERSION                  the format marker, so a reader knows what it has
//	objects/ab/<sha>.gz      file content, gzip, sharded by the hash's first byte
//	revisions/ab/<id>.json   one manifest per revision, naming its files
//	omnisaves/ab/<id>.json   lineage records: name, game, fork origin
//	games/ab/<id>.json       catalog identity, so a recovered save has a name
//	deletions/<kind>/ab/<id>.json committed deletion markers
//	reclaiming/ab/<sha>.gz objects staged for crash-safe removal
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Version is the store format written by this package. Newer formats are rejected.
const Version = 4

const (
	versionFile   = "VERSION"
	objectDir     = "objects"
	revisionDir   = "revisions"
	omnisaveDir   = "omnisaves"
	gameDir       = "games"
	deletionDir   = "deletions"
	reclaimingDir = "reclaiming"
	versionPrefix = "omnisave-store"
)

// ErrNotFound reports content or a record the store does not hold.
var ErrNotFound = errors.New("store: not found")

// ErrContentMismatch reports content whose bytes disagree with the hash naming
// them — a failed write, a truncated copy, or bit rot on the medium.
var ErrContentMismatch = errors.New("store: content does not match its hash")

// Store is an open save store directory.
type Store struct {
	root string
}

// Open prepares the store at root, creating and stamping it when it is new.
// An existing store is checked for a format it can read before anything in it
// is touched.
func Open(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("store: root must not be empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: absolute}
	if err := s.ensureLayout(); err != nil {
		return nil, err
	}
	return s, nil
}

// Root is the store's directory — the thing to copy.
func (s *Store) Root() string { return s.root }

func (s *Store) ensureLayout() error {
	for _, directory := range []string{"", objectDir, revisionDir, omnisaveDir, gameDir, reclaimingDir,
		filepath.Join(deletionDir, DeletionOmnisave), filepath.Join(deletionDir, DeletionRevision),
		filepath.Join(deletionDir, DeletionGame)} {
		if err := os.MkdirAll(filepath.Join(s.root, directory), 0o755); err != nil {
			return fmt.Errorf("store: create layout: %w", err)
		}
	}
	if err := s.checkVersion(); err != nil {
		return err
	}
	if err := s.restoreQuarantinedObjects(); err != nil {
		return err
	}
	return s.sweepTemporaries()
}

// sweepTemporaries removes files left by interrupted atomic writes.
func (s *Store) sweepTemporaries() error {
	return filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	})
}

// checkVersion reads the format marker, stamping it when the store is new.
func (s *Store) checkVersion() error {
	path := filepath.Join(s.root, versionFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		marker := fmt.Sprintf("%s %d\n", versionPrefix, Version)
		return writeFileAtomic(path, []byte(marker), 0o644)
	}
	if err != nil {
		return fmt.Errorf("store: read version: %w", err)
	}
	found, err := parseVersion(string(data))
	if err != nil {
		return err
	}
	if found > Version {
		return fmt.Errorf("store: format version %d is newer than supported version %d", found, Version)
	}
	if found < Version {
		marker := fmt.Sprintf("%s %d\n", versionPrefix, Version)
		return writeFileAtomic(path, []byte(marker), 0o644)
	}
	return nil
}

func parseVersion(marker string) (int, error) {
	fields := strings.Fields(strings.TrimSpace(marker))
	if len(fields) != 2 || fields[0] != versionPrefix {
		return 0, fmt.Errorf("store: %s does not name an Omnisave store", versionFile)
	}
	var version int
	if _, err := fmt.Sscanf(fields[1], "%d", &version); err != nil || version < 1 {
		return 0, fmt.Errorf("store: %s carries an unreadable version", versionFile)
	}
	return version, nil
}

// objectPath locates content in a shard selected by the hash's first byte.
func (s *Store) objectPath(hash string) string {
	return filepath.Join(s.root, objectDir, hash[:2], hash+".gz")
}

func (s *Store) quarantinedObjectPath(hash string) string {
	return filepath.Join(s.root, reclaimingDir, hash[:2], hash+".gz")
}

func (s *Store) revisionPath(id string) string {
	return filepath.Join(s.root, revisionDir, shard(id), id+".json")
}

func (s *Store) omnisavePath(id string) string {
	return filepath.Join(s.root, omnisaveDir, shard(id), id+".json")
}

func (s *Store) gamePath(id string) string {
	return filepath.Join(s.root, gameDir, shard(id), id+".json")
}

func (s *Store) deletionPath(targetKind, id string) string {
	return filepath.Join(s.root, deletionDir, targetKind, shard(id), id+".json")
}

// shard distributes opaque identifiers by the first byte of their hash.
func shard(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:1])
}

// ValidHash reports whether a string is a lowercase hexadecimal SHA-256, the
// only shape this store will read or write as an object name.
func ValidHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, character := range hash {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

// writeFileAtomic replaces a file without exposing partial content.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// syncDirectory makes a completed rename durable across power loss.
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
