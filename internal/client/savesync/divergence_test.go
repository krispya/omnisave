package savesync_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// diverge seeds the fixture, then moves both sides: another Device commits
// while this one writes local progress.
func diverge(t *testing.T, server *remote.Client, fixture *savesynctest.Fixture) tracking.Binding {
	t.Helper()
	if outcome := syncOnce(t, server, fixture); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	bound, _ := fixture.State.BindingFor(fixture.Local())
	savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	fixture.Write(t, "local-divergence")
	return bound
}

// forkNamed fails unless the save the fixture is now bound to carries name.
func forkNamed(t *testing.T, server *remote.Client, fixture *savesynctest.Fixture, name string) {
	t.Helper()
	forked, _ := fixture.State.BindingFor(fixture.Local())
	for _, save := range savesynctest.Saves(t, server) {
		if save.ID != forked.OmnisaveID {
			continue
		}
		if save.DisplayName != name {
			t.Fatalf("expected the fork named %q, got %q", name, save.DisplayName)
		}
		return
	}
	t.Fatalf("expected the save bound here, %q, on the server", forked.OmnisaveID)
}

func TestDivergedSaveCanForkHereAndContinueLocally(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := diverge(t, server, &fixture)
	fixture.State.Device.Name = "Steam Deck"

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedFork)})

	if outcome.Forked != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected fork-here to only fork, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "local-divergence" {
		t.Fatalf("expected fork-here to keep local content, got %q", content)
	}
	forked, _ := fixture.State.BindingFor(fixture.Local())
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on a new lineage, got %+v", forked)
	}
	// The name links the fork to the lineage it left and the device whose
	// progress it keeps.
	forkNamed(t, server, &fixture, "Save 1 (Steam Deck)")
	// The next pass is quiet: the fork's current revision is exactly the local content.
	if outcome := syncOnce(t, server, &fixture); outcome.Changed() {
		t.Fatalf("expected the forked lineage to be in sync, got %+v", outcome)
	}
}

// Using this Device's save stays on the same omnisave: the local content is
// committed on top of the Current Revision and becomes current, so the
// replaced revision is its parent and other Devices adopt it as a plain pull.
func TestUsingThisDevicesSaveMakesItCurrentOnTheSameOmnisave(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := diverge(t, server, &fixture)
	replaced := *savesynctest.Saves(t, server)[0].CurrentRevisionID
	fixture.State.RecordAchievementsSeen(fixture.Local(), time.Unix(1700000000, 0), []string{"act-1"})

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedUseLocal)})

	if outcome.Pushed != 1 || outcome.Pulled != 0 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected using local to only commit, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "local-divergence" {
		t.Fatalf("expected using local to leave local content alone, got %q", content)
	}
	saves := savesynctest.Saves(t, server)
	if len(saves) != 1 {
		t.Fatalf("expected no new omnisave, got %+v", saves)
	}
	committed, _ := findRevision(savesynctest.Revisions(t, server, bound.OmnisaveID), *saves[0].CurrentRevisionID)
	if committed.ParentID == nil || *committed.ParentID != replaced {
		t.Fatalf("expected the local save on top of the replaced current, got %+v", committed)
	}
	rebound, _ := fixture.State.BindingFor(fixture.Local())
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil || *rebound.LastSyncedRevisionID != committed.ID {
		t.Fatalf("expected the binding synced to the local revision, got %+v", rebound)
	}
	// Staying on the same omnisave keeps what this Device already accounted
	// for, so achievements unlocked before the answer are not lost.
	if seen, ok := fixture.State.AchievementsSeen(fixture.Local()); !ok || len(seen.IDs) != 1 {
		t.Fatalf("expected the achievement watermark kept, got %+v", seen)
	}
	if outcome := syncOnce(t, server, &fixture); outcome.Changed() {
		t.Fatalf("expected the local save to be in sync, got %+v", outcome)
	}
}

