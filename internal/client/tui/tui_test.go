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
	if !strings.Contains(compact, "1 target · 1 game · 2 saves · 3 files · 4.0 KiB") {
		t.Fatalf("expected compact save statistics, got %q", compact)
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
