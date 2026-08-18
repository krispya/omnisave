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

// Every record names its kind and version so it remains identifiable in isolation.
const (
	KindRevision = "revision"
	KindOmnisave = "omnisave"
	KindGame     = "game"
	KindDeletion = "deletion"

	DeletionOmnisave = "omnisave"
	DeletionRevision = "revision"
	DeletionGame     = "game"
)

// Revision is a self-contained immutable snapshot manifest.
type Revision struct {
	Kind     string           `json:"kind"`
	Version  int              `json:"version"`
	ID       string           `json:"id"`
	Omnisave RevisionOmnisave `json:"omnisave"`
	Game     RevisionGame     `json:"game"`
	// Parent is the revision this one followed, or nil at the root of a tree.
	Parent    *string   `json:"parent"`
	CreatedAt time.Time `json:"created_at"`
	// SavedAt is when the content was written by the game, as the committing
	// Device reported it; absent for revisions committed before Devices did.
	SavedAt  *time.Time        `json:"saved_at,omitempty"`
	Files    []RevisionFile    `json:"files"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RevisionOmnisave names the lineage a snapshot belongs to.
type RevisionOmnisave struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// RevisionGame identifies the game without requiring another store record.
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

// Omnisave records a lineage's mutable identity, labels, and current pointer.
type Omnisave struct {
	Kind        string `json:"kind"`
	Version     int    `json:"version"`
	ID          string `json:"id"`
	GameID      string `json:"game_id"`
	DisplayName string `json:"display_name"`
	// CurrentRevisionID is the global snapshot Devices synchronize toward.
	// It is not derivable once restoring can select any node in the tree.
	CurrentRevisionID *string `json:"current_revision_id,omitempty"`
	// RevisionNames keeps mutable labels out of immutable snapshot manifests.
	RevisionNames map[string]string `json:"revision_names,omitempty"`
	// RevisionNameSources mirrors RevisionNames with each name's origin —
	// omnisave.NameSourceLabeler or NameSourceManual — so a rebuilt database
	// still knows which names automation may replace.
	RevisionNameSources map[string]string `json:"revision_name_sources,omitempty"`
	// Achievements records what a game unlocked while Omnisave watched it, and
	// the revision each unlock landed on. They belong to the lineage rather
	// than to a snapshot: an unlock is an account's event, not part of the
	// bytes a manifest describes, and the same snapshot restored elsewhere has
	// earned nothing.
	Achievements []Achievement        `json:"achievements,omitempty"`
	ForkedFrom   *omnisave.ForkOrigin `json:"forked_from,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	// DeletedAt and DeletedRevisions are version-3 compatibility fields. Open
	// migrates them to immutable deletion markers before recovery.
	DeletedAt        *time.Time        `json:"deleted_at,omitempty"`
	DeletedRevisions []string          `json:"deleted_revisions,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// Achievement is one unlock and the revision it landed on. RevisionID is
// empty while the unlock is still waiting for the next commit.
type Achievement struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	UnlockedAt  time.Time `json:"unlocked_at"`
	RevisionID  string    `json:"revision_id,omitempty"`
}

// Game records the catalog identity needed to identify a recovered save.
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

// Deletion is immutable proof that a logical deletion committed.
type Deletion struct {
	Kind       string    `json:"kind"`
	Version    int       `json:"version"`
	TargetKind string    `json:"target_kind"`
	TargetID   string    `json:"target_id"`
	DeletedAt  time.Time `json:"deleted_at"`
}

// PutRevision writes an immutable manifest and leaves an existing one unchanged.
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

// PutOmnisave writes a changed lineage record and leaves an equal record untouched.
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

// PutDeletion records an acknowledged deletion once. Identifiers are never
// reused, so an existing immutable marker is already the same committed fact.
func (s *Store) PutDeletion(marker Deletion) error {
	if marker.TargetID == "" || marker.DeletedAt.IsZero() || !validDeletionKind(marker.TargetKind) {
		return errors.New("store: deletion needs a supported target and timestamp")
	}
	marker.Kind, marker.Version = KindDeletion, Version
	path := s.deletionPath(marker.TargetKind, marker.TargetID)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.writeRecord(path, marker)
}

// GetDeletion reads one committed deletion marker.
func (s *Store) GetDeletion(targetKind, id string) (Deletion, error) {
	if !validDeletionKind(targetKind) {
		return Deletion{}, ErrNotFound
	}
	var marker Deletion
	err := s.readRecord(s.deletionPath(targetKind, id), KindDeletion, &marker)
	return marker, err
}

// HasDeletion reports whether a committed marker rests in the store.
func (s *Store) HasDeletion(targetKind, id string) bool {
	if !validDeletionKind(targetKind) {
		return false
	}
	_, err := os.Stat(s.deletionPath(targetKind, id))
	return err == nil
}

// EachDeletion calls visit for every marker of one supported target kind.
func (s *Store) EachDeletion(targetKind string, visit func(Deletion) error) error {
	if !validDeletionKind(targetKind) {
		return errors.New("store: unsupported deletion target")
	}
	return s.eachRecord(filepath.Join(deletionDir, targetKind), func(path string) error {
		var marker Deletion
		if err := s.readRecord(path, KindDeletion, &marker); err != nil {
			return err
		}
		return visit(marker)
	})
}

// EachDeletionID lists marker identifiers without reading their records.
func (s *Store) EachDeletionID(targetKind string, visit func(id string) error) error {
	if !validDeletionKind(targetKind) {
		return errors.New("store: unsupported deletion target")
	}
	return s.eachRecordID(filepath.Join(deletionDir, targetKind), visit)
}

func validDeletionKind(targetKind string) bool {
	return targetKind == DeletionOmnisave || targetKind == DeletionRevision || targetKind == DeletionGame
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

// EachOmnisave calls visit with every lineage record.
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

// EachRevisionID visits manifest identifiers without reading their records.
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

// RemoveRevision deletes a manifest only after an immutable deletion marker is
// durable; revisions are otherwise immutable and permanent.
func (s *Store) RemoveRevision(id string) error {
	if err := os.Remove(s.revisionPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RemoveGame deletes a catalog identity record after its immutable game marker
// and the markers for its lineages are durable.
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
