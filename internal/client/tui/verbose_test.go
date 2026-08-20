package tui

import (
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func steamGame(title, steamID string) target.InstalledGame {
	return target.InstalledGame{
		ID: "steam:" + steamID,
		Identity: target.GameIdentity{
			Title:       title,
			Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: steamID}},
		},
		Environment: target.Environment{HostOS: saveprofile.OSMacOS},
	}
}

func rendered(games ...client.GameScan) string {
	return renderVerbose([]client.TargetScan{{
		Target: target.Target{Adapter: "steam", Source: "installer", Root: "/games/steam"},
		Games:  games,
	}})
}

// A game the manifest has never heard of must not read the same as a game
// whose rules were followed and came up empty. Telling them apart is the
// whole point of the report: one is an upstream gap, the other is a bug.
func TestVerboseSeparatesAMissingEntryFromAFruitlessSearch(t *testing.T) {
	unknown := client.GameScan{
		Game:    steamGame("Draw Steel", "4003310"),
		Profile: client.ProfileTrace{Consulted: true},
	}
	searched := client.GameScan{
		Game: steamGame("Big Walk", "1478500"),
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Big Walk",
			Rules: []saveprofile.RuleOutcome{{
				Rule:    saveprofile.Rule{ID: "a", Path: "<home>/Library/Big Walk"},
				Outcome: saveprofile.OutcomeMissing,
				Path:    "/Users/player/Library/Big Walk",
			}},
		},
	}

	view := rendered(unknown, searched)
	if !strings.Contains(view, "No save-location rules") ||
		!strings.Contains(view, "No ludusavi entry for steam 4003310") {
		t.Errorf("expected an unknown game to name the missing entry, got %q", view)
	}
	if !strings.Contains(view, "No save found") || !strings.Contains(view, "1 location not found") {
		t.Errorf("expected a searched game to say it searched, got %q", view)
	}
	if !strings.Contains(view, "/Library/Big Walk") {
		t.Errorf("expected the searched path to be named so it can be checked, got %q", view)
	}
}

// The manifest spells one path once per platform. A location another rule
// already searched must not reappear as somewhere discovery skipped.
func TestVerboseDoesNotRepeatALocationItAlreadySearched(t *testing.T) {
	game := client.GameScan{
		Game: steamGame("Project Zomboid", "108600"),
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Project Zomboid",
			Rules: []saveprofile.RuleOutcome{
				{
					Rule:    saveprofile.Rule{ID: "shared", Path: "<home>/Zomboid/Saves"},
					Outcome: saveprofile.OutcomeMissing,
					Path:    homeDirectory() + "/Zomboid/Saves",
				},
				{
					Rule:    saveprofile.Rule{ID: "shared", Path: "<home>/Zomboid/Saves", OS: saveprofile.OSWindows},
					Outcome: saveprofile.OutcomeInapplicable,
				},
				{
					Rule:    saveprofile.Rule{ID: "shared", Path: "<home>/Zomboid/Saves", OS: saveprofile.OSLinux},
					Outcome: saveprofile.OutcomeInapplicable,
				},
			},
		},
	}

	view := rendered(game)
	if strings.Contains(view, "not applicable here") {
		t.Errorf("expected the searched location to suppress its platform twins, got %q", view)
	}
	if count := strings.Count(view, "Zomboid/Saves"); count != 1 {
		t.Errorf("expected the location to appear once, got %d in %q", count, view)
	}
}

// One location excluded under several platforms is still one location.
func TestVerboseCollectsAnExcludedLocationsConstraints(t *testing.T) {
	game := client.GameScan{
		Game: steamGame("Undertale", "391540"),
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Undertale",
			Rules: []saveprofile.RuleOutcome{
				{
					Rule:    saveprofile.Rule{ID: "elsewhere", Path: "<xdgData>/UNDERTALE", OS: saveprofile.OSLinux},
					Outcome: saveprofile.OutcomeInapplicable,
				},
				{
					Rule:    saveprofile.Rule{ID: "elsewhere", Path: "<xdgData>/UNDERTALE", OS: saveprofile.OSWindows},
					Outcome: saveprofile.OutcomeInapplicable,
				},
			},
		},
	}

	view := rendered(game)
	if !strings.Contains(view, "1 rule not applicable here") {
		t.Errorf("expected the platform twins to count as one rule, got %q", view)
	}
	if !strings.Contains(view, "for linux, for windows") {
		t.Errorf("expected both constraints on one line, got %q", view)
	}
}

