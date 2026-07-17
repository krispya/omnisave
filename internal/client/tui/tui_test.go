package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func TestCompletedScanStaysCompactUntilVerboseModeIsRequested(t *testing.T) {
	modified := time.Date(2026, time.July, 16, 14, 30, 0, 0, time.Local)
	game := client.GameScan{
		Game: target.InstalledGame{Identity: target.GameIdentity{Title: "Example Game"}},
		Saves: []target.Save{
			{Kind: "cloud", Files: []target.File{{Path: "/saves/cloud.dat", Size: 1024, Modified: modified}}},
			{Kind: "local", Files: []target.File{
				{Path: "/saves/slot1.dat", Size: 2048, Modified: modified},
				{Path: "/saves/slot2.dat", Size: 1024, Modified: modified},
			}},
		},
	}
	results := []client.TargetScan{{
		Target: target.Target{Adapter: "steam", Source: "installer", Root: "/games/steam"},
		Games:  []client.GameScan{game},
	}}
	adapter := adapterState{name: "steam", stage: adapterComplete, results: results}

	compact := renderAdapter(adapter, false, "")
	if !strings.Contains(compact, "1 target · 1 game · 2 saves") || strings.Contains(strings.Split(compact, "\n")[0], "3 files") {
		t.Fatalf("expected the adapter to show only target, game, and save counts, got %q", compact)
	}
	if !strings.Contains(compact, "Example Game  2 saves · 3 files · 4.0 KiB") {
		t.Fatalf("expected compact mode to summarize each game, got %q", compact)
	}
	if !strings.Contains(compact, "└─ Example Game") {
		t.Fatalf("expected games to be nested beneath their adapter, got %q", compact)
	}
	if strings.Contains(compact, "/saves/slot1.dat") {
		t.Fatalf("expected compact mode to omit individual files, got %q", compact)
	}

	verbose := renderAdapter(adapter, true, "")
	if !strings.Contains(verbose, "Example Game") || !strings.Contains(verbose, "/saves/slot1.dat (2.0 KiB)") {
		t.Fatalf("expected verbose mode to include games and files, got %q", verbose)
	}
}

func TestTrackingChoicesShowAdapterAndPreserveSelections(t *testing.T) {
	scans := []client.TargetScan{
		{
			Target: target.Target{ID: "retroarch:one", Adapter: "retroarch", Source: "installer", Root: "/Applications/RetroArch.app"},
			Games: []client.GameScan{{
				Game: target.InstalledGame{ID: "retroarch:chrono", Identity: target.GameIdentity{Title: "Chrono Trigger"}},
			}},
		},
		{
			Target: target.Target{ID: "steam:one", Adapter: "steam", Source: "installer", Root: "/games/Steam"},
			Games: []client.GameScan{{
				Game: target.InstalledGame{ID: "steam:stardew", Identity: target.GameIdentity{Title: "Stardew Valley"}},
			}},
		},
	}

	groups := trackingChoiceGroups(scans, map[string]bool{"steam:stardew": true})
	if len(groups) != 2 {
		t.Fatalf("expected one group per target, got %+v", groups)
	}
	if groups[0].label != "RetroArch" || groups[0].choices[0].selected {
		t.Fatalf("expected an unselected RetroArch group, got %+v", groups[0])
	}
	if groups[1].label != "Steam" || !groups[1].choices[0].selected {
		t.Fatalf("expected the tracked Steam choice to remain selected, got %+v", groups[1])
	}
}

func TestTrackingPromptKeepsGamesBeforeASelectedGameVisible(t *testing.T) {
	group := gameChoiceGroup{
		label: "Steam",
		choices: []gameChoice{
			{id: "steam:zomboid", label: "Project Zomboid"},
			{id: "steam:spire", label: "Slay the Spire 2"},
			{id: "steam:draw-steel", label: "Draw Steel Codex Playtest", selected: true},
		},
	}
	selected := []string{"steam:draw-steel"}
	field := trackingMultiSelect(group, &selected)
	field.WithTheme(trackingTheme())
	field.WithWidth(100)
	field.Focus()

	view := field.View()
	for _, title := range []string{"Project Zomboid", "Slay the Spire 2", "Draw Steel Codex Playtest"} {
		if !strings.Contains(view, title) {
			t.Fatalf("expected a preselected game to leave every choice visible, got %q", view)
		}
	}
}
