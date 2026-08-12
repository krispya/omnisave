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
		"Save changed here and Save 1 moved on the server",
		"› Fork here",
		"continue this device's progress as a new playthrough",
		"Take current",
		"keep this progress as a fork and take the current revision",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the diverged prompt to contain %q, got:\n%s", text, view)
		}
	}
}