// divergeOntoOldContent builds a divergence whose local content the server
// already holds: the save reverts to the seed revision's bytes while another
// device moves current further along the same line. Nothing local is unsynced,
// so neither answer should have to preserve anything.
func divergeOntoOldContent(t *testing.T, server *remote.Client, fixture *savesynctest.Fixture) tracking.Binding {
	t.Helper()
	_, bound := savesynctest.PushSecondRevision(t, server, fixture, "second-progress")
	savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	fixture.Write(t, "first-progress")
	if outcome := syncOnce(t, server, fixture); outcome.Diverged != 1 {
		t.Fatalf("expected a headless pass to report the divergence, got %+v", outcome)
	}
	return bound
}

func TestJumpingFromContentTheHistoryAlreadyHoldsKeepsNoBranch(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := divergeOntoOldContent(t, server, &fixture)
	before := savesynctest.Revisions(t, server, bound.OmnisaveID)

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedJump)})

	if outcome.Pulled != 1 || outcome.Branched != 0 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected jump to pull without preserving, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "deck-progress" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	// The matched revision was the proof the server already had the content.
	if after := savesynctest.Revisions(t, server, bound.OmnisaveID); len(after) != len(before) {
		t.Fatalf("expected no new revision, had %d and got %d", len(before), len(after))
	}
	rebound, _ := fixture.State.BindingFor(fixture.Local())
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to advance on the same lineage, got %+v", rebound)
	}
}

func TestForkingFromContentTheHistoryAlreadyHoldsSharesTheMatchedRevision(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := divergeOntoOldContent(t, server, &fixture)
	before := savesynctest.Revisions(t, server, bound.OmnisaveID)
	fixture.State.Device.Name = "Steam Deck"

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedFork)})

	if outcome.Forked != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected fork-here to fork without pushing, got %+v", outcome)
	}
	forked, _ := fixture.State.BindingFor(fixture.Local())
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on a new lineage, got %+v", forked)
	}
	// The fork begins at the matched revision — shared ancestry, no copy.
	if _, shared := findRevision(before, *forked.LastSyncedRevisionID); !shared {
		t.Fatal("expected the fork to share the matched revision instead of pushing a new one")
	}
	forkNamed(t, server, &fixture, "Save 1 (Steam Deck)")
	// The next pass is quiet: the fork's current revision is the local content.
	if outcome := syncOnce(t, server, &fixture); outcome.Changed() {
		t.Fatalf("expected the forked lineage to be in sync, got %+v", outcome)
	}
}

// A manual bind to non-matching content leaves a binding with no baseline,
// which is diverged from the start — but a local save that turns out to equal
// the Current Revision has nothing to resolve, so even a headless pass
// rebinds it silently instead of waiting for a question.
func TestABaselinelessBindingMatchingCurrentRebindsSilently(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	syncOnce(t, server, &fixture)
	bound, _ := fixture.State.BindingFor(fixture.Local())
	// Rebinding drops the baseline, as a manual `omnisave bind` would.
	if err := fixture.State.Bind(fixture.Local(), bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}

	outcome := syncOnce(t, server, &fixture)

	if outcome.Rebound != 1 || outcome.Diverged != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the matching save to rebind silently, got %+v", outcome)
	}
	if rebound, _ := fixture.State.BindingFor(fixture.Local()); rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the rebind to restore a baseline, got %+v", rebound)
	}
}

