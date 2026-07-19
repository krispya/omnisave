package binding_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

type fakeServer struct {
	uploads     map[string][]byte
	created     []omnisave.CreateOmnisave
	committed   map[string]omnisave.CreateRevision
	deleted     []string
	commitError error
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		uploads:   make(map[string][]byte),
		committed: make(map[string]omnisave.CreateRevision),
	}
}

func (f *fakeServer) UploadArtifact(_ context.Context, artifact omnisave.Artifact, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	if int64(len(data)) != artifact.Size {
		return errors.New("artifact size does not match content")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return errors.New("artifact hash does not match content")
	}
	f.uploads[artifact.SHA256] = data
	return nil
}

func (f *fakeServer) CreateOmnisave(_ context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error) {
	f.created = append(f.created, input)
	return &omnisave.Omnisave{ID: "omnisave-1", GameID: input.GameID}, nil
}

func (f *fakeServer) CommitRevision(_ context.Context, omnisaveID string, input omnisave.CreateRevision) (*omnisave.Revision, error) {
	if f.commitError != nil {
		return nil, f.commitError
	}
	f.committed[omnisaveID] = input
	return &omnisave.Revision{ID: "revision-1", OmnisaveID: omnisaveID, Files: input.Upserts}, nil
}

func (f *fakeServer) DeleteOmnisave(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func writeFile(t *testing.T, directory, name, content string) target.File {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return target.File{Path: path, LocationID: "battery", RelativePath: name, Size: int64(len(content))}
}

func revisionFile(path, content, format string) omnisave.RevisionFile {
	digest := sha256.Sum256([]byte(content))
	return omnisave.RevisionFile{
		Path: path,
		Artifact: omnisave.Artifact{
			Format: format,
			SHA256: hex.EncodeToString(digest[:]),
			Size:   int64(len(content)),
		},
	}
}

func TestFindContentMatchesRecognizesTheUniqueMatchingHead(t *testing.T) {
	directory := t.TempDir()
	local := target.Save{Files: []target.File{
		writeFile(t, directory, "Zelda.srm", "battery-content"),
		writeFile(t, directory, "Zelda.rtc", "clock-content"),
	}}
	headID := "revision-a2"
	lineages := []binding.Lineage{
		{
			Omnisave: omnisave.Omnisave{ID: "save-a", HeadRevisionID: &headID},
			Revisions: []omnisave.Revision{
				{ID: "revision-a1", Files: []omnisave.RevisionFile{revisionFile("battery/Zelda.srm", "old", "application/octet-stream")}},
				{ID: headID, Files: []omnisave.RevisionFile{
					revisionFile("battery/Zelda.rtc", "clock-content", "application/x-clock"),
					revisionFile("battery/Zelda.srm", "battery-content", "application/x-save"),
				}},
			},
		},
		{
			Omnisave: omnisave.Omnisave{ID: "save-b"},
			Revisions: []omnisave.Revision{{
				ID:    "revision-b1",
				Files: []omnisave.RevisionFile{revisionFile("battery/Zelda.srm", "different", "application/octet-stream")},
			}},
		},
	}

	matches, err := binding.FindContentMatches(local, lineages)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Omnisave.ID != "save-a" || !matches[0].MatchesHead() {
		t.Fatalf("expected only save-a's head to match, got %+v", matches)
	}
}

func TestFindContentMatchesRecognizesAnOlderRevisionWithoutCallingItTheHead(t *testing.T) {
	directory := t.TempDir()
	local := target.Save{Files: []target.File{writeFile(t, directory, "Metroid.srm", "checkpoint")}}
	headID := "revision-2"
	lineage := binding.Lineage{
		Omnisave: omnisave.Omnisave{ID: "save-a", HeadRevisionID: &headID},
		Revisions: []omnisave.Revision{
			{ID: "revision-1", Files: []omnisave.RevisionFile{revisionFile("battery/Metroid.srm", "checkpoint", "application/octet-stream")}},
			{ID: headID, Files: []omnisave.RevisionFile{revisionFile("battery/Metroid.srm", "continued", "application/octet-stream")}},
		},
	}

	matches, err := binding.FindContentMatches(local, []binding.Lineage{lineage})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].MatchesHead() || len(matches[0].Revisions) != 1 || matches[0].Revisions[0].ID != "revision-1" {
		t.Fatalf("expected an older-only match in save-a, got %+v", matches)
	}
}

