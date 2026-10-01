package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

func TestUnmatchedLocalSaveOffersSyncOrCreateWithoutIgnore(t *testing.T) {
	action := unmatchedBindingSync
	options := []savesync.AmbiguousOption{{OmnisaveID: "omnisave-1", Name: "Save 1"}, {OmnisaveID: "omnisave-2", Name: "New Game+"}}
	form := unmatchedBindingActionForm("Slay the Spire 2", options, &action).WithWidth(80)
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

// A lone existing save is named in the first step, so syncing with it needs
// no picker and goes straight to which save becomes current.
func TestALoneExistingSaveIsNamedInTheFirstStep(t *testing.T) {
	action := unmatchedBindingSync
	options := []savesync.AmbiguousOption{{OmnisaveID: "omnisave-1", Name: "Main"}}

	form := unmatchedBindingActionForm("Slay the Spire 2", options, &action).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	if !strings.Contains(view, "› Sync with Main") || !strings.Contains(view, "Create a new save") {
		t.Fatalf("expected the first step to name the lone save, got:\n%s", view)
	}
	step := currentStep("Main")
	if view := divergedStepView(t, *step); !strings.Contains(view, "Which save becomes current on Main?") {
		t.Fatalf("expected syncing to ask which save becomes current, got:\n%s", view)
	}
	if step.Options[1].Choice != savesync.DivergedUseLocal {
		t.Fatalf("expected the second option to use this device's save, got %+v", step.Options)
	}
}

func TestSyncWithSaveListsExistingSaves(t *testing.T) {
	options := []savesync.AmbiguousOption{
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
	options := []savesync.AmbiguousOption{
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
