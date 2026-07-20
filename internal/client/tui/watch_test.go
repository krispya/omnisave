package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func newTestWatchModel(requests chan WatchRequest) watchModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	return watchModel{
		config:   WatchConfig{ServerURL: "http://localhost:8080"},
		requests: requests,
		spinner:  indicator,
	}
}

func TestWatchViewShowsTheTableTallyAndFooter(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	updated, _ := model.Update(watchWatchingMsg{files: 3})
	updated, _ = updated.(watchModel).Update(watchPassFinishedMsg{
		snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now().Add(-2 * time.Minute)},
			{Glyph: "·", Title: "Project Zomboid", Events: "No save available"},
		}},
		summary: "  2 tracked · up to date",
		at:      time.Now(),
	})
	clocked, _ := updated.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	for _, text := range []string{
		"▲ Omnisave · watching",
		"✓ Slay the Spire 2  Save 1 · synced 2m ago",
		"· Project Zomboid   No save available",
		"2 tracked · up to date",
		"watching 3 save paths · synced just now",
		"http://localhost:8080",
		"s sync now · q quit",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the watch view to contain %q, got:\n%s", text, view)
		}
	}
}

func TestWatchSpinnerFinishesAFullRotationOnFastPasses(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	started := time.Now()
	updated, _ := model.Update(watchPassStartedMsg{at: started})
	settled, command := updated.(watchModel).Update(watchPassFinishedMsg{at: started.Add(10 * time.Millisecond)})

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
	slowSettled, _ := slow.(watchModel).Update(watchPassFinishedMsg{at: started})
	if slowSettled.(watchModel).syncing {
		t.Fatal("expected a pass longer than the minimum to settle immediately")
	}
}

func TestWatchViewSpinsWhileSyncingAndKeysWork(t *testing.T) {
	requests := make(chan WatchRequest, 1)
	model := newTestWatchModel(requests)
	updated, _ := model.Update(watchPassStartedMsg{})
	syncingView := ansi.Strip(updated.(watchModel).View())
	if !strings.Contains(syncingView, "▲ Omnisave · watching ⠋") {
		t.Fatalf("expected the header to carry the spinner while syncing, got:\n%s", syncingView)
	}
	if !strings.Contains(syncingView, "watching 0 save paths") {
		t.Fatalf("expected the footer to keep its usual text while syncing, got:\n%s", syncingView)
	}

	settled, _ := updated.(watchModel).Update(watchPassFinishedMsg{at: time.Now()})
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
