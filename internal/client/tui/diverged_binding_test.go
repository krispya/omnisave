package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The prompt opens on taking current: the answer that adds no save is the
// safe thing to land on when someone confirms without reading.
func TestDivergedBindingPromptOffersTwoLosslessChoices(t *testing.T) {
	choice := DivergedBindingDefault
	question := DivergedQuestion{
		GameTitle:    "Slay the Spire 2",
		OmnisaveName: "Save 1",
		ForkName:     "Save 1 (Steam Deck)",
	}
	form := divergedBindingForm(question, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save 1 diverges between this device and the server",
		"Fork as Save 1 (Steam Deck)",
		"› Jump to current",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the diverged prompt to contain %q, got:\n%s", text, view)
		}
	}
	if strings.Index(view, "Jump to current") > strings.Index(view, "Fork as Save 1 (Steam Deck)") {
		t.Fatalf("expected jump to current to be the first answer, got:\n%s", view)
	}
}

// A Device with no name has no deconflicting name to offer, so the fork
// answer says only that a save appears.
func TestTheForkAnswerFallsBackWithoutADeviceName(t *testing.T) {
	options := DivergedOptions(DivergedQuestion{OmnisaveName: "Save 1"})
	if options[1].Label != "Fork as a new save" {
		t.Fatalf("expected a generic fork label without a device name, got %q", options[1].Label)
	}
	if options[DivergedDefaultIndex(options)].Choice != DivergedBindingJump {
		t.Fatal("expected both surfaces to open on taking current")
	}
}
