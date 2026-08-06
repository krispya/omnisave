package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSyncToDevicePromptOffersEverySaveAndDeferral(t *testing.T) {
	var choice string
	form := syncToDeviceForm("Chrono Trigger", []SyncToDeviceOption{
		{OmnisaveID: "save-1", Name: "Main Playthrough"},
		{OmnisaveID: "save-2", Name: "New Game+"},
	}, &choice).WithWidth(80)
	form.Update(form.Init())

	view := ansi.Strip(form.View())
	for _, text := range []string{
		"Chrono Trigger",
		"No local save exists on this device",
		"Main Playthrough · sync current revision to this device",
		"New Game+ · sync current revision to this device",
		"Decide later · leave this device without a save",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the sync-to-device prompt to contain %q, got:\n%s", text, view)
		}
	}
}
