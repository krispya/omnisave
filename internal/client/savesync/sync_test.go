package savesync_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// rewriteInPlace replaces a file's bytes and restores the modification time
// it had, so nothing a stat can see about the file has changed. It is how a
// test asks whether a pass read the save or only looked at it.
func rewriteInPlace(t *testing.T, path, content string) {
	t.Helper()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(content)) != before.Size() {
		t.Fatalf("rewriting in place needs the same size, had %d and want %d", before.Size(), len(content))
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
}

// A pass used to read every bound save in full every time it ran, which the
// periodic pull and any other Device's commit both trigger. A save whose
// files have not moved under an Omnisave that has not moved cannot need
// anything, and the pass now settles it without opening it.
func TestASettledSaveIsNotReadAgain(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
	// The first pass seeds the save; the second proves it equal the reading
	// way and remembers how its files stood.
	syncOnce(t, server, &fixture)
	syncOnce(t, server, &fixture)
	bound, isBound := fixture.State.BindingFor(fixture.Local())
	if !isBound {
		t.Fatal("expected the seeded save to be bound")
	}

	// Content the pass cannot see without reading the file.
	rewriteInPlace(t, fixture.LocalPath, "secretly-rewritten")

	if outcome := syncOnce(t, server, &fixture); outcome.Pushed != 0 {
		t.Fatalf("expected a save nothing touched to be settled unread, got %d pushed", outcome.Pushed)
	}
	if history := savesynctest.Revisions(t, server, bound.OmnisaveID); len(history) != 1 {
		t.Fatalf("expected the history to stand at the seed, got %d revisions", len(history))
	}
}

// The save's history is the other thing a pass spent on every bound save
// every time: one request per Omnisave, to re-read a history that only
// matters once something has moved.
func TestASettledSaveCostsNoHistoryRequest(t *testing.T) {
	var histories atomic.Int64
	server := savesynctest.NewObservedServer(t, func(request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/revisions") && request.Method == http.MethodGet {
			histories.Add(1)
		}
	})
	fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture)
	syncOnce(t, server, &fixture)

	histories.Store(0)
	syncOnce(t, server, &fixture)

	if asked := histories.Load(); asked != 0 {
		t.Fatalf("expected a settled save to ask the server for nothing, got %d history requests", asked)
	}
}

// The live view spins the row a pass has in hand. A pass with nothing to do
// must therefore take nothing in hand: a row that spins for a game the pass
// only considered claims a sync is happening when none is, and at the speed
// those decisions are made it claims it as a flicker down the table.
func TestASettledPassTakesNoGameInHand(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture)
	syncOnce(t, server, &fixture)

	ctx := context.Background()
	report := &savesynctest.Recorder{}
	outcome, confirmed := savesync.SyncTracking(ctx, server, &fixture.State, fixture.Scans, nil, report)
	ports := savesync.Ports{Server: server, Report: report}
	if err := savesync.Reconcile(ctx, ports, &fixture.State, fixture.Scans, confirmed, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if held := slices.DeleteFunc(report.Worked, func(title string) bool { return title == "" }); len(held) != 0 {
		t.Fatalf("expected a settled pass to work on nothing, got %v", held)
	}
}

// Telling the server what it already knows is a request per game per pass,
// and a tracked game that is still installed is not news.
func TestASettledPassRestatesNoTracking(t *testing.T) {
	var claims atomic.Int64
	server := savesynctest.NewObservedServer(t, func(request *http.Request) {
		if strings.Contains(request.URL.Path, "/tracking/") && request.Method == http.MethodPut {
			claims.Add(1)
		}
	})
	fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture)
	if claims.Load() == 0 {
		t.Fatal("expected the first pass to state what this device tracks")
	}

	claims.Store(0)
	syncOnce(t, server, &fixture)
	if stated := claims.Load(); stated != 0 {
		t.Fatalf("expected an unchanged claim to go unrepeated, got %d", stated)
	}

	// A game that stops being installed is a different claim, and the server
	// hears it.
	fixture.Scans[0].Games = nil
	syncOnce(t, server, &fixture)
	if stated := claims.Load(); stated != 1 {
		t.Fatalf("expected a changed claim to reach the server, got %d", stated)
	}
}

