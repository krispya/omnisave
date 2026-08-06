package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStaleBindingPromptUsesGameEventOptionHierarchy(t *testing.T) {
	choice := StaleBindingJump
	form := staleBindingForm("Slay the Spire 2", "Save 1", &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save matches a revision of Save 1 that is not its current one",
		"› Jump to current",
		"replace local files with the current revision",
		"Fork here",
		"keep these files as a new independent playthrough",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the stale binding prompt to contain %q, got:\n%s", text, view)
		}
	}
}
