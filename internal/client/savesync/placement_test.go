package savesync_test

import (
	"context"
	"errors"
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
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// finishingAdapter is a target whose store has bookkeeping to settle after
// a placement, standing in for Steam's cloud file registry.
type finishingAdapter struct {
	failure  error
	finished []target.Save
	removed  [][]string
	before   []map[string]string
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

func (a *finishingAdapter) FinishPlacement(_ context.Context, _ target.Target, _ target.InstalledGame, save target.Save, evidence target.PlacementEvidence) (target.PlacementReport, error) {
	a.finished = append(a.finished, save)
	a.removed = append(a.removed, evidence.Removed)
	a.before = append(a.before, evidence.Before)
	if a.failure != nil {
		return target.PlacementReport{}, a.failure
	}
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

// Steam is unavailable after disk placement. A restart must resume cloud work,
// retaining the old baseline until the entire restore succeeds.
func TestAPendingCloudRestoreSurvivesRestart(t *testing.T) {
	baseline := testRevision("before", "omnisave-1", "old-progress")
	baseline.Files = append(baseline.Files, omnisave.RevisionFile{Path: "battery/finished.run", Artifact: artifactOf("finished")})
	current := testRevision("after", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
	removed := filepath.Join(filepath.Dir(fixture.LocalPath), "finished.run")
	if err := os.WriteFile(removed, []byte("finished"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture.Save.Files = append(fixture.Save.Files, target.File{Path: removed, LocationID: "battery", RelativePath: "finished.run", Size: 8})
	fixture.Scans[0].Games[0].Saves[0] = fixture.Save
	adapter := &finishingAdapter{failure: errors.New("Steam unavailable")}
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	report := &savesynctest.Recorder{}
	ports := savesync.Ports{Server: server, Adapters: client.NewScanner(nil, adapter), Report: report, Checkpoint: store.Save}
	outcome := savesync.Outcome{}
	if err := savesync.Reconcile(context.Background(), ports, &fixture.State, fixture.Scans, map[string]bool{"local-game-1": true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 1 || len(report.Of("SyncedWith")) != 0 {
		t.Fatalf("unfinished cloud work reported synced: %+v", outcome)
	}
	if fixture.Read(t) != "new-progress" {
		t.Fatal("local placement did not finish")
	}
	var err error
	fixture.State, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := fixture.State.BindingFor(fixture.Local())
	if *bound.LastSyncedRevisionID != baseline.ID {
		t.Fatal("failed restore advanced baseline")
	}
	if _, ok := fixture.State.PlacementFor(fixture.Local()); !ok {
		t.Fatal("pending restore was not persisted")
	}
	// Discovery after restart no longer sees the removed history file.
	fixture.Scans[0].Games[0].Saves[0] = adapter.finished[0]
	adapter.failure = nil
	outcome = savesync.Outcome{}
	if err := savesync.Reconcile(context.Background(), ports, &fixture.State, fixture.Scans, map[string]bool{"local-game-1": true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 0 || outcome.Pulled != 1 || len(adapter.finished) != 2 {
		t.Fatalf("retry did not complete: %+v", outcome)
	}
	if !reflect.DeepEqual(adapter.removed, [][]string{{removed}, {removed}}) || adapter.before[1][removed] != artifactOf("finished").SHA256 {
		t.Fatal("restart lost preserved cloud deletion evidence")
	}
	fixture.State, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fixture.State.PlacementFor(fixture.Local()); ok {
		t.Fatal("completed restore remains pending")
	}
	bound, _ = fixture.State.BindingFor(fixture.Local())
	if *bound.LastSyncedRevisionID != current.ID {
		t.Fatal("completed restore did not advance baseline")
	}
}

func TestARestoreRefusesToPlaceWithoutADurableJournal(t *testing.T) {
	baseline := testRevision("before", "omnisave-1", "old-progress")
	current := testRevision("after", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
	adapter := &finishingAdapter{}
	ports := savesync.Ports{Server: server, Adapters: client.NewScanner(nil, adapter), Report: &savesynctest.Recorder{}, Checkpoint: func(tracking.State) error { return errors.New("disk full") }}
	outcome := savesync.Outcome{}
	if err := savesync.Reconcile(context.Background(), ports, &fixture.State, fixture.Scans, map[string]bool{"local-game-1": true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	// The next pass in the same process must not trust an unpersisted journal.
	outcome = savesync.Outcome{}
	if err := savesync.Reconcile(context.Background(), ports, &fixture.State, fixture.Scans, map[string]bool{"local-game-1": true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 1 || fixture.Read(t) != "old-progress" || len(adapter.finished) != 0 {
		t.Fatalf("restore proceeded without journal: %+v", outcome)
	}
}

func TestAPendingRestoreDoesNotOverwriteNewLocalProgress(t *testing.T) {
	baseline := testRevision("before", "omnisave-1", "old-progress")
	current := testRevision("after", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
	adapter := &finishingAdapter{failure: errors.New("Steam unavailable")}
	scanner := client.NewScanner(nil, adapter)
	reconcileWith(t, server, scanner, &fixture, savesync.Options{})
	fixture.Write(t, "played-on")
	adapter.failure = nil
	outcome, _ := reconcileWith(t, server, scanner, &fixture, savesync.Options{})
	if outcome.Failed != 1 || len(adapter.finished) != 1 || fixture.Read(t) != "played-on" {
		t.Fatal("pending restore overwrote later progress")
	}
}

func TestAPendingRestoreWaitsForTheGameToClose(t *testing.T) {
	baseline := testRevision("before", "omnisave-1", "old-progress")
	current := testRevision("after", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
	adapter := &finishingAdapter{failure: errors.New("Steam unavailable")}
	scanner := client.NewScanner(nil, adapter)
	reconcileWith(t, server, scanner, &fixture, savesync.Options{})
	adapter.failure = nil
	outcome, _ := reconcileWith(t, server, scanner, &fixture, savesync.Options{Gate: savesync.NewPullGate(map[string]bool{"local-game-1": true})})
	if outcome.Deferred != 1 || outcome.Failed != 0 || len(adapter.finished) != 1 {
		t.Fatalf("pending restore ignored running game: %+v", outcome)
	}
}

// A retry must not replay an obsolete decision after the user changes targets.
func TestAPendingRestoreHoldsWhenItsSelectionChanges(t *testing.T) {
	for _, changed := range []string{"binding", "current revision", "deleted save"} {
		t.Run(changed, func(t *testing.T) {
			baseline := testRevision("before", "omnisave-1", "old-progress")
			current := testRevision("after", "omnisave-1", "new-progress")
			fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
			adapter := &finishingAdapter{failure: errors.New("Steam unavailable")}
			scanner := client.NewScanner(nil, adapter)
			reconcileWith(t, server, scanner, &fixture, savesync.Options{})
			adapter.failure = nil
			var nextServer savesync.Server = server
			if changed == "binding" {
				if err := fixture.State.Bind(fixture.Local(), "another-save"); err != nil {
					t.Fatal(err)
				}
			} else {
				nextServer = movedCurrentServer{Server: server, revisionID: baseline.ID, deleted: changed == "deleted save"}
			}
			outcome, report := reconcileWith(t, nextServer, scanner, &fixture, savesync.Options{})
			if changed == "deleted save" {
				if outcome.Untracked != 1 || len(fixture.State.PendingPlacements) != 0 || len(adapter.finished) != 1 {
					t.Fatalf("deleted save retained its pending restore: %+v", outcome)
				}
				return
			}
			if outcome.Failed != 1 || len(adapter.finished) != 1 || len(report.Of("SyncedWith")) != 0 {
				t.Fatalf("obsolete restore was replayed: %+v", outcome)
			}
		})
	}
}

type movedCurrentServer struct {
	savesync.Server
	revisionID string
	deleted    bool
}

func (s movedCurrentServer) ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error) {
	if s.deleted {
		return nil, nil
	}
	saves, err := s.Server.ListOmnisaves(ctx)
	for i := range saves {
		saves[i].CurrentRevisionID = &s.revisionID
	}
	return saves, err
}

// Reopening an unbound answer applies only while placement has not landed:
// a Steam failure after placement still needs an unattended retry.
func TestAnUnboundRestoreRetriesSteamAfterLocalPlacement(t *testing.T) {
	baseline := testRevision("before", "omnisave-1", "old-progress")
	current := testRevision("after", "omnisave-1", "new-progress")
	fixture, server := pullOnFinishingTarget(t, baseline, current, map[string]string{current.Files[0].Artifact.SHA256: "new-progress"})
	fixture.State.Unbind(fixture.Local())
	adapter := &finishingAdapter{failure: errors.New("Steam unavailable")}
	scanner := client.NewScanner(nil, adapter)
	options := savesync.Options{Prompts: savesync.Prompts{Stale: func(savesync.StaleQuestion) (savesync.StaleChoice, error) {
		return savesync.StaleJump, nil
	}}}
	first, _ := reconcileWith(t, server, scanner, &fixture, options)
	if first.Failed != 1 || fixture.Read(t) != "new-progress" {
		t.Fatalf("expected local placement before Steam failure: %+v", first)
	}
	adapter.failure = nil
	second, _ := reconcileWith(t, server, scanner, &fixture, savesync.Options{})
	if second.Pulled != 1 || second.Failed != 0 || len(adapter.finished) != 2 || len(fixture.State.PendingPlacements) != 0 {
		t.Fatalf("unfinished Steam placement did not retry: %+v", second)
	}
}
