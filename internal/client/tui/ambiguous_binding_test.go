package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAmbiguousBindingPromptMarksMatchesAndOffersSeedAndDeferral(t *testing.T) {
	var choice string
	form := ambiguousBindingForm("Slay the Spire 2", []AmbiguousBindingOption{
		{OmnisaveID: "omnisave-1", Name: "Save 1", MatchedRevisionID: "revision-3"},
		{OmnisaveID: "omnisave-2", Name: "Save 1 (fork)", MatchedRevisionID: "revision-3"},
	}, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Save matches more than one Omnisave",
		"Save 1 · matches this save",
		"Save 1 (fork) · matches this save",
		"Seed a new Omnisave · start syncing from this save",
		"Decide later · leave this save unsynced",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the ambiguous binding prompt to contain %q, got:\n%s", text, view)
		}
	}
}

func TestAmbiguousBindingPromptNamesTheNoMatchSituation(t *testing.T) {
	var choice string
	form := ambiguousBindingForm("Slay the Spire 2", []AmbiguousBindingOption{
		{OmnisaveID: "omnisave-1", Name: "Save 1"},
	}, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	if !strings.Contains(view, "Save matches none of this game's Omnisaves") ||
		!strings.Contains(view, "Save 1 · different content") {
		t.Fatalf("expected the no-match situation to be named, got:\n%s", view)
	}
}
