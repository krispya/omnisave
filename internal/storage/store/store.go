// Package store is Omnisave's portable save store: one directory holding
// everything needed to recover saved games, and nothing else.
//
// The store is the durable record; the SQLite database is an index over it.
// Copy the directory anywhere and every save is recoverable from it alone,
// without this server, this database, or a network. That property is what the
// layout is for:
//
//	VERSION                  the format marker, so a reader knows what it has
//	objects/ab/<sha>.gz      file content, gzip, sharded by the hash's first byte
//	revisions/ab/<id>.json   one manifest per revision, naming its files
//	omnisaves/ab/<id>.json   lineage records: name, game, fork origin, tombstone
//	games/ab/<id>.json       catalog identity, so a recovered save has a name
//
// Manifests and records are uncompressed JSON because a person reading them
// during a recovery has a text editor and may have nothing else. Object content
// is compressed because saves are numerous and compress well; gzip is the one
// format every platform can already open.
//
// Deliberately absent: credentials, pairing state, the owner PIN, the owner
// token, and owner settings. Those are deployment secrets, they are not needed
// to recover a save, and this directory is meant to be handed to someone.
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

// Version is the store format this package writes. A reader refuses a store
// numbered higher than this: an older binary cannot know what a newer format
// left out, and guessing with save data is not acceptable. Version 3 added
// deleted_revisions to lineage records — a reader that ignored it would
// resurrect deleted revisions on rebuild, which is exactly the guessing this
// refusal exists to prevent.
const Version = 3

const (
	versionFile   = "VERSION"
	objectDir     = "objects"
	revisionDir   = "revisions"
	omnisaveDir   = "omnisaves"
	gameDir       = "games"
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
	for _, directory := range []string{"", objectDir, revisionDir, omnisaveDir, gameDir} {
		if err := os.MkdirAll(filepath.Join(s.root, directory), 0o755); err != nil {
			return fmt.Errorf("store: create layout: %w", err)
		}
	}
	if err := s.checkVersion(); err != nil {
		return err
	}
	return s.sweepTemporaries()
}

// sweepTemporaries removes the residue of writes that never finished — a crash
// between creating a temporary file and renaming it into place. Nothing ever
// reads them, but they accumulate forever on a machine that loses power, and a
// person recovering by hand should not have to wonder what they are.
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

// objectPath locates content by hash. Objects sit under a directory named for
// the hash's first byte: a store accumulates one object per distinct file per
// revision, and flat directories of that size are slow to walk and unpleasant
// to open in a file manager.
func (s *Store) objectPath(hash string) string {
	return filepath.Join(s.root, objectDir, hash[:2], hash+".gz")
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

// shard groups records the way objects are grouped. Record identifiers are
// opaque, so this takes the first two characters of their hash rather than of
// the identifier itself, which keeps the spread even whatever they look like.
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

// writeFileAtomic replaces a file with content, leaving either the old file or
// the new one behind and never a partial write. A store may be copied at any
// moment, including during a write, and a half-written manifest in the copy
// would be indistinguishable from a corrupt one.
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

// syncDirectory forces a directory entry to disk. Renaming a fully written file
// into place is atomic, but the rename itself is not durable until the
// directory holding it is flushed — without this, power loss can leave a store
// whose object or manifest was written and then forgotten. The store is the
// durable record, so it pays for the flush.
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
