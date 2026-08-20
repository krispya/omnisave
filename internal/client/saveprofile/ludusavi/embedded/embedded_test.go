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

// The community manifest splits Lisa across wiki pages that share one Steam
// id: the Definitive Edition saves under LocalLow, the classic build as
// .rvdata2 files in the install directory. The embedded manifest must keep
// both titles' rules, or one build's saves go undiscovered.
func TestEmbeddedManifestKeepsBothLisaBuildsSaveRules(t *testing.T) {
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "335670"}}}
	profile, err := embedded.Provider().Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, rule := range profile.Rules {
		found[rule.Path] = true
	}
	if !found["<base>/*.rvdata2"] {
		t.Fatalf("expected the classic build's install-directory rule, got %+v", profile.Rules)
	}
	if !found["<home>/AppData/LocalLow/Serenity Forge/LISA The Painful Definitive Edition/Savegame*.dat"] {
		t.Fatalf("expected the Definitive Edition's LocalLow rule, got %+v", profile.Rules)
	}
}

// The Steam package for Lisa: The First nests the original game two levels
// beneath Steam's install root. The checked-in patch keeps that packaging
// difference visible until the equivalent path reaches the community data.
func TestEmbeddedManifestFindsLisaTheFirstSteamSave(t *testing.T) {
	installRoot := t.TempDir()
	saveDirectory := filepath.Join(installRoot, "Lisa_1", "Lisa_1")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "Save01.lsd")
	if err := os.WriteFile(savePath, []byte("joy"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:          "steam:2743030",
		TargetID:    "steam",
		InstallRoot: installRoot,
		Identity: target.GameIdentity{
			Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "2743030"}},
		},
		Environment: target.Environment{HostOS: saveprofile.OSWindows, Runtime: target.RuntimeNative},
	}
	profile, err := embedded.Provider().Find(context.Background(), game.Identity)
	if err != nil {
		t.Fatal(err)
	}
	saves, err := saveprofile.Resolve(game, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the nested Steam save, got %+v", saves)
	}
}

// Strife and its Veteran Edition share a Steam id and save template but give
// that template different platform constraints. The merged profile must keep
// every supported platform, or the shared path never applies there.
func TestEmbeddedManifestKeepsSharedTemplateConstraints(t *testing.T) {
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "317040"}}}
	profile, err := embedded.Provider().Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, rule := range profile.Rules {
		if rule.Path == "<base>/strfsav*.ssg" {
			found[rule.OS] = true
		}
	}
	for _, platform := range []string{saveprofile.OSWindows, saveprofile.OSMacOS, saveprofile.OSLinux} {
		if !found[platform] {
			t.Fatalf("expected the shared template on %s, got %+v", platform, profile.Rules)
		}
	}
}

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
