package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

func divergedStepView(t *testing.T, step DivergedStep) string {
	t.Helper()
	picked := step.Options[0]
	form := divergedStepForm("Slay the Spire 2", step, &picked).WithWidth(80)
	form.Update(form.Init())
	return ansi.Strip(form.View())
}

// The prompt first asks whether the save stays on its omnisave, opening on
// syncing: the answer that adds no save is the safe thing to land on when
// someone confirms without reading.
func TestDivergedPromptFirstAsksWhetherToSyncOrFork(t *testing.T) {
	first := DivergedSteps(savesync.DivergedQuestion{
		GameTitle:    "Slay the Spire 2",
		OmnisaveName: "Main",
		ForkName:     "Main (Steam Deck)",
	})

	view := divergedStepView(t, first)
	for _, text := range []string{
		"Slay the Spire 2",
		"Main diverges between this device and the server",
		"› Sync with Main",
		"Fork as Main (Steam Deck)",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the first step to contain %q, got:\n%s", text, view)
		}
	}
	if first.Options[1].Choice != savesync.DivergedFork || first.Options[1].Next != nil {
		t.Fatalf("expected forking to answer on the first step, got %+v", first.Options[1])
	}
}

// Syncing then asks which save becomes current. It opens on keeping the
// omnisave's current save, which moves nothing other Devices follow.
func TestSyncingAsksWhichSaveBecomesCurrent(t *testing.T) {
	first := DivergedSteps(savesync.DivergedQuestion{GameTitle: "Slay the Spire 2", OmnisaveName: "Main"})
	sync := first.Options[0].Next
	if sync == nil {
		t.Fatal("expected syncing to open a second step")
	}

	view := divergedStepView(t, *sync)
	for _, text := range []string{
		"Which save becomes current on Main?",
		"› Keep the current save",
		"Use this device's save",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the second step to contain %q, got:\n%s", text, view)
		}
	}
	if sync.Options[0].Choice != savesync.DivergedJump || sync.Options[1].Choice != savesync.DivergedUseLocal {
		t.Fatalf("expected the current save then this device's, got %+v", sync.Options)
	}
}

// A Device with no name has no deconflicting name to offer, so the fork
// answer says only that a save appears.
func TestTheForkAnswerFallsBackWithoutADeviceName(t *testing.T) {
	first := DivergedSteps(savesync.DivergedQuestion{OmnisaveName: "Save 1"})
	if first.Options[1].Label != "Fork as a new save" {
		t.Fatalf("expected a generic fork label without a device name, got %q", first.Options[1].Label)
	}
}
