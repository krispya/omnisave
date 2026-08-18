// Package service implements the Omnisave application service.
package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

type service struct {
	repository storage.OmnisaveRepository
	namer      RevisionNamer
}

const (
	maxDisplayNameLength  = 100
	maxRevisionFiles      = 1024
	maxRevisionPathLength = 1024
	// A report carries what one Device saw unlock since its last report, which
	// is a handful even after a long session away from the server.
	maxAchievementsPerReport = 256
	maxAchievementIDLength   = 200
	maxAchievementNameLength = 500
	maxAchievementDescLength = 2000
)

// RevisionNamer derives a display name for a freshly committed revision from
// the game it belongs to and its complete file set. It is best-effort by
// contract: "" means the revision stays unnamed, and implementations must not
// fail a commit for want of a name.
type RevisionNamer interface {
	NameRevision(ctx context.Context, gameID string, files []omnisave.RevisionFile) string
}

// New creates an Omnisave service backed by repository, committing revisions
// unnamed.
func New(repository storage.OmnisaveRepository) omnisave.Service {
	return NewWithNamer(repository, nil)
}

// NewWithNamer creates an Omnisave service that asks namer to name each
// committed revision from its content. A nil namer commits unnamed.
func NewWithNamer(repository storage.OmnisaveRepository, namer RevisionNamer) omnisave.Service {
	return &service{repository: repository, namer: namer}
}

