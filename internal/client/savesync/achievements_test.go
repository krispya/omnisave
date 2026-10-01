package savesync_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// achievingAdapter is a target whose game has achievements the test moves,
// standing in for a store's own records.
type achievingAdapter struct {
	unlocked []target.Achievement
}

func (a *achievingAdapter) Name() string { return "achieving" }

func (a *achievingAdapter) DiscoverTargets(context.Context) ([]target.Target, error) { return nil, nil }

func (a *achievingAdapter) DiscoverGames(context.Context, target.Target) ([]target.InstalledGame, error) {
	return nil, nil
}

func (a *achievingAdapter) DiscoverSaves(context.Context, target.Target, target.InstalledGame) ([]target.Save, error) {
	return nil, nil
}

func (a *achievingAdapter) DiscoverSaveDestinations(context.Context, target.Target, target.InstalledGame) ([]target.SaveDestination, error) {
	return nil, nil
}

func (a *achievingAdapter) UnlockedAchievements(context.Context, target.Target, target.InstalledGame, target.Save) ([]target.Achievement, error) {
	return a.unlocked, nil
}

// unlock records an unlock, oldest first, as a store's records would show it.
func (a *achievingAdapter) unlock(id, name string, at time.Time) {
	a.unlocked = append(a.unlocked, target.Achievement{ID: id, Name: name, UnlockedAt: at})
}

// newAchievingFixture is the sync fixture on a target that can see
// achievements, with a game whose history already has some.
func newAchievingFixture(t *testing.T, content string) (savesynctest.Fixture, *achievingAdapter, *client.Scanner) {
	t.Helper()
	fixture := savesynctest.NewSyncFixture(t, content)
	fixture.Scans[0].Target.Adapter = "achieving"
	game := fixture.State.Games["local-game-1"]
	game.Adapter = "achieving"
	fixture.State.Games["local-game-1"] = game
	adapter := &achievingAdapter{}
	return fixture, adapter, client.NewScanner(nil, adapter)
}

// syncWithAchievements is one pass whose adapters can read achievements.
func syncWithAchievements(t *testing.T, scanner *client.Scanner, server *remote.Client, fixture *savesynctest.Fixture) *savesynctest.Recorder {
	t.Helper()
	ctx := context.Background()
	report := &savesynctest.Recorder{}
	outcome, confirmed := savesync.SyncTracking(ctx, server, &fixture.State, fixture.Scans, nil, report)
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	ports := savesync.Ports{Server: server, Adapters: scanner, Report: report}
	if err := savesync.Reconcile(ctx, ports, &fixture.State, fixture.Scans, confirmed, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	return report
}

func achievementsOf(t *testing.T, server *remote.Client, fixture *savesynctest.Fixture) []omnisave.Achievement {
	t.Helper()
	bound, isBound := fixture.State.BindingFor(fixture.Local())
	if !isBound {
		t.Fatal("expected the save to be bound")
	}
	marks, err := server.ListAchievements(context.Background(), bound.OmnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	return marks
}

// Omnisave can only honestly mark a revision for an unlock it was there for.
// A library joined mid-playthrough already has achievements behind it, and
// pinning all of them to the oldest revision it happens to hold would say
// something false about that snapshot. So the first pass only takes note of
// where the history stands, and everything after it is reported.
func TestTheFirstPassLearnsAGamesHistoryWithoutMarkingIt(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture, game, scanner := newAchievingFixture(t, "saved-game-content")
	game.unlock("VG_DAY1", "Good first day.", time.Now().Add(-30*24*time.Hour))

	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 0 {
		t.Fatalf("expected nothing marked from before this device watched, got %+v", marks)
	}

	// Earned while watching, and the game saved afterwards. The pass commits
	// that save and reports the unlock, which lands on the new revision.
	game.unlock("VG_DAY2", "Back to work.", time.Now())
	fixture.Write(t, "saved-game-progress")
	report := syncWithAchievements(t, scanner, server, &fixture)

	marks := achievementsOf(t, server, &fixture)
	if len(marks) != 1 || marks[0].ID != "VG_DAY2" {
		t.Fatalf("expected only the unlock this device watched, got %+v", marks)
	}
	if marks[0].RevisionID == nil {
		t.Fatalf("expected the unlock placed on a revision, got %+v", marks[0])
	}
	if unlocked := report.Of("Unlocked"); len(unlocked) != 1 || len(unlocked[0].Names) != 1 || unlocked[0].Names[0] != "Back to work." {
		t.Fatalf("expected the recorded unlock reported by name, got %+v", unlocked)
	}

	// Nothing new: a later pass has nothing to say and adds nothing.
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 1 {
		t.Fatalf("expected a quiet pass to add nothing, got %+v", marks)
	}
}

// A report that fails must not be forgotten: the watermark stays where it was
// so the next pass tries the same unlock again.
func TestAnUnreportedUnlockIsTriedAgainOnTheNextPass(t *testing.T) {
	var refuse atomic.Bool
	refuse.Store(true)
	server := savesynctest.NewInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if refuse.Load() && request.Method == http.MethodPost &&
			strings.HasSuffix(request.URL.Path, "/achievements") {
			response.WriteHeader(http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture, game, scanner := newAchievingFixture(t, "saved-game-content")
	game.unlock("VG_DAY1", "Good first day.", time.Now().Add(-30*24*time.Hour))
	syncWithAchievements(t, scanner, server, &fixture)

	game.unlock("VG_DAY2", "Back to work.", time.Now())
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 0 {
		t.Fatalf("expected the refused report to record nothing, got %+v", marks)
	}

	refuse.Store(false)
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 1 || marks[0].ID != "VG_DAY2" {
		t.Fatalf("expected the unlock reported again once the server answered, got %+v", marks)
	}
}

// Whole-second store timestamps are not unique cursors. If a report-size
// boundary cuts through a group earned in the same second, later passes still
// have to carry the rest of that group.
func TestSameSecondUnlocksContinueAcrossReports(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture, game, scanner := newAchievingFixture(t, "saved-game-content")
	game.unlock("BEFORE_WATCH", "Before watch", time.Now().Add(-time.Hour))
	syncWithAchievements(t, scanner, server, &fixture)

	unlockedAt := time.Now()
	for index := range savesync.MaxUnlocksPerReport + 1 {
		id := fmt.Sprintf("ACHIEVEMENT_%03d", index)
		game.unlock(id, id, unlockedAt)
	}

	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != savesync.MaxUnlocksPerReport {
		t.Fatalf("expected the first report-size batch, got %d marks", len(marks))
	}
	// Cache synchronization may reveal another tied unlock later, even one
	// whose ID sorts before the cursor's last accepted ID.
	game.unlock("000_LATE_CACHE_ENTRY", "Late cache entry", unlockedAt)
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != savesync.MaxUnlocksPerReport+2 {
		t.Fatalf("expected every new tied unlock after the boundary, got %d marks", len(marks))
	}
}
