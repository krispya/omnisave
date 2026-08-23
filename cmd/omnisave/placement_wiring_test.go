package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// finishingAdapter is a target whose store has bookkeeping to settle after
// a placement, standing in for Steam's cloud file registry.
type finishingAdapter struct {
	finished []target.Save
}

func (a *finishingAdapter) Name() string { return "finishing" }

func (a *finishingAdapter) DiscoverTargets(context.Context) ([]target.Target, error) { return nil, nil }

func (a *finishingAdapter) DiscoverGames(context.Context, target.Target) ([]target.InstalledGame, error) {
	return nil, nil
}

func (a *finishingAdapter) DiscoverSaves(context.Context, target.Target, target.InstalledGame) ([]target.Save, error) {
	return nil, nil
}

func (a *finishingAdapter) DiscoverSaveDestinations(context.Context, target.Target, target.InstalledGame) ([]target.SaveDestination, error) {
	return nil, nil
}

func (a *finishingAdapter) FinishPlacement(_ context.Context, _ target.Target, _ target.InstalledGame, save target.Save) (target.PlacementReport, error) {
	a.finished = append(a.finished, save)
	return target.PlacementReport{Registered: []string{"file.save"}}, nil
}

// A pull that places the Current Revision into the local save must also let
// the adapter settle the store's bookkeeping, or a game that trusts the
// store's registry over its own folder discards the pull at launch
// (FDR-005).
func TestAPullFinishesThePlacementWithTheAdapter(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	fixture.scans[0].Target.Adapter = "finishing"
	game := fixture.state.Games["local-game-1"]
	game.Adapter = "finishing"
	fixture.state.Games["local-game-1"] = game
	adapter := &finishingAdapter{}
	scanner := client.NewScanner(nil, adapter)

	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Journey", CurrentRevisionID: &currentID,
	}
	baseline := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{baseline, current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("new-progress"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, remoteSave.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.state.RecordSynced(local, remoteSave.ID, baseline.ID); err != nil {
		t.Fatal(err)
	}

	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	err = reconcileSaves(context.Background(), scanner, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one pull, got %+v", outcome)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-progress" {
		t.Fatalf("local save = %q", content)
	}
	if len(adapter.finished) != 1 || adapter.finished[0].ID != fixture.save.ID {
		t.Fatalf("finished placements = %+v", adapter.finished)
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "Steam Cloud") {
		t.Fatalf("expected a registration sentence in the report:\n%s", lines)
	}
}

// The measured failure (FDR-005): a rewound revision can carry live state
// the local save had lost — a run file deleted by an abandon — and it is
// exactly that file the store must be told about. The finisher therefore
// has to see what the apply left on disk, not what discovery found before
// it ran.
func TestAPullHandsTheFinisherFilesTheRevisionRestored(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	fixture.scans[0].Target.Adapter = "finishing"
	game := fixture.state.Games["local-game-1"]
	game.Adapter = "finishing"
	fixture.state.Games["local-game-1"] = game
	adapter := &finishingAdapter{}
	scanner := client.NewScanner(nil, adapter)

	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Journey", CurrentRevisionID: &currentID,
	}
	baseline := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	// The rewind resurrects a file the local save no longer has.
	restoredDigest := sha256.Sum256([]byte("restored-run"))
	current.Files = append(current.Files, omnisave.RevisionFile{
		Path: "battery/current_run.save",
		Artifact: omnisave.Artifact{
			Format: "application/octet-stream",
			SHA256: hex.EncodeToString(restoredDigest[:]),
			Size:   int64(len("restored-run")),
		},
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{baseline, current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("new-progress"))
		case "/api/v1/artifacts/" + current.Files[1].Artifact.SHA256:
			_, _ = response.Write([]byte("restored-run"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, remoteSave.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.state.RecordSynced(local, remoteSave.ID, baseline.ID); err != nil {
		t.Fatal(err)
	}

	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	err = reconcileSaves(context.Background(), scanner, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one pull, got %+v", outcome)
	}
	if len(adapter.finished) != 1 {
		t.Fatalf("finished placements = %+v", adapter.finished)
	}
	restored := ""
	for _, file := range adapter.finished[0].Files {
		if strings.HasSuffix(file.Path, "current_run.save") {
			restored = file.Path
		}
	}
	if restored == "" {
		t.Fatalf("the finisher never saw the restored file; got %+v", adapter.finished[0].Files)
	}
	content, err := os.ReadFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "restored-run" {
		t.Fatalf("restored file = %q", content)
	}
}
