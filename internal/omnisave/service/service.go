// Package service implements the Omnisave application service.
package service

import (
	"context"
	"encoding/hex"
	"errors"
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
}

const (
	maxDisplayNameLength  = 100
	maxRevisionFiles      = 1024
	maxRevisionPathLength = 1024
)

// New creates an Omnisave service backed by repository.
func New(repository storage.OmnisaveRepository) omnisave.Service {
	return &service{repository: repository}
}

func (s *service) Create(ctx context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error) {
	if input.GameID == "" {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(input.DisplayName)
	if !valid {
		return nil, omnisave.ErrInvalid
	}
	save := omnisave.Omnisave{
		ID:          uuid.NewString(),
		GameID:      input.GameID,
		DisplayName: displayName,
		CreatedAt:   time.Now().UTC(),
		Metadata:    cloneMap(input.Metadata),
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
	if !valid {
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
	now := time.Now().UTC()
	revisionID := uuid.NewString()
	fork := omnisave.Omnisave{
		ID:             uuid.NewString(),
		GameID:         source.GameID,
		DisplayName:    displayName,
		HeadRevisionID: &revisionID,
		ForkedFrom: &omnisave.ForkOrigin{
			OmnisaveID: source.ID,
			RevisionID: sourceRevision.ID,
		},
		CreatedAt: now,
		Metadata:  mergeMaps(source.Metadata, input.Metadata),
	}
	initial := omnisave.Revision{
		ID:         revisionID,
		OmnisaveID: fork.ID,
		CreatedAt:  now,
		Files:      cloneFiles(sourceRevision.Files),
		Metadata:   cloneMap(sourceRevision.Metadata),
	}
	if err := s.repository.ForkOmnisave(ctx, fork, initial); err != nil {
		return nil, translateError(err)
	}
	return &omnisave.ForkResult{Omnisave: fork, Revision: initial}, nil
}

func (s *service) CommitRevision(ctx context.Context, saveID string, input omnisave.CreateRevision) (*omnisave.Revision, error) {
	save, err := s.repository.GetOmnisave(ctx, saveID)
	if err != nil {
		return nil, translateError(err)
	}
	if input.ExpectedHeadID != nil && *input.ExpectedHeadID == "" {
		return nil, omnisave.ErrInvalid
	}
	if !sameString(save.HeadRevisionID, input.ExpectedHeadID) {
		return nil, &omnisave.HeadConflict{
			ExpectedHeadID: cloneString(input.ExpectedHeadID),
			ActualHeadID:   cloneString(save.HeadRevisionID),
		}
	}
	filesByPath := make(map[string]omnisave.RevisionFile)
	if input.ExpectedHeadID != nil {
		parent, err := s.repository.GetRevision(ctx, saveID, *input.ExpectedHeadID)
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
		ParentID:   cloneString(input.ExpectedHeadID),
		CreatedAt:  time.Now().UTC(),
		Files:      files,
		Metadata:   cloneMap(input.Metadata),
	}
	if err := s.repository.CommitRevision(ctx, input.ExpectedHeadID, revision); err != nil {
		var conflict *storage.HeadConflict
		if errors.As(err, &conflict) {
			return nil, &omnisave.HeadConflict{
				ExpectedHeadID: cloneString(input.ExpectedHeadID),
				ActualHeadID:   cloneString(conflict.ActualHeadID),
			}
		}
		return nil, translateError(err)
	}
	return &revision, nil
}

func (s *service) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	revision, err := s.repository.GetRevision(ctx, saveID, revisionID)
	return revision, translateError(err)
}

func (s *service) ListRevisions(ctx context.Context, saveID string) ([]omnisave.Revision, error) {
	revisions, err := s.repository.ListRevisions(ctx, saveID)
	return revisions, translateError(err)
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

func cloneFiles(source []omnisave.RevisionFile) []omnisave.RevisionFile {
	return append([]omnisave.RevisionFile(nil), source...)
}

func cloneString(source *string) *string {
	if source == nil {
		return nil
	}
	copy := *source
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

var _ omnisave.Service = (*service)(nil)
