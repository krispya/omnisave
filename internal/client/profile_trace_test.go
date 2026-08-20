package client_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	steamtarget "github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	steamlocator "github.com/krisbaumgartner/omnisave/internal/client/target/steam/locator"
)

// A scan that finds nothing has to say why. Without the trace, a game whose
// community entry is missing and a game whose rules point somewhere empty are
// the same silence, and neither can be reported usefully.
func TestScanRecordsWhyAGameFoundNoSave(t *testing.T) {
	steamRoot := t.TempDir()
	writeSteamApp(t, steamRoot, "900002", "Known Game", "KnownGame")
	writeSteamApp(t, steamRoot, "900003", "Unknown Game", "UnknownGame")

	profiles, err := ludusavi.New([]byte(`
Known Game:
  files:
    <base>/Saves:
      tags: [save]
    <winAppData>/KnownGame:
      tags: [save]
      when:
        - os: windows
  steam:
    id: 900002
`))
	if err != nil {
		t.Fatal(err)
	}
	scanner := client.NewScanner(profiles, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	traces := make(map[string]client.ProfileTrace)
	for _, scan := range scans {
		for _, game := range scan.Games {
			traces[game.Game.Identity.Title] = game.Profile
		}
	}
	if len(traces) != 2 {
		t.Fatalf("expected both games to carry a trace, got %+v", traces)
	}

	unknown := traces["Unknown Game"]
	if !unknown.Consulted || unknown.Found {
		t.Errorf("expected the unknown game to record a consulted miss, got %+v", unknown)
	}

	known := traces["Known Game"]
	if !known.Found || known.Provider != "ludusavi" || known.Title != "Known Game" {
		t.Errorf("expected the known game to name its entry, got %+v", known)
	}
	outcomes := make(map[saveprofile.Outcome]saveprofile.RuleOutcome, len(known.Rules))
	for _, rule := range known.Rules {
		outcomes[rule.Outcome] = rule
	}
	missing, searched := outcomes[saveprofile.OutcomeMissing]
	if !searched {
		t.Fatalf("expected the applicable rule to report an absent location, got %+v", known.Rules)
	}
	if missing.Path == "" || !filepath.IsAbs(missing.Path) {
		t.Errorf("expected an absolute path a reader can go and check, got %q", missing.Path)
	}
	if _, excluded := outcomes[saveprofile.OutcomeInapplicable]; !excluded {
		t.Errorf("expected the Windows-only rule to report why it never applied, got %+v", known.Rules)
	}
}

// Rules that found files and were then set aside for the adapter's own save
// must not read as rules that found nothing (FDR-003, decision 10).
func TestScanRecordsRulesSetAsideForAnAdapterSave(t *testing.T) {
	steamRoot := t.TempDir()
	installRoot := writeSteamApp(t, steamRoot, "413150", "Stardew Valley", "Stardew Valley")
	remoteDirectory := filepath.Join(steamRoot, "userdata", "76561198000000000", "413150", "remote")
	if err := os.MkdirAll(remoteDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "progress.sav"), []byte("cloud"), 0600); err != nil {
		t.Fatal(err)
	}
	saveDirectory := filepath.Join(installRoot, "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "progress.sav"), []byte("native"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Stardew Valley:
  files:
    <base>/Saves:
      tags: [save]
  steam:
    id: 413150
`))
	if err != nil {
		t.Fatal(err)
	}
	scanner := client.NewScanner(profiles, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected one game, got %+v", scans)
	}

	trace := scans[0].Games[0].Profile
	if !trace.Suppressed {
		t.Fatalf("expected the trace to record the profile standing aside, got %+v", trace)
	}
	if len(trace.Rules) != 1 || trace.Rules[0].Outcome != saveprofile.OutcomeFound || trace.Rules[0].Files != 1 {
		t.Fatalf("expected the set-aside rule to report what it found, got %+v", trace.Rules)
	}
}

// A scanner with no profile provider must say so rather than imply the
// manifest was consulted and came up empty.
func TestScanWithoutProfilesSaysRulesWereNotConsulted(t *testing.T) {
	steamRoot := t.TempDir()
	writeSteamApp(t, steamRoot, "900004", "Any Game", "AnyGame")

	scanner := client.NewScanner(nil, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected one game, got %+v", scans)
	}
	if trace := scans[0].Games[0].Profile; trace.Consulted || trace.Found {
		t.Fatalf("expected an unconsulted trace, got %+v", trace)
	}
}
