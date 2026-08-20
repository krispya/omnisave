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

func TestEmbeddedManifestResolvesEveryPatchedSave(t *testing.T) {
	tests := []struct {
		name      string
		patch     string
		steamID   string
		hostOS    string
		saveFiles []string
	}{
		{
			name:    "Lisa The Painful native Linux build",
			patch:   "335670-lisa-the-painful.yaml",
			steamID: "335670",
			hostOS:  saveprofile.OSLinux,
			// The native build writes the base game and Joyful saves beside their games.
			saveFiles: []string{"Save01.rvdata2", "Joyful/Save01.rvdata2"},
		},
		{
			name:    "Lisa The First nested Steam package",
			patch:   "2743030-lisa-the-first.yaml",
			steamID: "2743030",
			hostOS:  saveprofile.OSWindows,
			// Steam nests the original game two levels beneath its install root.
			saveFiles: []string{"Lisa_1/Lisa_1/Save01.lsd"},
		},
	}

	coveredPatches := make(map[string]bool, len(tests))
	for _, test := range tests {
		coveredPatches[test.patch] = true
		t.Run(test.name, func(t *testing.T) {
			installRoot := t.TempDir()
			expected := make(map[string]bool, len(test.saveFiles))
			for _, relativePath := range test.saveFiles {
				path := filepath.Join(installRoot, filepath.FromSlash(relativePath))
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("save"), 0600); err != nil {
					t.Fatal(err)
				}
				expected[path] = true
			}

			game := target.InstalledGame{
				ID:          "steam:" + test.steamID,
				TargetID:    "steam",
				InstallRoot: installRoot,
				Identity: target.GameIdentity{
					Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: test.steamID}},
				},
				Environment: target.Environment{HostOS: test.hostOS, Runtime: target.RuntimeNative},
			}
			profile, err := embedded.Provider().Find(context.Background(), game.Identity)
			if err != nil {
				t.Fatal(err)
			}
			saves, err := saveprofile.Resolve(game, *profile)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, save := range saves {
				for _, file := range save.Files {
					found[file.Path] = true
				}
			}
			for path := range expected {
				if !found[path] {
					t.Fatalf("expected patched save %q, got %+v", path, saves)
				}
			}
		})
	}
	requireEveryPatchHasAResolutionStory(t, coveredPatches)
}

func requireEveryPatchHasAResolutionStory(t *testing.T, covered map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "patches"))
	if err != nil {
		t.Fatal(err)
	}
	patches := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			patches[entry.Name()] = true
		}
	}
	for patch := range patches {
		if !covered[patch] {
			t.Errorf("patch %s needs an embedded resolution story", patch)
		}
	}
	for patch := range covered {
		if !patches[patch] {
			t.Errorf("resolution story references missing patch %s", patch)
		}
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
