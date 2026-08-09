package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func newTestWatchModel(requests chan WatchRequest) watchModel {
	return newWatchModel(WatchConfig{ServerURL: "http://localhost:8080"}, requests)
}

// A run that reconciled before opening the view hands over what it found, so
// the first frame is the finished table with a footer that already proves
// the run reached the server — nothing waits for the next pass.
func TestAHandOffOpensOnTheTableAndClockItWasGiven(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Main", SyncedAt: time.Now().Add(-time.Hour)},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
		Synced: time.Now().Add(-11 * time.Minute),
	}, make(chan WatchRequest, 1))

	watching, _ := model.Update(watchWatchingMsg{files: 231})
	clocked, _ := watching.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	for _, text := range []string{
		"✓ Slay the Spire 2  Main · synced 1h ago",
		"· Project Zomboid   No save available",
		"watching 231 save paths · synced 11m ago · http://localhost:8081",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the opening frame to contain %q, got:\n%s", text, view)
		}
	}
}

func TestWatchViewShowsTheTableTallyAndFooter(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	updated, _ := model.Update(watchWatchingMsg{files: 3})
	updated, _ = updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now().Add(-2 * time.Minute)},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
		Summary: "  2 tracked · up to date",
		At:      time.Now(),
	}})
	clocked, _ := updated.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	for _, text := range []string{
		"▲ Omnisave · watching",
		"✓ Slay the Spire 2  Save 1 · synced 2m ago",
		"· Project Zomboid   No save available",
		"watching 3 save paths · synced just now",
		"http://localhost:8080",
		"s sync now · q quit",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the watch view to contain %q, got:\n%s", text, view)
		}
	}
}

func TestEventScrollbackLeavesSpaceBeforeTheWatchView(t *testing.T) {
	at := time.Date(2026, 8, 9, 0, 23, 0, 0, time.Local)
	lines := eventScrollbackLines([]Event{
		{Glyph: "○", Title: "Slay the Spire 2", Sentence: "waiting for game to close", At: at},
		{Glyph: "✓", Title: "Slay the Spire 2", Sentence: "synced with Main", At: at},
	})

	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("expected one blank line after the event batch, got %q", lines)
	}
	if strings.Contains(lines[0], "\n") || strings.Contains(lines[1], "\n") {
		t.Fatalf("expected events to stay adjacent, got %q", lines)
	}
	if idle := eventScrollbackLines(nil); len(idle) != 0 {
		t.Fatalf("expected an idle pass not to add a separator, got %q", idle)
	}
}

func TestWatchSpinnerFinishesAFullRotationOnFastPasses(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	started := time.Now()
	updated, _ := model.Update(watchPassStartedMsg{at: started})
	settled, command := updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: started.Add(10 * time.Millisecond)}})

	fast := settled.(watchModel)
	if !fast.syncing || command == nil {
		t.Fatalf("expected a fast pass to keep spinning until the minimum, syncing=%v", fast.syncing)
	}
	// A newer pass makes the pending settle stale; its arrival must not
	// stop the fresh spinner.
	restarted, _ := fast.Update(watchPassStartedMsg{at: started.Add(20 * time.Millisecond)})
	ignored, _ := restarted.(watchModel).Update(watchSpinDoneMsg{generation: fast.spinGeneration})
	if !ignored.(watchModel).syncing {
		t.Fatal("expected a stale settle timer to be ignored")
	}
	current, _ := ignored.(watchModel).Update(watchSpinDoneMsg{generation: ignored.(watchModel).spinGeneration})
	if current.(watchModel).syncing {
		t.Fatal("expected the current settle timer to stop the spinner")
	}

	slow, _ := newTestWatchModel(make(chan WatchRequest, 1)).Update(watchPassStartedMsg{at: started.Add(-2 * time.Second)})
	slowSettled, _ := slow.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: started}})
	if slowSettled.(watchModel).syncing {
		t.Fatal("expected a pass longer than the minimum to settle immediately")
	}
}

func TestAnOutageKeepsTheLastTableAndClockAndShowsTheCause(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	synced := time.Now().Add(-2 * time.Minute)
	settled, _ := model.Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: synced},
		}},
		At: synced,
	}})

	// The server goes away: the pass reaches no one and reports no games.
	offline, _ := settled.(watchModel).Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{General: []string{"Library sync failed — connection refused"}},
		At:       time.Now(),
	}})
	clocked, _ := offline.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	if !strings.Contains(view, "✓ Slay the Spire 2  Save 1 · synced 2m ago") {
		t.Fatalf("expected the outage to leave the last known table, got:\n%s", view)
	}
	if !strings.Contains(view, "✗ Library sync failed — connection refused") {
		t.Fatalf("expected the outage shown as the current condition, got:\n%s", view)
	}
	if strings.Contains(view, "synced just now") {
		t.Fatalf("expected a failed pass not to claim a fresh sync, got:\n%s", view)
	}
}

func TestWatchViewSpinsWhileSyncingAndKeysWork(t *testing.T) {
	requests := make(chan WatchRequest, 1)
	model := newTestWatchModel(requests)
	updated, _ := model.Update(watchPassStartedMsg{})
	syncingView := ansi.Strip(updated.(watchModel).View())
	header, _, _ := strings.Cut(syncingView, "\n")
	if header != "▲ Omnisave · watching ⠋" {
		t.Fatalf("expected the active header to end at the spinner, got %q", header)
	}
	if !strings.Contains(syncingView, "watching 0 save paths") {
		t.Fatalf("expected the footer to keep its usual text while syncing, got:\n%s", syncingView)
	}

	settled, _ := updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: time.Now()}})
	settled.(watchModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	select {
	case request := <-requests:
		if request != WatchSyncNow {
			t.Fatalf("expected a sync-now request, got %v", request)
		}
	default:
		t.Fatal("expected pressing s to request a sync")
	}

	_, command := settled.(watchModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command == nil {
		t.Fatal("expected q to quit the view")
	}
}

// Presence rides the standing table without waiting for a pass: a live
// session claims the game's state glyph, and the glyph returns when the
// report empties.
func TestWatchMarksPlayingGamesWithTheirGlyph(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Main", SyncedAt: time.Now()},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
	}, make(chan WatchRequest, 1))

	playing, _ := model.Update(watchPlayingMsg{"Slay the Spire 2"})
	view := ansi.Strip(playing.(watchModel).View())
	if !strings.Contains(view, "▶ Slay the Spire 2") {
		t.Fatalf("expected the playing glyph to claim the row, got:\n%s", view)
	}
	if strings.Contains(view, "✓ Slay the Spire 2") {
		t.Fatalf("expected the state glyph replaced while playing, got:\n%s", view)
	}

	cleared, _ := playing.(watchModel).Update(watchPlayingMsg(nil))
	view = ansi.Strip(cleared.(watchModel).View())
	if strings.Contains(view, "▶") || !strings.Contains(view, "✓ Slay the Spire 2") {
		t.Fatalf("expected the state glyph back once play stops, got:\n%s", view)
	}
}
