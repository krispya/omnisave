package savesync_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// finishingAdapter is a target whose store has bookkeeping to settle after
// a placement, standing in for Steam's cloud file registry.
type finishingAdapter struct {
	finished []target.Save
	removed  [][]string
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

func (a *finishingAdapter) FinishPlacement(_ context.Context, _ target.Target, _ target.InstalledGame, save target.Save, removed []string) (target.PlacementReport, error) {
	a.finished = append(a.finished, save)
	a.removed = append(a.removed, removed)
	return target.PlacementReport{Registered: []string{"file.save"}}, nil
}

// pullOnFinishingTarget binds the fixture's save on a finishing target at
// baseline, under a lineage whose current revision has moved on to current,
// so the next pass pulls.
func pullOnFinishingTarget(t *testing.T, baseline, current omnisave.Revision, artifacts map[string]string) (savesynctest.Fixture, *remote.Client) {
	t.Helper()
	fixture := savesynctest.NewFixture(t, "old-progress")
	fixture.Scans[0].Target.Adapter = "finishing"
	game := fixture.State.Games["local-game-1"]
	game.Adapter = "finishing"
	fixture.State.Games["local-game-1"] = game
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Journey", CurrentRevisionID: &current.ID,
	}
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{baseline, current})
		case strings.HasPrefix(request.URL.Path, "/api/v1/artifacts/"):
			content, known := artifacts[strings.TrimPrefix(request.URL.Path, "/api/v1/artifacts/")]
			if !known {
				http.NotFound(response, request)
				return
			}
			_, _ = response.Write([]byte(content))
		default:
			http.NotFound(response, request)
		}
	})
	if err := fixture.State.Bind(fixture.Local(), remoteSave.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.State.RecordSynced(fixture.Local(), remoteSave.ID, baseline.ID); err != nil {
		t.Fatal(err)
	}
	return fixture, server
}

// A pull that places the Current Revision into the local save must also let
// the adapter settle the store's bookkeeping, or a game that trusts the
// store's registry over its own folder discards the pull at launch
// (FDR-005).
func TestAPullFinishesThePlacementWithTheAdapter(t *testing.T) {
	baseline := testRevision("revision-1", "omnisave-1", "old-progress")
	current := testRevision("revision-2", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{
		current.Files[0].Artifact.SHA256: "new-progress",
	})
	adapter := &finishingAdapter{}

	outcome, report := reconcileWith(t, server, client.NewScanner(nil, adapter), &fixture, savesync.Options{})

	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one pull, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "new-progress" {
		t.Fatalf("local save = %q", content)
	}
	if len(adapter.finished) != 1 || adapter.finished[0].ID != fixture.Save.ID {
		t.Fatalf("finished placements = %+v", adapter.finished)
	}
	if registered := report.Of("StoreRegistered"); len(registered) != 1 || registered[0].Count != 1 {
		t.Fatalf("expected the registration reported, got %+v", report.Events)
	}
}

// The measured failure (FDR-005, decision 13): a rewound revision can carry live state
// the local save had lost — a run file deleted by an abandon — and it is
// exactly that file the store must be told about. The finisher therefore
// has to see what the apply left on disk, not what discovery found before
// it ran.
func TestAPullHandsTheFinisherFilesTheRevisionRestored(t *testing.T) {
	baseline := testRevision("revision-1", "omnisave-1", "old-progress")
	current := testRevision("revision-2", "omnisave-1", "new-progress")
	// The rewind resurrects a file the local save no longer has.
	current.Files = append(current.Files, omnisave.RevisionFile{
		Path: "battery/current_run.save", Artifact: artifactOf("restored-run"),
	})
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{
		current.Files[0].Artifact.SHA256: "new-progress",
		current.Files[1].Artifact.SHA256: "restored-run",
	})
	adapter := &finishingAdapter{}

	outcome, _ := reconcileWith(t, server, client.NewScanner(nil, adapter), &fixture, savesync.Options{})

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

// A native snapshot after a run ends carries its history file. Rewinding to
// the active run removes that history locally and must tell the cloud to do
// the same; otherwise the next launch resurrects it and rejects the run.
func TestARewindHandsTheFinisherOnlyFilesThePlacementRemoved(t *testing.T) {
	baseline := testRevision("finished-run", "omnisave-1", "old-progress")
	baseline.Files = append(baseline.Files, omnisave.RevisionFile{
		Path: "battery/history/finished.run", Artifact: artifactOf("finished"),
	})
	current := testRevision("active-run", "omnisave-1", "new-progress")
	current.Files = append(current.Files, omnisave.RevisionFile{
		Path: "battery/current_run.save", Artifact: artifactOf("active"),
	})
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{
		current.Files[0].Artifact.SHA256: "new-progress",
		current.Files[1].Artifact.SHA256: "active",
	})
	root := filepath.Dir(fixture.LocalPath)
	removed := filepath.Join(root, "history", "finished.run")
	if err := os.MkdirAll(filepath.Dir(removed), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(removed, []byte("finished"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture.Save.Files = append(fixture.Save.Files, target.File{
		Path: removed, LocationID: "battery", RelativePath: "history/finished.run", Size: 8,
	})
	fixture.Scans[0].Games[0].Saves[0] = fixture.Save
	adapter := &finishingAdapter{}

	outcome, _ := reconcileWith(t, server, client.NewScanner(nil, adapter), &fixture, savesync.Options{})

	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected a rewind, got %+v", outcome)
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("finished history remains on disk: %v", err)
	}
	if !reflect.DeepEqual(adapter.removed, [][]string{{removed}}) {
		t.Fatalf("cloud removal evidence = %v", adapter.removed)
	}
	active, err := os.ReadFile(filepath.Join(root, "current_run.save"))
	if err != nil || string(active) != "active" {
		t.Fatalf("active run was not restored: %q, %v", active, err)
	}
}
