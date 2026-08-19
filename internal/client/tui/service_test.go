package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestInstallServicePromptUsesConciseChoiceLabels(t *testing.T) {
	choice := installServiceYes
	form := installServiceForm(&choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Background syncing",
		"Keep syncing after you close this terminal?",
		"Yes",
		"No",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the install-service prompt to contain %q, got:\n%s", text, view)
		}
	}
	for _, description := range []string{"run Omnisave in the background", "sync when I run omnisave myself"} {
		if strings.Contains(view, description) {
			t.Fatalf("expected choices without the description %q, got:\n%s", description, view)
		}
	}
}
