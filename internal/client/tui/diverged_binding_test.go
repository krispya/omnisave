package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDivergedBindingPromptOffersTwoLosslessChoices(t *testing.T) {
	choice := DivergedBindingFork
	form := divergedBindingForm("Slay the Spire 2", "Save 1", &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save has new progress here and on Save 1",
		"› Fork here",
		"continue this device's progress as a new playthrough",
		"Jump to latest",
		"keep this progress as a fork and take the latest revision",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the diverged prompt to contain %q, got:\n%s", text, view)
		}
	}
}