// seedForeignLayoutLineage resolves the fixture's game and seeds it an
// Omnisave whose revisions live in another save's layout — the shape a Steam
// Cloud lineage has next to a native-folder Local Save. No Local Save of the
// fixture can ever adopt its Current Revision.
func seedForeignLayoutLineage(t *testing.T, server *remote.Client, fixture *savesynctest.Fixture) *omnisave.Omnisave {
	t.Helper()
	ctx := context.Background()
	outcome, _ := savesync.SyncTracking(ctx, server, &fixture.State, fixture.Scans, nil, &savesynctest.Recorder{})
	serverGameID := fixture.State.Games["local-game-1"].ServerGameID
	if !outcome.Synced || serverGameID == "" {
		t.Fatalf("expected tracking to resolve the game, got %+v", outcome)
	}
	cloudPath := filepath.Join(t.TempDir(), "profile.save")
	if err := os.WriteFile(cloudPath, []byte("cloud-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloudSave := target.Save{
		ID: "cloud-save", TargetID: "cloud-target", GameID: "cloud-game", Kind: "cloud",
		Files: []target.File{{Path: cloudPath, LocationID: "remote", RelativePath: "profile.save"}},
	}
	seeded, _, err := binding.Seed(ctx, server, serverGameID, cloudSave, "")
	if err != nil {
		t.Fatal(err)
	}
	return seeded
}

// A binding whose lineage is spelled in another save's layout can never adopt
// that lineage's current, nor commit onto it without mixing two layouts in
// one tree. Both sync answers refuse before changing anything, so repeating
// them stacks nothing.
func TestSyncingWithAForeignLayoutLineageFailsBeforeChangingAnything(t *testing.T) {
	for _, choice := range []savesync.DivergedChoice{savesync.DivergedJump, savesync.DivergedUseLocal} {
		t.Run(string(choice), func(t *testing.T) {
			server := savesynctest.NewServer(t)
			fixture := savesynctest.NewSyncFixture(t, "local-progress")
			seeded := seedForeignLayoutLineage(t, server, &fixture)
			if err := fixture.State.Bind(fixture.Local(), seeded.ID); err != nil {
				t.Fatal(err)
			}

			outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, choice)})

			if outcome.Failed != 1 || outcome.Pushed != 0 || outcome.Branched != 0 || outcome.Pulled != 0 {
				t.Fatalf("expected the answer refused with nothing changed, got %+v", outcome)
			}
			if content := fixture.Read(t); content != "local-progress" {
				t.Fatalf("expected the local save untouched, got %q", content)
			}
			if revisions := savesynctest.Revisions(t, server, seeded.ID); len(revisions) != 1 {
				t.Fatalf("expected no revision committed for a refused answer, got %+v", revisions)
			}
		})
	}
}

// A game whose only lineages live in another layout offers nothing this save
// could adopt, so an unmatched Local Save creates its own Omnisave without a
// question: one safe outcome remains (FDR-003, decision 1).
func TestAnUnmatchedSaveWithOnlyForeignLayoutLineagesSeedsWithoutAsking(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "local-progress")
	seeded := seedForeignLayoutLineage(t, server, &fixture)

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: strictPrompts(t)})

	if outcome.Seeded != 1 || outcome.Failed != 0 || outcome.Unbound != 0 {
		t.Fatalf("expected the unmatched save seeded without asking, got %+v", outcome)
	}
	bound, ok := fixture.State.BindingFor(fixture.Local())
	if !ok || bound.OmnisaveID == seeded.ID || bound.LastSyncedRevisionID == nil {
		t.Fatalf("expected a fresh lineage bound with a baseline, got %+v", bound)
	}
}

// An unattended pass takes the same one safe outcome: nothing matches and
// nothing can be adopted, so the save gets its own omnisave instead of
// waiting on a question nobody is there to answer.
func TestAnUnattendedPassSeedsAnUnmatchedSaveWithNothingToAdopt(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "local-progress")
	seeded := seedForeignLayoutLineage(t, server, &fixture)

	outcome := syncOnce(t, server, &fixture)

	if outcome.Seeded != 1 || outcome.Unbound != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the unattended pass to seed, got %+v", outcome)
	}
	if bound, ok := fixture.State.BindingFor(fixture.Local()); !ok || bound.OmnisaveID == seeded.ID {
		t.Fatalf("expected a fresh omnisave bound, got %+v", bound)
	}
}

