package saveprofile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func TestWindowsProfileResolvesInsideAProtonEnvironment(t *testing.T) {
	prefix := t.TempDir()
	saveDirectory := filepath.Join(prefix, "drive_c", "users", "steamuser", "AppData", "Roaming", "Example", "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "slot.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:     saveprofile.OSLinux,
			Runtime:    target.RuntimeProton,
			PrefixRoot: prefix,
		},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "Example",
		Rules: []saveprofile.Rule{{
			ID: "1", Path: "<winAppData>/Example/Saves", OS: saveprofile.OSWindows, Store: "steam", Kind: "save",
		}},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the Windows rule to resolve inside Proton, got %+v", saves)
	}
}

func TestLudusaviMacProfileResolvesApplicationSupportSave(t *testing.T) {
	home := t.TempDir()
	saveDirectory := filepath.Join(home, "Library", "Application Support", "Example", "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "slot.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Example:
  files:
    <macAppSupport>/Example/Saves:
      tags: [save]
      when:
        - os: mac
          store: steam
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:  saveprofile.OSMacOS,
			Runtime: target.RuntimeNative,
			Home:    home,
		},
	}
	profile, err := profiles.Find(context.Background(), game.Identity)
	if err != nil {
		t.Fatal(err)
	}
	saves, err := saveprofile.Resolve(game, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the macOS Application Support save, got %+v", saves)
	}
}
