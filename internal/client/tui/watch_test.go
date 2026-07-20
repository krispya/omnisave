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
		lines:   []string{"  ✓ Slay the Spire 2  Up to date", "  · Project Zomboid   No save available"},
		summary: "  2 tracked · up to date",
		at:      time.Now(),
	})

	view := ansi.Strip(updated.(watchModel).View())
	for _, text := range []string{
		"▲ Omnisave · watching",
		"✓ Slay the Spire 2  Up to date",
		"· Project Zomboid   No save available",
		"2 tracked · up to date",
		"watching 3 save files · synced just now",
		"http://localhost:8080",
		"s sync now · q quit",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the watch view to contain %q, got:\n%s", text, view)
		}
	}
}

func TestWatchViewSpinsWhileSyncingAndKeysWork(t *testing.T) {
	requests := make(chan WatchRequest, 1)
	model := newTestWatchModel(requests)
	updated, _ := model.Update(watchPassStartedMsg{})
	if view := ansi.Strip(updated.(watchModel).View()); !strings.Contains(view, "syncing") {
		t.Fatalf("expected the footer to show syncing, got:\n%s", view)
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