// The summary is only ever an excuse to skip work, never a reason to miss
// it: both halves of what it stands for are checked against.
func TestASettledSaveIsReadAgainWhenEitherSideMoves(t *testing.T) {
	t.Run("local files move", func(t *testing.T) {
		server := savesynctest.NewServer(t)
		fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
		syncOnce(t, server, &fixture)
		syncOnce(t, server, &fixture)

		fixture.Write(t, "played-further")
		if outcome := syncOnce(t, server, &fixture); outcome.Pushed != 1 {
			t.Fatalf("expected local progress to commit, got %d pushed", outcome.Pushed)
		}
	})

	t.Run("current revision moves", func(t *testing.T) {
		server := savesynctest.NewServer(t)
		fixture := savesynctest.NewSyncFixture(t, "saved-game-content")
		syncOnce(t, server, &fixture)
		syncOnce(t, server, &fixture)
		bound, _ := fixture.State.BindingFor(fixture.Local())

		// Another Device commits: the local files are untouched and their
		// summary still stands, but what it stood for has moved.
		savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")

		if outcome := syncOnce(t, server, &fixture); outcome.Pulled != 1 {
			t.Fatalf("expected the moved current revision to pull, got %+v", outcome)
		}
		if content := fixture.Read(t); content != "deck-progress" {
			t.Fatalf("expected the pull to land, got %q", content)
		}
	})
}

func TestWatchedPathsIncludeParentDirectoriesSoNewFilesTrigger(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	prospective := filepath.Join(t.TempDir(), "future-save")
	fixture.Scans[0].Games[0].Destinations = []target.SaveDestination{{
		ID: "future", Locations: []target.SaveLocation{{
			ID: "battery", Path: prospective, Kind: target.SaveLocationDirectory,
		}},
	}}

	paths := savesync.WatchedFiles(&fixture.State, fixture.Scans)

	for _, want := range []string{fixture.LocalPath, filepath.Dir(fixture.LocalPath), prospective} {
		if !slices.Contains(paths, want) {
			t.Fatalf("expected current and prospective save paths to be polled, got %q", paths)
		}
	}
}

func TestSyncLifecyclePushesPullsAndReportsDivergence(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")

	// First pass seeds; the seed revision becomes the baseline.
	if outcome := syncOnce(t, server, &fixture); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}

	// A pass with nothing new stays quiet.
	if outcome := syncOnce(t, server, &fixture); outcome.Changed() {
		t.Fatalf("expected an in-sync pass to report nothing, got %+v", outcome)
	}

	// Local progress pushes and advances the baseline.
	fixture.Write(t, "second-progress")
	if outcome := syncOnce(t, server, &fixture); outcome.Pushed != 1 || outcome.Failed != 0 {
		t.Fatalf("expected local progress to push, got %+v", outcome)
	}

	// The spacing floor suppresses an immediate follow-up commit.
	fixture.Write(t, "third-progress")
	if outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{PushFloor: time.Hour}); outcome.Changed() {
		t.Fatalf("expected the spacing floor to hold the commit, got %+v", outcome)
	}
	if outcome := syncOnce(t, server, &fixture); outcome.Pushed != 1 {
		t.Fatalf("expected the held commit to push without a floor, got %+v", outcome)
	}

	// Another device's progress pulls down automatically.
	bound, ok := fixture.State.BindingFor(fixture.Local())
	if !ok {
		t.Fatal("expected a binding after seeding")
	}
	savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if outcome := syncOnce(t, server, &fixture); outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the current revision to sync down, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "deck-progress" {
		t.Fatalf("expected the pull to place the current revision's content, got %q", content)
	}

	// Progress on both sides diverges: headless passes report and skip.
	fixture.Write(t, "local-divergence")
	savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-divergence")
	if outcome := syncOnce(t, server, &fixture); outcome.Diverged != 1 || outcome.Pushed != 0 || outcome.Pulled != 0 {
		t.Fatalf("expected headless divergence to only report, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "local-divergence" {
		t.Fatalf("expected headless divergence to leave local content, got %q", content)
	}
}

