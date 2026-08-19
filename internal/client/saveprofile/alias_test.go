package saveprofile_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// A resolved save carries the identity of every spelling the profile gives
// its location — the other OSes' rules included — so a revision minted under
// another OS's spelling can be recognized as the same place (FDR-003,
// decision 11). Destinations carry the same identities for a save that does
// not exist yet.
func TestResolveCarriesEveryOSSpellingAsAnAlias(t *testing.T) {
	home := t.TempDir()
	saveDirectory := filepath.Join(home, ".config", "UNDERTALE")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "file0"), []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:       "steam:391540",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "391540"}}},
		Environment: target.Environment{
			HostOS:  saveprofile.OSLinux,
			Runtime: target.RuntimeNative,
			Home:    home,
		},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "391540",
		Rules: []saveprofile.Rule{
			{ID: "linux1", Path: "<home>/.config/UNDERTALE", OS: saveprofile.OSLinux, Kind: "save"},
			{ID: "mac1", Path: "<home>/Library/Application Support/com.tobyfox.undertale", OS: saveprofile.OSMacOS, Kind: "save"},
			{ID: "win1", Path: "<winLocalAppData>/UNDERTALE", OS: saveprofile.OSWindows, Kind: "save"},
		},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected one resolved save, got %+v", saves)
	}
	for _, alias := range []string{"linux1", "mac1", "win1"} {
		if !slices.Contains(saves[0].LocationAliases, alias) {
			t.Fatalf("expected alias %q on the resolved save, got %v", alias, saves[0].LocationAliases)
		}
	}

	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || !slices.Contains(destinations[0].LocationAliases, "mac1") {
		t.Fatalf("expected the destination to carry the aliases, got %+v", destinations)
	}
}
