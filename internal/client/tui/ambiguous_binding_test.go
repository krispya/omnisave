package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestUnmatchedLocalSaveOffersSyncOrCreateWithoutIgnore(t *testing.T) {
	action := unmatchedBindingSync
	form := unmatchedBindingActionForm("Slay the Spire 2", &action).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Unmatched local save",
		"Sync with save",
		"Create a new save",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the unmatched-save prompt to contain %q, got:\n%s", text, view)
		}
	}
	for _, absent := range []string{"Decide later", "Leave this save unsynced"} {
		if strings.Contains(view, absent) {
			t.Fatalf("expected no ignore choice %q, got:\n%s", absent, view)
		}
	}
}

func TestSyncWithSaveListsExistingSaves(t *testing.T) {
	options := []AmbiguousBindingOption{
		{OmnisaveID: "omnisave-1", Name: "Save 1"},
		{OmnisaveID: "omnisave-2", Name: "New Game+"},
	}
	selected := options[0].OmnisaveID
	form := unmatchedBindingSaveForm("Slay the Spire 2", options, &selected).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{"Slay the Spire 2", "Choose a save", "Save 1", "New Game+"} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the save picker to contain %q, got:\n%s", text, view)
		}
	}
}

func TestMultipleMatchesOfferEachSaveOrANewSave(t *testing.T) {
	options := []AmbiguousBindingOption{
		{OmnisaveID: "omnisave-1", Name: "Save 1", MatchedRevisionID: "revision-3"},
		{OmnisaveID: "omnisave-2", Name: "Save 1 (fork)", MatchedRevisionID: "revision-3"},
	}
	selected := options[0].OmnisaveID
	form := multipleMatchesForm("Slay the Spire 2", options, &selected).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Slay the Spire 2",
		"Local save matches more than one save",
		"Save 1",
		"Save 1 (fork)",
		"Create a new save",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the multiple-match prompt to contain %q, got:\n%s", text, view)
		}
	}
}
