package binding_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

type artifactSource map[string][]byte

func (s artifactSource) OpenArtifact(_ context.Context, hash string) (io.ReadCloser, error) {
	content, ok := s[hash]
	if !ok {
		return nil, fmt.Errorf("artifact not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestFastForwardAppliesACompleteVerifiedHeadSnapshot(t *testing.T) {
	directory := t.TempDir()
	current := target.Save{Files: []target.File{
		writeFile(t, directory, "progress.sav", "old-progress"),
		writeFile(t, directory, "obsolete.dat", "old-sidecar"),
	}}
	matched := omnisave.Revision{ID: "revision-1", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/progress.sav", "old-progress", "application/octet-stream"),
		revisionFile("battery/obsolete.dat", "old-sidecar", "application/octet-stream"),
	}}
	head := omnisave.Revision{ID: "revision-2", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/progress.sav", "new-progress", "application/octet-stream"),
		revisionFile("battery/profile/new.dat", "new-sidecar", "application/octet-stream"),
	}}
	source := artifactSource{}
	for _, file := range head.Files {
		if file.Path == "battery/progress.sav" {
			source[file.Artifact.SHA256] = []byte("new-progress")
		} else {
			source[file.Artifact.SHA256] = []byte("new-sidecar")
		}
	}

	if err := binding.FastForward(context.Background(), source, current, matched, head); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(directory, "progress.sav"), "new-progress")
	assertFileContent(t, filepath.Join(directory, "profile", "new.dat"), "new-sidecar")
	if _, err := os.Stat(filepath.Join(directory, "obsolete.dat")); !os.IsNotExist(err) {
		t.Fatalf("expected a file absent from the head to be removed, got %v", err)
	}
}

func TestFastForwardLeavesTheLocalSaveUntouchedWhenADownloadIsInvalid(t *testing.T) {
	directory := t.TempDir()
	current := target.Save{Files: []target.File{writeFile(t, directory, "progress.sav", "old-progress")}}
	matched := omnisave.Revision{ID: "revision-1", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/progress.sav", "old-progress", "application/octet-stream"),
	}}
	headFile := revisionFile("battery/progress.sav", "new-progress", "application/octet-stream")
	head := omnisave.Revision{ID: "revision-2", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{headFile}}

	err := binding.FastForward(context.Background(), artifactSource{headFile.Artifact.SHA256: []byte("corrupt")}, current, matched, head)
	if err == nil {
		t.Fatal("expected corrupt server content to stop the fast-forward")
	}
	assertFileContent(t, filepath.Join(directory, "progress.sav"), "old-progress")
}

func TestMaterializePlacesACompleteHeadIntoAnEmptySaveDestination(t *testing.T) {
	directory := t.TempDir()
	config := revisionFile("config/settings.json", "settings", "application/json")
	progress := revisionFile("save/progress.sav", "progress", "application/octet-stream")
	head := omnisave.Revision{ID: "revision-1", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{config, progress}}
	destination := target.SaveDestination{
		ID: "local-save", TargetID: "target-a", GameID: "game-a", Kind: "local",
		Locations: []target.SaveLocation{
			{ID: "config", Path: filepath.Join(directory, "settings.json"), Kind: target.SaveLocationUnknown},
			{ID: "save", Path: filepath.Join(directory, "Saves"), Kind: target.SaveLocationUnknown},
		},
	}
	source := artifactSource{
		config.Artifact.SHA256:   []byte("settings"),
		progress.Artifact.SHA256: []byte("progress"),
	}

	materialized, err := binding.Materialize(context.Background(), source, destination, head)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(directory, "settings.json"), "settings")
	assertFileContent(t, filepath.Join(directory, "Saves", "progress.sav"), "progress")
	if materialized.ID != destination.ID || len(materialized.Files) != 2 {
		t.Fatalf("expected a discovered save matching the destination, got %+v", materialized)
	}
}

func TestMaterializeLeavesAnEmptyDestinationUntouchedWhenADownloadIsInvalid(t *testing.T) {
	directory := t.TempDir()
	headFile := revisionFile("battery/progress.sav", "progress", "application/octet-stream")
	destination := target.SaveDestination{
		ID: "local-save", TargetID: "target-a", GameID: "game-a", Kind: "battery",
		Locations: []target.SaveLocation{{
			ID: "battery", Path: filepath.Join(directory, "saves"), Kind: target.SaveLocationDirectory,
		}},
	}

	_, err := binding.Materialize(context.Background(),
		artifactSource{headFile.Artifact.SHA256: []byte("corrupt")}, destination,
		omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{headFile}})
	if err == nil {
		t.Fatal("expected corrupt server content to stop materialization")
	}
	if _, err := os.Stat(filepath.Join(directory, "saves")); !os.IsNotExist(err) {
		t.Fatalf("expected the empty destination to stay untouched, got %v", err)
	}
}

type appearingArtifactSource struct {
	artifactSource
	target string
}

func (s appearingArtifactSource) OpenArtifact(ctx context.Context, hash string) (io.ReadCloser, error) {
	if err := os.WriteFile(s.target, []byte("local-progress"), 0o600); err != nil {
		return nil, err
	}
	return s.artifactSource.OpenArtifact(ctx, hash)
}

func TestMaterializeRefusesAFileThatAppearsDuringDownload(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "progress.sav")
	headFile := revisionFile("battery/progress.sav", "server-progress", "application/octet-stream")
	destination := target.SaveDestination{
		ID: "local-save", TargetID: "target-a", GameID: "game-a", Kind: "battery",
		Locations: []target.SaveLocation{{
			ID: "battery", Path: directory, Kind: target.SaveLocationDirectory,
		}},
	}
	source := appearingArtifactSource{
		artifactSource: artifactSource{headFile.Artifact.SHA256: []byte("server-progress")},
		target:         targetPath,
	}

	if _, err := binding.Materialize(context.Background(), source, destination,
		omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{headFile}}); err == nil {
		t.Fatal("expected a late local save to stop materialization")
	}
	assertFileContent(t, targetPath, "local-progress")
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != expected {
		t.Fatalf("expected %q at %s, got %q", expected, path, content)
	}
}