// Jump-to-latest keeps the unsynced local progress as a branch inside the
// same Omnisave — named for this device, left behind the current pointer —
// then adopts the current revision. No second save appears.
func TestJumpingFromADivergenceKeepsLocalProgressAsABranch(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	syncOnce(t, server, &fixture)
	baseline, _ := fixture.State.BindingFor(fixture.Local())
	fixture.Write(t, "local-divergence")
	savesynctest.OtherDeviceCommit(t, server, baseline.OmnisaveID, "deck-divergence")
	fixture.State.Device.Name = "Steam Deck"

	prompts := strictPrompts(t)
	prompts.Diverged = func(question savesync.DivergedQuestion) (savesync.DivergedChoice, error) {
		// The question names the save forking would have created.
		if question.ForkName != "Save 1 (Steam Deck)" {
			t.Fatalf("expected the question to carry the fork name, got %+v", question)
		}
		return savesync.DivergedJump, nil
	}
	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: prompts})

	if outcome.Branched != 1 || outcome.Pulled != 1 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected jump to keep a branch then pull, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "deck-divergence" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	saves := savesynctest.Saves(t, server)
	if len(saves) != 1 {
		t.Fatalf("expected the jump to leave a single save, got %d", len(saves))
	}
	var branch omnisave.Revision
	for _, revision := range savesynctest.Revisions(t, server, baseline.OmnisaveID) {
		if len(revision.Files) == 1 && revision.Files[0].Artifact.SHA256 == artifactOf("local-divergence").SHA256 {
			branch = revision
		}
	}
	if branch.ID == "" {
		t.Fatal("expected the branch to carry the diverged local content")
	}
	if branch.DisplayName != "Steam Deck" {
		t.Fatalf("expected the branch revision named for the device, got %q", branch.DisplayName)
	}
	if branch.ParentID == nil || *branch.ParentID != *baseline.LastSyncedRevisionID {
		t.Fatalf("expected the branch to continue the baseline %v, got parent %v",
			baseline.LastSyncedRevisionID, branch.ParentID)
	}
	if saves[0].CurrentRevisionID == nil || *saves[0].CurrentRevisionID == branch.ID {
		t.Fatalf("expected the branch to stay behind the current pointer, got current %v", saves[0].CurrentRevisionID)
	}
	rebound, _ := fixture.State.BindingFor(fixture.Local())
	if rebound.OmnisaveID != baseline.OmnisaveID {
		t.Fatalf("expected the binding to stay on the original lineage, got %+v", rebound)
	}
	if rebound.LastSyncedRevisionID == nil || *rebound.LastSyncedRevisionID != *saves[0].CurrentRevisionID {
		t.Fatalf("expected the baseline to advance to current, got %+v", rebound)
	}
}

func TestSyncPullsARestoredCurrentRevisionBackDown(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	seed, bound := savesynctest.PushSecondRevision(t, server, &fixture, "second-progress")

	// The Dash rewinds current to the seed; this device is now ahead of it.
	savesynctest.Rewind(t, server, bound, seed)

	outcome := syncOnce(t, server, &fixture)
	if outcome.Pulled != 1 || outcome.Failed != 0 || outcome.Diverged != 0 {
		t.Fatalf("expected a clean device to adopt the restored revision, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "first-progress" {
		t.Fatalf("expected the local save to revert to the restored revision, got %q", content)
	}
	assertBinding(t, &fixture, bound.OmnisaveID, seed.ID)
}

func TestAPullWaitsWhileTheGameIsPlayedAndAppliesOnceItCloses(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	seed, bound := savesynctest.PushSecondRevision(t, server, &fixture, "second-progress")
	savesynctest.Rewind(t, server, bound, seed)

	// The pass runs while the game is being played: the pull waits, the
	// local save is untouched, and the gate records whose exit resolves it.
	gate := savesync.NewPullGate(map[string]bool{"local-game-1": true})
	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Gate: gate})
	if outcome.Deferred != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull deferred under the playing game, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "second-progress" {
		t.Fatalf("expected the deferred pull to leave the local save alone, got %q", content)
	}
	if waiting := gate.Waiting(); len(waiting) != 1 || waiting[0] != "local-game-1" {
		t.Fatalf("expected the gate to record the waiting game, got %v", waiting)
	}

	// The game closed: the same comparison now pulls and moves the baseline.
	outcome = savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Gate: savesync.NewPullGate(nil)})
	if outcome.Pulled != 1 || outcome.Deferred != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull once the game closed, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "first-progress" {
		t.Fatalf("expected the restored revision placed after the game closed, got %q", content)
	}
	assertBinding(t, &fixture, bound.OmnisaveID, seed.ID)
}

// A rewind under unsynced local progress is not a conflict: the server moved
// its pointer back without adding anything this device lacks, so the local
// content commits as a branch off the baseline it continues, and current
// follows it (FDR-005, decision 10). No prompt, no preservation fork.
func TestARestoreUnderLocalProgressBranchesWithoutAsking(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	seed, bound := savesynctest.PushSecondRevision(t, server, &fixture, "second-progress")
	savesynctest.Rewind(t, server, bound, seed)
	// The baseline is now a descendant of current, and the game kept writing.
	fixture.Write(t, "local-edit")

	// A headless pass resolves it on its own: no prompts are wired in, so a
	// pass that tried to ask would leave it diverged rather than branch.
	outcome := syncOnce(t, server, &fixture)
	if outcome.Branched != 1 || outcome.Diverged != 0 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected a rewind under local progress to branch, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "local-edit" {
		t.Fatalf("expected the branch commit to leave the local save alone, got %q", content)
	}

	// The new revision continues the baseline it actually came from, not the
	// restored node, and it is what the Omnisave now points at.
	history := savesynctest.Revisions(t, server, bound.OmnisaveID)
	if len(history) != 3 {
		t.Fatalf("expected the seed, the baseline, and the branch commit, got %d revisions", len(history))
	}
	branched, _ := fixture.State.BindingFor(fixture.Local())
	if branched.LastSyncedRevisionID == nil {
		t.Fatalf("expected the baseline to advance to the branch commit, got %+v", branched)
	}
	committed, found := findRevision(history, *branched.LastSyncedRevisionID)
	if !found {
		t.Fatal("expected the branch commit in the Omnisave's history")
	}
	if committed.ParentID == nil || *committed.ParentID != *bound.LastSyncedRevisionID {
		t.Fatalf("expected the branch commit to attach to the old baseline, got parent %v", committed.ParentID)
	}
	saves := savesynctest.Saves(t, server)
	if len(saves) != 1 {
		t.Fatalf("expected branching to create no new Omnisave, got %d saves", len(saves))
	}
	if saves[0].CurrentRevisionID == nil || *saves[0].CurrentRevisionID != committed.ID {
		t.Fatalf("expected the branch commit to become current, got %v", saves[0].CurrentRevisionID)
	}
}

