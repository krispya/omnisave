package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWaitShowsABrailleActivityLineUntilTheTaskFinishes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	model := newWaitModel(ctx, cancel, &WaitSession{}, "", func(context.Context, *WaitSession) {
		called = true
	})

	view := model.View()
	if model.spinner.Spinner.Frames[0] != "⠋" || strings.TrimSpace(view) != "⠋" {
		t.Fatalf("expected only the braille spinner while the task runs, got %q", view)
	}
	message := model.startTask()()
	updated, _ := model.Update(message)
	completed := updated.(waitModel)
	if !called || !completed.done || completed.View() != "" {
		t.Fatalf("expected the activity line to clear after the task, called=%v done=%v view=%q", called, completed.done, completed.View())
	}
}

func TestWaitAbortKeysCancelTheTaskContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newWaitModel(ctx, cancel, &WaitSession{}, "Syncing with the server", func(context.Context, *WaitSession) {})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	aborted := updated.(waitModel)
	if !aborted.aborted {
		t.Fatalf("expected esc to abort the wait")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("expected the task context to be cancelled on abort")
	}
}

func TestWaitUpdatesItsActivityLabel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newWaitModel(ctx, cancel, &WaitSession{}, "Preparing saves", func(context.Context, *WaitSession) {})

	updated, _ := model.Update(waitLabelMsg("uploading"))
	view := updated.(waitModel).View()
	if !strings.Contains(view, "uploading") || strings.Contains(view, "Preparing saves") {
		t.Fatalf("expected the current activity label, got %q", view)
	}
}
