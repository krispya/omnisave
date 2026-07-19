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
		writeFile(t, directory, "slot.sav", "old-progress"),
		writeFile(t, directory, "obsolete.dat", "old-sidecar"),
	}}
	matched := omnisave.Revision{ID: "revision-1", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/slot.sav", "old-progress", "application/octet-stream"),
		revisionFile("battery/obsolete.dat", "old-sidecar", "application/octet-stream"),
	}}
	head := omnisave.Revision{ID: "revision-2", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/slot.sav", "new-progress", "application/octet-stream"),
		revisionFile("battery/profile/new.dat", "new-sidecar", "application/octet-stream"),
	}}
	source := artifactSource{}
	for _, file := range head.Files {
		if file.Path == "battery/slot.sav" {
			source[file.Artifact.SHA256] = []byte("new-progress")
		} else {
			source[file.Artifact.SHA256] = []byte("new-sidecar")
		}
	}

	if err := binding.FastForward(context.Background(), source, current, matched, head); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(directory, "slot.sav"), "new-progress")
	assertFileContent(t, filepath.Join(directory, "profile", "new.dat"), "new-sidecar")
	if _, err := os.Stat(filepath.Join(directory, "obsolete.dat")); !os.IsNotExist(err) {
		t.Fatalf("expected a file absent from the head to be removed, got %v", err)
	}
}

func TestFastForwardLeavesTheLocalSaveUntouchedWhenADownloadIsInvalid(t *testing.T) {
	directory := t.TempDir()
	current := target.Save{Files: []target.File{writeFile(t, directory, "slot.sav", "old-progress")}}
	matched := omnisave.Revision{ID: "revision-1", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{
		revisionFile("battery/slot.sav", "old-progress", "application/octet-stream"),
	}}
	headFile := revisionFile("battery/slot.sav", "new-progress", "application/octet-stream")
	head := omnisave.Revision{ID: "revision-2", OmnisaveID: "save-a", Files: []omnisave.RevisionFile{headFile}}

	err := binding.FastForward(context.Background(), artifactSource{headFile.Artifact.SHA256: []byte("corrupt")}, current, matched, head)
	if err == nil {
		t.Fatal("expected corrupt server content to stop the fast-forward")
	}
	assertFileContent(t, filepath.Join(directory, "slot.sav"), "old-progress")
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