// A rewind that another device has already built on leaves current on a
// sibling branch. That is still not a conflict — nothing this device holds is
// at risk — so its progress branches off its own baseline, and the restored
// node ends up with two children.
func TestProgressBesideASiblingBranchBranchesRatherThanDiverging(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	seed, bound := savesynctest.PushSecondRevision(t, server, &fixture, "second-progress")
	savesynctest.Rewind(t, server, bound, seed)
	// Another device adopts the rewind and plays on from it, so current now
	// sits on a branch this device's baseline never touched.
	sibling := savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-after-rewind")
	fixture.Write(t, "local-edit")

	outcome := syncOnce(t, server, &fixture)
	if outcome.Branched != 1 || outcome.Diverged != 0 || outcome.Failed != 0 {
		t.Fatalf("expected progress beside a sibling branch to branch, got %+v", outcome)
	}
	history := savesynctest.Revisions(t, server, bound.OmnisaveID)
	children := 0
	for _, revision := range history {
		if revision.ParentID != nil && *revision.ParentID == seed.ID {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("expected the restored node to carry two branches, got %d children", children)
	}
	// The sibling's work is untouched; it simply is no longer current.
	if _, found := findRevision(history, sibling.ID); !found {
		t.Fatal("expected the other device's revision to survive branching")
	}
}

// The Undertale story: a game Steam does not sync, whose profile rules spell
// its one save location differently per OS. Two devices with different
// spellings share one lineage — the second device joins by translated
// content match, its commits keep the lineage's original spelling, and the
// first device pulls them back (FDR-003, decision 11).
func TestAProfileSaveSyncsAcrossOperatingSystems(t *testing.T) {
	server := savesynctest.NewServer(t)
	mac := savesynctest.NewSyncFixture(t, "mac-progress")
	if outcome := syncOnce(t, server, &mac); outcome.Seeded != 1 {
		t.Fatalf("expected the first device to seed, got %+v", outcome)
	}
	macBound, _ := mac.State.BindingFor(mac.Local())

	// The second device resolves the same rule under another OS: a different
	// location identity, the same relative file, the same content.
	deck := savesynctest.NewSyncFixture(t, "mac-progress")
	deck.Save.Files[0].LocationID = "deck"
	deck.Save.LocationAliases = []string{"battery", "deck"}
	deck.Scans[0].Games[0].Saves[0] = deck.Save
	if outcome := syncOnce(t, server, &deck); outcome.Rebound != 1 || outcome.Seeded != 0 {
		t.Fatalf("expected the second device to join the lineage by content, got %+v", outcome)
	}
	deckBound, _ := deck.State.BindingFor(deck.Local())
	if deckBound.OmnisaveID != macBound.OmnisaveID {
		t.Fatalf("expected one shared lineage, got %q and %q", macBound.OmnisaveID, deckBound.OmnisaveID)
	}

	// The second device plays; its commit keeps the lineage's spelling.
	deck.Write(t, "deck-progress")
	if outcome := syncOnce(t, server, &deck); outcome.Pushed != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the second device to commit, got %+v", outcome)
	}
	deckBound, _ = deck.State.BindingFor(deck.Local())
	pushed, found := findRevision(savesynctest.Revisions(t, server, deckBound.OmnisaveID), *deckBound.LastSyncedRevisionID)
	if !found {
		t.Fatal("expected the second device's commit in the shared history")
	}
	for _, file := range pushed.Files {
		if !strings.HasPrefix(file.Path, "battery/") {
			t.Fatalf("expected the commit spelled in the lineage's vocabulary, got %q", file.Path)
		}
	}

	// The first device follows the shared lineage.
	if outcome := syncOnce(t, server, &mac); outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the first device to pull, got %+v", outcome)
	}
	if content := mac.Read(t); content != "deck-progress" {
		t.Fatalf("expected the first device to hold the second's progress, got %q", content)
	}
}
