package savesync_test

import (
	"context"
	"slices"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
)

// A pass is the whole of one sync run: it scans through the adapters, tracks
// what it found, reconciles every save, and names the files whose change
// should start the next one.
func TestAPassSyncsWhatItScansAndNamesTheFilesToWatch(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	ports := savesync.Ports{
		Server:   server,
		Adapters: client.NewScanner(nil, savesynctest.Adapter{Fixture: &fixture}),
		Report:   &savesynctest.Recorder{},
	}

	result, err := savesync.Pass(context.Background(), ports, &fixture.State, savesync.PassOptions{})

	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome.Seeded != 1 || result.Outcome.Failed != 0 {
		t.Fatalf("expected the pass to seed the save it scanned, got %+v", result.Outcome)
	}
	if !slices.Contains(result.Watched, fixture.LocalPath) {
		t.Fatalf("expected the save's file watched, got %q", result.Watched)
	}
	// Without a detector nothing was swept, which is not the same as
	// nothing being played: presence must not be reported from it.
	if result.Played.Swept || !result.Played.Presence.Reportable() {
		t.Fatalf("expected an unswept pass with a reportable presence, got %+v", result.Played)
	}
}
