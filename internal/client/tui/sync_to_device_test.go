package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

func TestSyncToDevicePromptOffersEverySaveWithConciseLabels(t *testing.T) {
	var choice string
	form := syncToDeviceForm("Chrono Trigger", []savesync.SyncToDeviceOption{
		{OmnisaveID: "save-1", Name: "Main Playthrough"},
		{OmnisaveID: "save-2", Name: "New Game+"},
	}, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Chrono Trigger",
		"No local save exists on this device",
		"Main Playthrough",
		"New Game+",
		"Leave this device without a save",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the sync-to-device prompt to contain %q, got:\n%s", text, view)
		}
	}
	for _, description := range []string{"sync current revision"} {
		if strings.Contains(view, description) {
			t.Fatalf("expected choices without the description %q, got:\n%s", description, view)
		}
	}
}

func TestSlotPlacementAsksAboutOneSaveWithTheSlotsAsAnswers(t *testing.T) {
	options := []savesync.SyncToDeviceOption{
		{OmnisaveID: "history", Name: "Co-op run", DestinationID: "slot2", DestinationLabel: "Profile 2"},
		{OmnisaveID: "history", Name: "Co-op run", DestinationID: "slot3", DestinationLabel: "Profile 3"},
		{OmnisaveID: "other", Name: "Solo run", DestinationID: "slot2", DestinationLabel: "Profile 2"},
	}
	saves := placementSaves(options)
	if len(saves) != 2 || len(saves[0]) != 2 || saves[1][0].Name != "Solo run" {
		t.Fatalf("expected one question per save, got %+v", saves)
	}
	var selected string
	form := slotPlacementForm("Slay the Spire 2", saves[0], &selected).WithWidth(80)
	form.Update(form.Init())
	view := ansi.Strip(form.View())
	for _, text := range []string{"Put Co-op run in an empty slot?", "Profile 2", "Profile 3", "No"} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected %q in the slot placement prompt, got:\n%s", text, view)
		}
	}
	if strings.Contains(view, "Solo run") {
		t.Fatal("one question named two saves")
	}
}
