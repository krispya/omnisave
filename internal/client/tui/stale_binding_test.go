package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStaleBindingPromptUsesGameEventOptionHierarchy(t *testing.T) {
	choice := StaleBindingFastForward
	form := staleBindingForm("Slay the Spire 2", "Save 1", &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save matches an older revision of Save 1",
		"› Jump to latest",
		"replace local files with the latest revision",
		"Fork here",
		"keep these files as a new independent playthrough",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the stale binding prompt to contain %q, got:\n%s", text, view)
		}
	}
}
