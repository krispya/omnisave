package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStaleBindingPromptUsesGameEventOptionHierarchy(t *testing.T) {
	choice := StaleBindingJump
	question := StaleQuestion{
		GameTitle:    "Slay the Spire 2",
		OmnisaveName: "Save 1",
		ForkName:     "Save 1 (Steam Deck)",
	}
	form := staleBindingForm(question, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save matches a revision of Save 1 that is not its current one",
		"› Jump to current",
		"Fork as Save 1 (Steam Deck)",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the stale binding prompt to contain %q, got:\n%s", text, view)
		}
	}
}
