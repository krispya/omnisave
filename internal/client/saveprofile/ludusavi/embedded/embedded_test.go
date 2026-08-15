package embedded_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi/embedded"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Undertale opts out of Steam Cloud, so its saves are only reachable through
// profile knowledge. The embedded manifest must keep answering for it.
func TestEmbeddedManifestFindsUndertaleSavesOnLinux(t *testing.T) {
	home := t.TempDir()
	saveDirectory := filepath.Join(home, ".config", "UNDERTALE")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file0", "undertale.ini"} {
		if err := os.WriteFile(filepath.Join(saveDirectory, name), []byte("determination"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	game := target.InstalledGame{
		ID:       "steam:deck:391540",
		TargetID: "steam:deck",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "391540"}}},
		Environment: target.Environment{
			HostOS:  saveprofile.OSLinux,
			Runtime: target.RuntimeNative,
			Home:    home,
		},
	}
	profile, err := embedded.Provider().Find(context.Background(), game.Identity)
	if err != nil {
		t.Fatal(err)
	}
	saves, err := saveprofile.Resolve(game, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected one Undertale save, got %+v", saves)
	}
	// The manifest refreshes from community data, so the test pins the files
	// this test created rather than the entry's exact rule count.
	found := map[string]bool{}
	for _, file := range saves[0].Files {
		found[file.RelativePath] = true
	}
	if !found["file0"] || !found["undertale.ini"] {
		t.Fatalf("expected both created files in the Undertale save, got %+v", saves[0].Files)
	}
}
