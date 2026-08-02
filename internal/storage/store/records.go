package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Record kinds. Every file the store writes names its own kind and version in
// its first two fields, so a reader that finds one in isolation — pulled out of
// a backup, mailed to somebody, recovered by a file scavenger — can still tell
// what it is holding.
const (
	KindRevision = "revision"
	KindOmnisave = "omnisave"
	KindGame     = "game"
)

// Revision is a manifest: one immutable snapshot, naming the content of every
// file in it. This is the record that makes the store self-describing. An
// object on its own is anonymous bytes; a manifest is what says those bytes are
// the third save of Chrono Trigger and belong at saves/chrono.srm.
//
// It carries the game's and the lineage's identity rather than referencing
// them, so a single manifest is enough to place its files even if every other
// record in the store were lost.
type Revision struct {
	Kind     string           `json:"kind"`
	Version  int              `json:"version"`
	ID       string           `json:"id"`
	Omnisave RevisionOmnisave `json:"omnisave"`
	Game     RevisionGame     `json:"game"`
	// Parent is the revision this one followed, or nil at the root of a
	// lineage. Following these to their end reconstructs history and, because
	// nothing points at the newest revision, identifies the head.
	Parent    *string           `json:"parent"`
	CreatedAt time.Time         `json:"created_at"`
	Files     []RevisionFile    `json:"files"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// RevisionOmnisave names the lineage a snapshot belongs to.
type RevisionOmnisave struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// RevisionGame names the game a snapshot is of. The identifiers travel with it
// so a recovered save can be matched back to a catalog entry — or to a game a
// person recognizes — without a provider lookup.
type RevisionGame struct {
	ID          string                   `json:"id"`
	Title       string                   `json:"title"`
	Platform    string                   `json:"platform,omitempty"`
	Identifiers []catalog.GameIdentifier `json:"identifiers,omitempty"`
}

// RevisionFile places one object at one path inside the save.
type RevisionFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Format string `json:"format,omitempty"`
}

// Omnisave records a lineage's mutable facts — the ones no revision carries
// because they change without a commit. It is written when a save is created,
// renamed, or deleted, and never on an ordinary commit.
//
// The head revision is deliberately absent: it is derivable from the manifests,
// and a field that had to be rewritten on every commit would be a field that
// could be stale in a copy taken mid-write.
type Omnisave struct {
	Kind        string               `json:"kind"`
	Version     int                  `json:"version"`
	ID          string               `json:"id"`
	GameID      string               `json:"game_id"`
	DisplayName string               `json:"display_name"`
	ForkedFrom  *omnisave.ForkOrigin `json:"forked_from,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	// DeletedAt tombstones a lineage instead of erasing its record. Without
	// this, restoring a store would resurrect every save its owner had
	// deliberately thrown away.
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Game records catalog identity, so a recovered save has a title rather than an
// opaque identifier. Provider metadata that can be fetched again is not kept
// here; identity that cannot be re-derived is.
type Game struct {
	Kind            string                    `json:"kind"`
	Version         int                       `json:"version"`
	ID              string                    `json:"id"`
	Title           string                    `json:"title"`
	SortTitle       string                    `json:"sort_title,omitempty"`
	Platform        string                    `json:"platform,omitempty"`
	PlatformCompany string                    `json:"platform_company,omitempty"`
	Publisher       string                    `json:"publisher,omitempty"`
	Identifiers     []catalog.GameIdentifier  `json:"identifiers,omitempty"`
	Fingerprints    []catalog.GameFingerprint `json:"fingerprints,omitempty"`
}

// PutRevision writes a manifest. Manifests are immutable: writing one that
// already rests here is a no-op rather than an overwrite, so a reconciling
// pass can run over a healthy store without rewriting all of it.
func (s *Store) PutRevision(revision Revision) error {
	if revision.ID == "" {
		return errors.New("store: revision needs an id")
	}
	revision.Kind, revision.Version = KindRevision, Version
	path := s.revisionPath(revision.ID)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.writeRecord(path, revision)
}

// GetRevision reads a manifest.
func (s *Store) GetRevision(id string) (Revision, error) {
	var revision Revision
	err := s.readRecord(s.revisionPath(id), KindRevision, &revision)
	return revision, err
}

// HasRevision reports whether a manifest rests here, without reading it.
func (s *Store) HasRevision(id string) bool {
	_, err := os.Stat(s.revisionPath(id))
	return err == nil
}

// PutOmnisave writes or replaces a lineage record, leaving an unchanged one
// alone. Records are rewritten far more often than they change — every open
// reconciles them — and an untouched file is one less thing to have been
// half-written when a copy was taken.
func (s *Store) PutOmnisave(record Omnisave) error {
	if record.ID == "" {
		return errors.New("store: omnisave needs an id")
	}
	record.Kind, record.Version = KindOmnisave, Version
	return s.writeRecord(s.omnisavePath(record.ID), record)
}

// GetOmnisave reads a lineage record.
func (s *Store) GetOmnisave(id string) (Omnisave, error) {
	var record Omnisave
	err := s.readRecord(s.omnisavePath(id), KindOmnisave, &record)
	return record, err
}

// PutGame writes or replaces a catalog identity record.
func (s *Store) PutGame(record Game) error {
	if record.ID == "" {
		return errors.New("store: game needs an id")
	}
	record.Kind, record.Version = KindGame, Version
	return s.writeRecord(s.gamePath(record.ID), record)
}

// GetGame reads a catalog identity record.
func (s *Store) GetGame(id string) (Game, error) {
	var record Game
	err := s.readRecord(s.gamePath(id), KindGame, &record)
	return record, err
}

// EachRevision calls visit with every manifest in the store, in no particular
// order. Recovery walks this: the manifests are the history.
func (s *Store) EachRevision(visit func(Revision) error) error {
	return s.eachRecord(revisionDir, func(path string) error {
		var revision Revision
		if err := s.readRecord(path, KindRevision, &revision); err != nil {
			return err
		}
		return visit(revision)
	})
}

// EachOmnisave calls visit with every lineage record, tombstoned ones included.
func (s *Store) EachOmnisave(visit func(Omnisave) error) error {
	return s.eachRecord(omnisaveDir, func(path string) error {
		var record Omnisave
		if err := s.readRecord(path, KindOmnisave, &record); err != nil {
			return err
		}
		return visit(record)
	})
}

// EachGame calls visit with every catalog identity record.
func (s *Store) EachGame(visit func(Game) error) error {
	return s.eachRecord(gameDir, func(path string) error {
		var record Game
		if err := s.readRecord(path, KindGame, &record); err != nil {
			return err
		}
		return visit(record)
	})
}

// EachRevisionID calls visit with every manifest's identifier without reading
// any manifest. A record's file is named by its identifier, so what the store
// holds is answerable from a directory walk alone — which is what lets a
// rebuild ask that question on every open and read only what it is missing.
func (s *Store) EachRevisionID(visit func(id string) error) error {
	return s.eachRecordID(revisionDir, visit)
}

// EachOmnisaveID calls visit with every lineage record's identifier without
// reading any record.
func (s *Store) EachOmnisaveID(visit func(id string) error) error {
	return s.eachRecordID(omnisaveDir, visit)
}

// EachGameID calls visit with every catalog identity record's identifier
// without reading any record.
func (s *Store) EachGameID(visit func(id string) error) error {
	return s.eachRecordID(gameDir, visit)
}

// RemoveRevision deletes a manifest. Only deletion of a whole lineage reaches
// this; revisions are otherwise immutable and permanent.
func (s *Store) RemoveRevision(id string) error {
	if err := os.Remove(s.revisionPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RemoveGame deletes a catalog identity record. The lineages that referenced it
// keep their tombstones, which carry the game identifier, so a deleted game
// still explains the saves that were deleted with it.
func (s *Store) RemoveGame(id string) error {
	if err := os.Remove(s.gamePath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) writeRecord(path string, record any) error {
	// Indented, with a trailing newline: these are read by people during a
	// recovery, sometimes in an editor that knows nothing about JSON.
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	return writeFileAtomic(path, content, 0o644)
}

func (s *Store) readRecord(path, kind string, into any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var header struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("store: read %s: %w", filepath.Base(path), err)
	}
	if header.Kind != kind {
		return fmt.Errorf("store: %s is a %q record, not a %q", filepath.Base(path), header.Kind, kind)
	}
	if header.Version > Version {
		return fmt.Errorf("store: %s is version %d, newer than supported version %d",
			filepath.Base(path), header.Version, Version)
	}
	return json.Unmarshal(data, into)
}

func (s *Store) eachRecordID(kindDir string, visit func(id string) error) error {
	return s.eachRecord(kindDir, func(path string) error {
		name := filepath.Base(path)
		return visit(strings.TrimSuffix(name, ".json"))
	})
}

func (s *Store) eachRecord(kindDir string, visit func(path string) error) error {
	root := filepath.Join(s.root, kindDir)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		return visit(path)
	})
}