// A binding with no baseline shares no revision with its lineage, so a jump
// keeps the local progress as a branch of current, inside the same omnisave.
// When the pull then fails, the branch is already in the history the retry
// matches against, so the retry preserves nothing twice.
func TestABaselinelessJumpKeepsProgressAsABranchOfCurrent(t *testing.T) {
	var failDownloads atomic.Bool
	server := savesynctest.NewInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if failDownloads.Load() && request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/api/v1/artifacts/") {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := savesynctest.NewSyncFixture(t, "server-content")
	if outcome := syncOnce(t, server, &fixture); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	bound, _ := fixture.State.BindingFor(fixture.Local())
	current := *bound.LastSyncedRevisionID
	fixture.Write(t, "local-progress")
	// Rebinding drops the baseline: unmatched content, diverged from the start.
	if err := fixture.State.Bind(fixture.Local(), bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}
	fixture.State.Device.Name = "Steam Deck"
	jump := savesync.Options{Prompts: answering(t, savesync.DivergedJump)}

	failDownloads.Store(true)
	outcome := savesynctest.SyncOnce(t, server, &fixture, jump)
	if outcome.Branched != 1 || outcome.Forked != 0 || outcome.Failed != 1 {
		t.Fatalf("expected the branch to land and the pull to fail, got %+v", outcome)
	}
	if saves := savesynctest.Saves(t, server); len(saves) != 1 || *saves[0].CurrentRevisionID != current {
		t.Fatalf("expected the progress kept inside the lineage behind current, got %+v", saves)
	}
	history := savesynctest.Revisions(t, server, bound.OmnisaveID)
	for _, revision := range history {
		if revision.ID != current && (revision.ParentID == nil || *revision.ParentID != current) {
			t.Fatalf("expected the branch to grow from current, got %+v", revision)
		}
	}
	if len(history) != 2 {
		t.Fatalf("expected the seed and one branch, got %+v", history)
	}

	failDownloads.Store(false)
	outcome = savesynctest.SyncOnce(t, server, &fixture, jump)
	if outcome.Pulled != 1 || outcome.Branched != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the retry to pull without another branch, got %+v", outcome)
	}
	if after := savesynctest.Revisions(t, server, bound.OmnisaveID); len(after) != len(history) {
		t.Fatalf("expected the retry to commit nothing new, had %d and got %d", len(history), len(after))
	}
	if content := fixture.Read(t); content != "server-content" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	rebound, _ := fixture.State.BindingFor(fixture.Local())
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding settled on the rejoined lineage, got %+v", rebound)
	}
}

// A preservation fork that failed before its push holds none of the progress
// it was made to keep, and reads as that progress saved when it is not. The
// failed answer removes it, so a retry starts clean instead of stacking
// empty forks.
func TestAFailedForkPreservationLeavesNoEmptyFork(t *testing.T) {
	var failCommits atomic.Bool
	server := savesynctest.NewInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if failCommits.Load() && request.Method == http.MethodPost &&
			strings.HasSuffix(request.URL.Path, "/revisions") {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := diverge(t, server, &fixture)
	fixture.State.Device.Name = "Steam Deck"
	fork := savesync.Options{Prompts: answering(t, savesync.DivergedFork)}

	failCommits.Store(true)
	outcome := savesynctest.SyncOnce(t, server, &fixture, fork)
	if outcome.Failed != 1 || outcome.Forked != 0 {
		t.Fatalf("expected the fork answer to fail cleanly, got %+v", outcome)
	}
	if saves := savesynctest.Saves(t, server); len(saves) != 1 {
		t.Fatalf("expected the empty fork removed, got %+v", saves)
	}

	failCommits.Store(false)
	outcome = savesynctest.SyncOnce(t, server, &fixture, fork)
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the retried fork to succeed, got %+v", outcome)
	}
	if saves := savesynctest.Saves(t, server); len(saves) != 2 {
		t.Fatalf("expected exactly one preservation fork, got %+v", saves)
	}
	forked, _ := fixture.State.BindingFor(fixture.Local())
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on the fork, got %+v", forked)
	}
}

