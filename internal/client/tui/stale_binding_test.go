package tui

import (
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// A stale save is asked exactly what a diverged one is: sync and choose
// which save becomes current, or fork.
func TestTheStalePromptAsksTheDivergenceSteps(t *testing.T) {
	question := savesync.StaleQuestion{GameTitle: "Slay the Spire 2", OmnisaveName: "Main", ForkName: "Main (Steam Deck)"}

	view := divergedStepView(t, staleSteps(question))

	for _, text := range []string{"Main diverges between this device and the server", "› Sync with Main", "Fork as Main (Steam Deck)"} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the stale prompt to contain %q, got:\n%s", text, view)
		}
	}
}