// A template that only restates the expanded path is the same sentence twice.
func TestVerboseShowsATemplateOnlyWhenItAddsSomething(t *testing.T) {
	game := client.GameScan{
		Game: steamGame("Example", "1"),
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Example",
			Rules: []saveprofile.RuleOutcome{
				{
					Rule:    saveprofile.Rule{ID: "plain", Path: "<home>/Library/Example"},
					Outcome: saveprofile.OutcomeMissing,
					Path:    homeDirectory() + "/Library/Example",
				},
				{
					Rule:    saveprofile.Rule{ID: "account", Path: "<home>/Library/Other/<storeUserId>"},
					Outcome: saveprofile.OutcomeMissing,
					Path:    homeDirectory() + "/Library/Other/*",
				},
			},
		},
	}

	view := rendered(game)
	if strings.Contains(view, "<home>/Library/Example") {
		t.Errorf("expected a template restating its path to be dropped, got %q", view)
	}
	if !strings.Contains(view, "<storeUserId>") {
		t.Errorf("expected a template naming a placeholder to survive, got %q", view)
	}
}

// A save the rules found and the scanner then set aside is not a save the
// rules failed to find, and the report has to say which happened.
func TestVerboseReportsRulesSetAsideByAnAdapterSave(t *testing.T) {
	game := client.GameScan{
		Game: steamGame("Slay the Spire 2", "2868840"),
		Saves: []target.Save{{
			Kind:  "cloud",
			Files: []target.File{{Path: "/steam/userdata/1/2868840/remote/profile.save", Size: 49}},
		}},
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Slay the Spire 2",
			Suppressed: true,
			Rules: []saveprofile.RuleOutcome{{
				Rule:    saveprofile.Rule{ID: "native", Path: "<home>/Library/SlayTheSpire2"},
				Outcome: saveprofile.OutcomeFound,
				Path:    "/Users/player/Library/SlayTheSpire2",
				Files:   356,
			}},
		},
	}

	view := rendered(game)
	if !strings.Contains(view, "Rules set aside") {
		t.Errorf("expected the suppression to be reported, got %q", view)
	}
	if !strings.Contains(view, "356 files") {
		t.Errorf("expected the set-aside rule to report what it found, got %q", view)
	}
	if !strings.Contains(view, "cloud save from the adapter") {
		t.Errorf("expected the adapter's own save to be named, got %q", view)
	}
}

// A save holding hundreds of files must not bury the games under it.
func TestVerboseCapsTheFilesItNames(t *testing.T) {
	files := make([]target.File, 0, 12)
	for index := range 12 {
		files = append(files, target.File{
			Path: "/saves/file" + string(rune('a'+index)), Size: 1, LocationID: "one",
		})
	}
	game := client.GameScan{
		Game: steamGame("Example", "1"),
		Saves: []target.Save{{
			Kind:     "local",
			Files:    files,
			Metadata: map[string]string{"profile_provider": "ludusavi"},
		}},
		Profile: client.ProfileTrace{
			Consulted: true, Found: true, Provider: "ludusavi", Title: "Example",
			Rules: []saveprofile.RuleOutcome{{
				Rule:    saveprofile.Rule{ID: "one", Path: "<home>/saves"},
				Outcome: saveprofile.OutcomeFound,
				Path:    "/saves",
				Files:   12,
			}},
		},
	}

	view := rendered(game)
	if !strings.Contains(view, "and 7 more") {
		t.Errorf("expected the listing to be capped, got %q", view)
	}
	if strings.Contains(view, "filel") {
		t.Errorf("expected files past the cap to be omitted, got %q", view)
	}
}