func (s *service) Create(ctx context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error) {
	if input.GameID == "" {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(input.DisplayName)
	if !valid {
		return nil, omnisave.ErrInvalid
	}
	if displayName == "" {
		var err error
		displayName, err = s.nextDefaultDisplayName(ctx, input.GameID)
		if err != nil {
			return nil, translateError(err)
		}
	}
	now := time.Now().UTC()
	save := omnisave.Omnisave{
		ID:                       uuid.NewString(),
		GameID:                   input.GameID,
		DisplayName:              displayName,
		CreatedAt:                now,
		CurrentRevisionCreatedAt: now,
		Metadata:                 cloneMap(input.Metadata),
	}
	if err := s.repository.InsertOmnisave(ctx, save); err != nil {
		return nil, translateError(err)
	}
	return &save, nil
}

func (s *service) List(ctx context.Context) ([]omnisave.Omnisave, error) {
	saves, err := s.repository.ListOmnisaves(ctx)
	return saves, translateError(err)
}

func (s *service) Get(ctx context.Context, id string) (*omnisave.Omnisave, error) {
	save, err := s.repository.GetOmnisave(ctx, id)
	return save, translateError(err)
}

func (s *service) Update(ctx context.Context, id string, input omnisave.UpdateOmnisave) (*omnisave.Omnisave, error) {
	if input.DisplayName == nil {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(*input.DisplayName)
	if !valid || displayName == "" {
		return nil, omnisave.ErrInvalid
	}
	if err := s.repository.UpdateOmnisaveDisplayName(ctx, id, displayName); err != nil {
		return nil, translateError(err)
	}
	return s.Get(ctx, id)
}

func (s *service) Delete(ctx context.Context, id string) error {
	return translateError(s.repository.DeleteOmnisave(ctx, id))
}

func (s *service) Fork(ctx context.Context, saveID string, input omnisave.ForkOmnisave) (*omnisave.ForkResult, error) {
	source, err := s.repository.GetOmnisave(ctx, saveID)
	if err != nil {
		return nil, translateError(err)
	}
	if input.RevisionID == "" {
		return nil, omnisave.ErrInvalid
	}
	sourceRevision, err := s.repository.GetRevision(ctx, saveID, input.RevisionID)
	if err != nil {
		return nil, translateError(err)
	}
	displayName, valid := normalizeDisplayName(input.DisplayName)
	if !valid {
		return nil, omnisave.ErrInvalid
	}
	if displayName == "" {
		displayName = truncateDisplayName(source.DisplayName + " (fork)")
	}
	now := time.Now().UTC()
	fork := omnisave.Omnisave{
		ID:                uuid.NewString(),
		GameID:            source.GameID,
		DisplayName:       displayName,
		CurrentRevisionID: &sourceRevision.ID,
		ForkedFrom: &omnisave.ForkOrigin{
			OmnisaveID: source.ID,
			RevisionID: sourceRevision.ID,
		},
		CreatedAt:                now,
		CurrentRevisionCreatedAt: sourceRevision.CreatedAt,
		Metadata:                 mergeMaps(source.Metadata, input.Metadata),
	}
	if err := s.repository.ForkOmnisave(ctx, fork); err != nil {
		return nil, translateError(err)
	}
	return &omnisave.ForkResult{Omnisave: fork, Revision: *sourceRevision}, nil
}

func (s *service) Restore(ctx context.Context, saveID string, input omnisave.RestoreRevision) (*omnisave.Omnisave, error) {
	if input.RevisionID == "" ||
		(input.ExpectedCurrentRevisionID != nil && *input.ExpectedCurrentRevisionID == "") {
		return nil, omnisave.ErrInvalid
	}
	if _, err := s.repository.GetRevision(ctx, saveID, input.RevisionID); err != nil {
		return nil, translateError(err)
	}
	if err := s.repository.RestoreOmnisave(
		ctx, saveID, input.RevisionID, input.ExpectedCurrentRevisionID,
	); err != nil {
		var conflict *storage.CurrentRevisionConflict
		if errors.As(err, &conflict) {
			return nil, &omnisave.CurrentRevisionConflict{
				ExpectedCurrentRevisionID: cloneString(input.ExpectedCurrentRevisionID),
				ActualCurrentRevisionID:   cloneString(conflict.ActualCurrentRevisionID),
			}
		}
		return nil, translateError(err)
	}
	return s.Get(ctx, saveID)
}

func (s *service) CommitRevision(ctx context.Context, saveID string, input omnisave.CreateRevision) (*omnisave.Revision, error) {
	save, err := s.repository.GetOmnisave(ctx, saveID)
	if err != nil {
		return nil, translateError(err)
	}
	if input.ExpectedCurrentRevisionID != nil && *input.ExpectedCurrentRevisionID == "" {
		return nil, omnisave.ErrInvalid
	}
	if input.ParentRevisionID != nil && *input.ParentRevisionID == "" {
		return nil, omnisave.ErrInvalid
	}
	if !sameString(save.CurrentRevisionID, input.ExpectedCurrentRevisionID) {
		return nil, &omnisave.CurrentRevisionConflict{
			ExpectedCurrentRevisionID: cloneString(input.ExpectedCurrentRevisionID),
			ActualCurrentRevisionID:   cloneString(save.CurrentRevisionID),
		}
	}
	// The parent is where the new node attaches; the expected current revision
	// is only the concurrency check. They are the same node for an ordinary
	// commit and differ for a branch commit, whose content continues a node a
	// restore moved current away from (FDR-005, decision 15).
	parentRevisionID := input.ExpectedCurrentRevisionID
	if input.ParentRevisionID != nil {
		parentRevisionID = input.ParentRevisionID
	}
	filesByPath := make(map[string]omnisave.RevisionFile)
	if parentRevisionID != nil {
		parent, err := s.repository.GetRevision(ctx, saveID, *parentRevisionID)
		if err != nil {
			return nil, translateError(err)
		}
		for _, file := range parent.Files {
			filesByPath[file.Path] = file
		}
	}

	if len(input.Upserts) == 0 && len(input.Deletes) == 0 {
		return nil, omnisave.ErrInvalid
	}
	missing := make([]string, 0)
	changedPaths := make(map[string]struct{}, len(input.Upserts)+len(input.Deletes))
	seenMissing := make(map[string]struct{})
	for _, file := range input.Upserts {
		if !validRevisionPath(file.Path) || file.Artifact.Format == "" ||
			!validSHA256(file.Artifact.SHA256) || file.Artifact.Size < 0 {
			return nil, omnisave.ErrInvalid
		}
		if _, duplicate := changedPaths[file.Path]; duplicate {
			return nil, omnisave.ErrInvalid
		}
		changedPaths[file.Path] = struct{}{}
		size, err := s.repository.StatArtifact(ctx, file.Artifact.SHA256)
		if errors.Is(err, storage.ErrNotFound) {
			if _, alreadyMissing := seenMissing[file.Artifact.SHA256]; !alreadyMissing {
				missing = append(missing, file.Artifact.SHA256)
				seenMissing[file.Artifact.SHA256] = struct{}{}
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if size != file.Artifact.Size {
			return nil, omnisave.ErrInvalid
		}
		filesByPath[file.Path] = file
	}
	for _, path := range input.Deletes {
		if !validRevisionPath(path) {
			return nil, omnisave.ErrInvalid
		}
		if _, duplicate := changedPaths[path]; duplicate {
			return nil, omnisave.ErrInvalid
		}
		changedPaths[path] = struct{}{}
		if _, exists := filesByPath[path]; !exists {
			return nil, omnisave.ErrInvalid
		}
		delete(filesByPath, path)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &omnisave.MissingArtifacts{SHA256: missing}
	}
	if len(filesByPath) > maxRevisionFiles {
		return nil, omnisave.ErrInvalid
	}
	files := make([]omnisave.RevisionFile, 0, len(filesByPath))
	for _, file := range filesByPath {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	revision := omnisave.Revision{
		ID:         uuid.NewString(),
		OmnisaveID: saveID,
		ParentID:   cloneString(parentRevisionID),
		CreatedAt:  time.Now().UTC(),
		SavedAt:    cloneReportedTime(input.SavedAt),
		Files:      files,
		Metadata:   cloneMap(input.Metadata),
	}
	if s.namer != nil {
		if name, valid := normalizeDisplayName(s.namer.NameRevision(ctx, save.GameID, files)); valid && name != "" {
			revision.DisplayName = name
			revision.NameSource = omnisave.NameSourceLabeler
		}
	}
	if err := s.repository.CommitRevision(ctx, input.ExpectedCurrentRevisionID, revision); err != nil {
		var conflict *storage.CurrentRevisionConflict
		if errors.As(err, &conflict) {
			return nil, &omnisave.CurrentRevisionConflict{
				ExpectedCurrentRevisionID: cloneString(input.ExpectedCurrentRevisionID),
				ActualCurrentRevisionID:   cloneString(conflict.ActualCurrentRevisionID),
			}
		}
		var unavailable *storage.ArtifactsUnavailable
		if errors.As(err, &unavailable) {
			return nil, &omnisave.MissingArtifacts{SHA256: unavailable.SHA256}
		}
		return nil, translateError(err)
	}
	return &revision, nil
}

// RecordAchievements places each unlock on the first revision committed at or
// after it — the earliest snapshot in this save's history that is known to
// carry the achievement. An unlock newer than every revision is stored with
// no revision and waits: the commit that follows claims it, which is what
// makes a report that arrives before the save is written land correctly.
func (s *service) RecordAchievements(ctx context.Context, saveID string, unlocks []omnisave.AchievementUnlock) ([]omnisave.Achievement, error) {
	if _, err := s.repository.GetOmnisave(ctx, saveID); err != nil {
		return nil, translateError(err)
	}
	if len(unlocks) == 0 || len(unlocks) > maxAchievementsPerReport {
		return nil, omnisave.ErrInvalid
	}
	// The history the marks are placed against is the one this save shows, so
	// a mark can never name a revision the save's own log does not list.
	history, err := s.repository.ListRevisions(ctx, saveID)
	if err != nil {
		return nil, translateError(err)
	}
	achievements := make([]omnisave.Achievement, 0, len(unlocks))
	seen := make(map[string]bool, len(unlocks))
	for _, unlock := range unlocks {
		normalized, valid := normalizeUnlock(unlock)
		if !valid {
			return nil, omnisave.ErrInvalid
		}
		if seen[normalized.ID] {
			return nil, omnisave.ErrInvalid
		}
		seen[normalized.ID] = true
		achievements = append(achievements, omnisave.Achievement{
			AchievementUnlock: normalized,
			RevisionID:        firstRevisionAfter(history, normalized.UnlockedAt),
		})
	}
	recorded, err := s.repository.RecordAchievements(ctx, saveID, achievements)
	return recorded, translateError(err)
}

func (s *service) ListAchievements(ctx context.Context, saveID string) ([]omnisave.Achievement, error) {
	achievements, err := s.repository.ListAchievements(ctx, saveID)
	return achievements, translateError(err)
}

// firstRevisionAfter finds the earliest revision committed at or after an
// unlock. Unlock times are whole seconds while commits are not, so a commit
// within the same second as an unlock counts as after it: the alternative
// would push a mark forward a whole revision over a fraction of a second.
func firstRevisionAfter(history []omnisave.Revision, unlockedAt time.Time) *string {
	var found *omnisave.Revision
	for index, revision := range history {
		if revision.CreatedAt.Unix() < unlockedAt.Unix() {
			continue
		}
		if found == nil || revision.CreatedAt.Before(found.CreatedAt) ||
			(revision.CreatedAt.Equal(found.CreatedAt) && revision.ID < found.ID) {
			found = &history[index]
		}
	}
	if found == nil {
		return nil
	}
	id := found.ID
	return &id
}

func normalizeUnlock(unlock omnisave.AchievementUnlock) (omnisave.AchievementUnlock, bool) {
	unlock.ID = strings.TrimSpace(unlock.ID)
	unlock.Name = strings.TrimSpace(unlock.Name)
	unlock.Description = strings.TrimSpace(unlock.Description)
	if unlock.ID == "" || unlock.Name == "" || unlock.UnlockedAt.IsZero() {
		return omnisave.AchievementUnlock{}, false
	}
	if utf8.RuneCountInString(unlock.ID) > maxAchievementIDLength ||
		utf8.RuneCountInString(unlock.Name) > maxAchievementNameLength ||
		utf8.RuneCountInString(unlock.Description) > maxAchievementDescLength {
		return omnisave.AchievementUnlock{}, false
	}
	// Whole seconds is the precision every store publishes, and storing more
	// would imply a resolution the report never had.
	unlock.UnlockedAt = time.Unix(unlock.UnlockedAt.Unix(), 0).UTC()
	return unlock, true
}

func (s *service) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	revision, err := s.repository.GetRevision(ctx, saveID, revisionID)
	return revision, translateError(err)
}

func (s *service) ListRevisions(ctx context.Context, saveID string) ([]omnisave.Revision, error) {
	revisions, err := s.repository.ListRevisions(ctx, saveID)
	return revisions, translateError(err)
}

func (s *service) UpdateRevision(ctx context.Context, saveID, revisionID string, input omnisave.UpdateRevision) (*omnisave.Revision, error) {
	if input.DisplayName == nil {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(*input.DisplayName)
	if !valid || displayName == "" {
		return nil, omnisave.ErrInvalid
	}
	if err := s.repository.UpdateRevisionDisplayName(ctx, saveID, revisionID, displayName); err != nil {
		return nil, translateError(err)
	}
	return s.GetRevision(ctx, saveID, revisionID)
}

func (s *service) DeleteRevision(ctx context.Context, saveID, revisionID string) error {
	if revisionID == "" {
		return omnisave.ErrInvalid
	}
	err := s.repository.DeleteRevision(ctx, saveID, revisionID)
	var inUse *storage.RevisionInUse
	if errors.As(err, &inUse) {
		return &omnisave.RevisionInUse{Reason: inUse.Reason}
	}
	return translateError(err)
}

func (s *service) StoreArtifact(ctx context.Context, artifact omnisave.Artifact, payload io.Reader) error {
	if payload == nil || artifact.Format == "" || !validSHA256(artifact.SHA256) || artifact.Size < 0 {
		return omnisave.ErrInvalid
	}
	return translateError(s.repository.StoreArtifact(ctx, storage.Artifact{
		Format: artifact.Format,
		SHA256: artifact.SHA256,
		Size:   artifact.Size,
	}, payload))
}

func (s *service) StatArtifact(ctx context.Context, hash string) (int64, error) {
	size, err := s.repository.StatArtifact(ctx, hash)
	return size, translateError(err)
}

func (s *service) OpenArtifact(ctx context.Context, hash string) (io.ReadCloser, error) {
	payload, err := s.repository.OpenArtifact(ctx, hash)
	return payload, translateError(err)
}

func translateError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return omnisave.ErrNotFound
	}
	if errors.Is(err, storage.ErrArtifactMismatch) {
		return omnisave.ErrInvalid
	}
	return err
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func mergeMaps(base, changes map[string]string) map[string]string {
	merged := cloneMap(base)
	if merged == nil && len(changes) > 0 {
		merged = make(map[string]string, len(changes))
	}
	for key, value := range changes {
		merged[key] = value
	}
	return merged
}

func cloneString(source *string) *string {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

// cloneReportedTime keeps an optional client-reported time, treating a zero
// value as unreported rather than as a date.
func cloneReportedTime(source *time.Time) *time.Time {
	if source == nil || source.IsZero() {
		return nil
	}
	copy := source.UTC()
	return &copy
}

func validSHA256(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32 && hash == strings.ToLower(hash)
}

func sameString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validRevisionPath(value string) bool {
	if value == "" || len(value) > maxRevisionPathLength || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func normalizeDisplayName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, utf8.RuneCountInString(name) <= maxDisplayNameLength
}

// nextDefaultDisplayName names an Omnisave created without one. Numbering
// continues past the game's highest surviving "Save N" so the default can
// never collide with a name an existing save still carries.
func (s *service) nextDefaultDisplayName(ctx context.Context, gameID string) (string, error) {
	saves, err := s.repository.ListOmnisaves(ctx)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, save := range saves {
		if save.GameID != gameID {
			continue
		}
		var number int
		if _, err := fmt.Sscanf(save.DisplayName, "Save %d", &number); err == nil && number > highest {
			highest = number
		}
	}
	return fmt.Sprintf("Save %d", highest+1), nil
}

func truncateDisplayName(name string) string {
	runes := []rune(name)
	if len(runes) <= maxDisplayNameLength {
		return name
	}
	return string(runes[:maxDisplayNameLength])
}

var _ omnisave.Service = (*service)(nil)
