package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDivergedBindingPromptOffersTwoLosslessChoices(t *testing.T) {
	choice := DivergedBindingFork
	question := DivergedQuestion{
		GameTitle:    "Slay the Spire 2",
		OmnisaveName: "Save 1",
		ForkName:     "Save 1 (Steam Deck)",
		Keep:         KeepAsBranch,
	}
	form := divergedBindingForm(question, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save 1 has new progress on this device and on the server",
		"› Fork here",
		"continue as Save 1 (Steam Deck)",
		"Take current",
		"keep this progress as a branch",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the diverged prompt to contain %q, got:\n%s", text, view)
		}
	}
}

// The options promise only what this save's shape allows: without a Device
// name the fork stays generic, without unsynced content there is nothing to
// keep, and without a baseline the kept progress is a save, not a branch.
func TestDivergedOptionsPromiseWhatEachShapeWouldDo(t *testing.T) {
	nameless := DivergedOptions(DivergedQuestion{OmnisaveName: "Save 1", Keep: KeepAsBranch})
	if nameless[0].Description != "continue as a new playthrough" {
		t.Fatalf("expected a generic fork promise without a device name, got %q", nameless[0].Description)
	}

	held := DivergedOptions(DivergedQuestion{
		OmnisaveName: "Save 1", ForkName: "Save 1 (Steam Deck)", Keep: KeepNothing,
	})
	if held[1].Description != "this progress is already in the history" {
		t.Fatalf("expected the jump to promise no copy for held content, got %q", held[1].Description)
	}

	seeded := DivergedOptions(DivergedQuestion{
		OmnisaveName: "Save 1", ForkName: "Save 1 (Steam Deck)", Keep: KeepAsSave,
	})
	if seeded[1].Description != "keep this progress as Save 1 (Steam Deck)" {
		t.Fatalf("expected the baseline-less jump to promise a new save, got %q", seeded[1].Description)
	}
}