func TestFindContentMatchesKeepsIdenticalForksAmbiguous(t *testing.T) {
	directory := t.TempDir()
	local := target.Save{Files: []target.File{writeFile(t, directory, "Chrono.srm", "shared-checkpoint")}}
	file := revisionFile("battery/Chrono.srm", "shared-checkpoint", "application/octet-stream")
	headA, headB := "revision-a", "revision-b"

	matches, err := binding.FindContentMatches(local, []binding.Lineage{
		{Omnisave: omnisave.Omnisave{ID: "save-a", HeadRevisionID: &headA}, Revisions: []omnisave.Revision{{ID: headA, Files: []omnisave.RevisionFile{file}}}},
		{Omnisave: omnisave.Omnisave{ID: "save-b", HeadRevisionID: &headB}, Revisions: []omnisave.Revision{{ID: headB, Files: []omnisave.RevisionFile{file}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected both identical lineages to remain visible, got %+v", matches)
	}
}

func TestSeedUploadsContentAndCommitsInitialRevision(t *testing.T) {
	directory := t.TempDir()
	save := target.Save{
		ID:       "save-1",
		TargetID: "target-1",
		GameID:   "game-1",
		Kind:     "battery",
		Files: []target.File{
			writeFile(t, directory, "Zelda.srm", "battery-content"),
			writeFile(t, directory, "Zelda.rtc", "clock-content"),
		},
	}
	server := newFakeServer()

	created, revision, err := binding.Seed(context.Background(), server, "server-game-1", save)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "omnisave-1" || revision.ID != "revision-1" {
		t.Fatalf("unexpected identities: %s %s", created.ID, revision.ID)
	}
	if len(server.created) != 1 || server.created[0].GameID != "server-game-1" {
		t.Fatalf("unexpected create requests: %+v", server.created)
	}
	if server.created[0].DisplayName != "" {
		t.Fatalf("seeded Omnisave should use the default display name, got %q", server.created[0].DisplayName)
	}
	if len(server.uploads) != 2 {
		t.Fatalf("expected 2 uploaded artifacts, got %d", len(server.uploads))
	}

	commit, ok := server.committed["omnisave-1"]
	if !ok {
		t.Fatal("no revision was committed")
	}
	if commit.ExpectedHeadID != nil {
		t.Fatalf("seed must commit against an empty head, got %v", *commit.ExpectedHeadID)
	}
	if len(commit.Upserts) != 2 {
		t.Fatalf("expected 2 upserts, got %d", len(commit.Upserts))
	}
	if commit.Upserts[0].Path != "battery/Zelda.srm" || commit.Upserts[1].Path != "battery/Zelda.rtc" {
		t.Fatalf("unexpected canonical paths: %s, %s", commit.Upserts[0].Path, commit.Upserts[1].Path)
	}
	for _, upsert := range commit.Upserts {
		if _, uploaded := server.uploads[upsert.Artifact.SHA256]; !uploaded {
			t.Fatalf("revision references artifact %s that was never uploaded", upsert.Artifact.SHA256)
		}
	}
	if len(server.deleted) != 0 {
		t.Fatalf("nothing should be deleted on success, got %v", server.deleted)
	}
}

func TestSeedRemovesEmptyOmnisaveWhenCommitFails(t *testing.T) {
	directory := t.TempDir()
	save := target.Save{
		ID:    "save-1",
		Kind:  "battery",
		Files: []target.File{writeFile(t, directory, "Metroid.srm", "content")},
	}
	server := newFakeServer()
	server.commitError = errors.New("server rejected the revision")

	if _, _, err := binding.Seed(context.Background(), server, "server-game-1", save); err == nil {
		t.Fatal("expected the seed to fail")
	}
	if len(server.deleted) != 1 || server.deleted[0] != "omnisave-1" {
		t.Fatalf("the empty Omnisave should be removed, got deletions %v", server.deleted)
	}
}

func TestSeedRejectsEmptySaves(t *testing.T) {
	server := newFakeServer()
	if _, _, err := binding.Seed(context.Background(), server, "server-game-1", target.Save{ID: "save-1"}); err == nil {
		t.Fatal("expected an error for a save with no files")
	}
	if len(server.created) != 0 {
		t.Fatalf("nothing should be created for an empty save, got %+v", server.created)
	}
}

func TestSeedRequiresResolvedGame(t *testing.T) {
	directory := t.TempDir()
	save := target.Save{Files: []target.File{writeFile(t, directory, "a.srm", "x")}}
	if _, _, err := binding.Seed(context.Background(), newFakeServer(), "", save); err == nil {
		t.Fatal("expected an error for a missing server game ID")
	}
}