// An outage can take down the fork's push and the cleanup that would remove
// the empty fork. The answer records the fork instead, and the retry pushes
// onto that same fork rather than stacking another beside it.
func TestAForkOutageThatAlsoBlocksCleanupResumesTheSameFork(t *testing.T) {
	var broken atomic.Bool
	server := savesynctest.NewInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		blocked := (request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/revisions")) ||
			request.Method == http.MethodDelete
		if broken.Load() && blocked {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	bound := diverge(t, server, &fixture)
	fixture.State.Device.Name = "Steam Deck"
	fork := savesync.Options{Prompts: answering(t, savesync.DivergedFork)}

	broken.Store(true)
	outcome := savesynctest.SyncOnce(t, server, &fixture, fork)
	if outcome.Failed != 1 || outcome.Forked != 0 {
		t.Fatalf("expected the fork answer to fail, got %+v", outcome)
	}
	saves := savesynctest.Saves(t, server)
	if len(saves) != 2 {
		t.Fatalf("expected the unremovable empty fork to stand, got %+v", saves)
	}
	var emptyFork omnisave.Omnisave
	for _, save := range saves {
		if save.ID != bound.OmnisaveID {
			emptyFork = save
		}
	}
	if recorded, ok := fixture.State.PendingPreservationFor(fixture.Local()); !ok || recorded != emptyFork.ID {
		t.Fatalf("expected the empty fork recorded for resumption, got %q %v", recorded, ok)
	}

	broken.Store(false)
	outcome = savesynctest.SyncOnce(t, server, &fixture, fork)
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the retried fork to succeed, got %+v", outcome)
	}
	if saves := savesynctest.Saves(t, server); len(saves) != 2 {
		t.Fatalf("expected the retry to resume the recorded fork, got %+v", saves)
	}
	forked, _ := fixture.State.BindingFor(fixture.Local())
	if forked.OmnisaveID != emptyFork.ID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on the resumed fork, got %+v", forked)
	}
	if _, ok := fixture.State.PendingPreservationFor(fixture.Local()); ok {
		t.Fatal("expected the settled answer to clear the recorded preservation")
	}
}

// Another lineage holding the same bytes is not this Device's preservation.
// A fork answer must never adopt it — only a preservation the answer itself
// recorded — because the twin is an independent playthrough whose future
// updates would otherwise cross into this Device's line.
func TestAForkAnswerNeverAdoptsATwinLineage(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "server-content")
	if outcome := syncOnce(t, server, &fixture); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	bound, _ := fixture.State.BindingFor(fixture.Local())
	fixture.Write(t, "local-progress")
	// Rebinding drops the baseline: unmatched content, diverged from the start.
	if err := fixture.State.Bind(fixture.Local(), bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}
	// An independent lineage that happens to hold the same bytes right now.
	twinPath := filepath.Join(t.TempDir(), "Chrono Trigger.srm")
	if err := os.WriteFile(twinPath, []byte("local-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	twinSave := target.Save{
		ID: "twin-save", TargetID: "twin-target", GameID: "twin-game", Kind: "battery",
		Files: []target.File{{Path: twinPath, LocationID: "battery", RelativePath: "Chrono Trigger.srm"}},
	}
	twin, _, err := binding.Seed(context.Background(), server, fixture.State.Games["local-game-1"].ServerGameID, twinSave, "Twin")
	if err != nil {
		t.Fatal(err)
	}
	fixture.State.Device.Name = "Steam Deck"

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedFork)})

	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the fork to create its own preservation, got %+v", outcome)
	}
	if forked, _ := fixture.State.BindingFor(fixture.Local()); forked.OmnisaveID == twin.ID {
		t.Fatal("expected the fork to leave the twin lineage alone")
	}
	if saves := savesynctest.Saves(t, server); len(saves) != 3 {
		t.Fatalf("expected the twin untouched beside a fresh preservation, got %+v", saves)
	}
}
