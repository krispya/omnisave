package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
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

// unlocks the game so far, oldest first, as a store's records would show it.
func (a *achievingAdapter) unlock(id, name string, at time.Time) {
	a.unlocked = append(a.unlocked, target.Achievement{ID: id, Name: name, UnlockedAt: at})
}

// newAchievingFixture is the sync fixture on a target that can see
// achievements, with a game whose history already has some.
func newAchievingFixture(t *testing.T, content string) (bindingFixture, *achievingAdapter, *client.Scanner) {
	t.Helper()
	fixture := newSyncFixture(t, content)
	fixture.scans[0].Target.Adapter = "achieving"
	game := fixture.state.Games["local-game-1"]
	game.Adapter = "achieving"
	fixture.state.Games["local-game-1"] = game
	adapter := &achievingAdapter{}
	return fixture, adapter, client.NewScanner(nil, adapter)
}

func syncWithAchievements(t *testing.T, scanner *client.Scanner, server *remote.Client, fixture *bindingFixture) {
	t.Helper()
	ctx := context.Background()
	outcome, confirmed := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, &tui.TrackReport{})
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	if err := reconcileSaves(ctx, scanner, server, &fixture.state, fixture.scans, confirmed,
		&outcome, &tui.TrackReport{}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
}

func achievementsOf(t *testing.T, server *remote.Client, fixture *bindingFixture) []omnisave.Achievement {
	t.Helper()
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, isBound := fixture.state.BindingFor(local)
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
	server := newRealServer(t)
	fixture, game, scanner := newAchievingFixture(t, "saved-game-content")
	game.unlock("VG_DAY1", "Good first day.", time.Now().Add(-30*24*time.Hour))

	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 0 {
		t.Fatalf("expected nothing marked from before this device watched, got %+v", marks)
	}

	// Earned while watching, and the game saved afterwards. The pass commits
	// that save and reports the unlock, which lands on the new revision.
	game.unlock("VG_DAY2", "Back to work.", time.Now())
	if err := os.WriteFile(fixture.localPath, []byte("saved-game-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	syncWithAchievements(t, scanner, server, &fixture)

	marks := achievementsOf(t, server, &fixture)
	if len(marks) != 1 || marks[0].ID != "VG_DAY2" {
		t.Fatalf("expected only the unlock this device watched, got %+v", marks)
	}
	if marks[0].RevisionID == nil {
		t.Fatalf("expected the unlock placed on a revision, got %+v", marks[0])
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
	refuse := true
	server := newInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if refuse && request.Method == http.MethodPost &&
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

	refuse = false
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != 1 || marks[0].ID != "VG_DAY2" {
		t.Fatalf("expected the unlock reported again once the server answered, got %+v", marks)
	}
}

// Whole-second store timestamps are not unique cursors. If a report-size
// boundary cuts through a group earned in the same second, later passes still
// have to carry the rest of that group.
func TestSameSecondUnlocksContinueAcrossReports(t *testing.T) {
	server := newRealServer(t)
	fixture, game, scanner := newAchievingFixture(t, "saved-game-content")
	game.unlock("BEFORE_WATCH", "Before watch", time.Now().Add(-time.Hour))
	syncWithAchievements(t, scanner, server, &fixture)

	unlockedAt := time.Now()
	for index := range maxUnlocksPerReport + 1 {
		id := fmt.Sprintf("ACHIEVEMENT_%03d", index)
		game.unlock(id, id, unlockedAt)
	}

	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != maxUnlocksPerReport {
		t.Fatalf("expected the first report-size batch, got %d marks", len(marks))
	}
	// Cache synchronization may reveal another tied unlock later, even one
	// whose ID sorts before the cursor's last accepted ID.
	game.unlock("000_LATE_CACHE_ENTRY", "Late cache entry", unlockedAt)
	syncWithAchievements(t, scanner, server, &fixture)
	if marks := achievementsOf(t, server, &fixture); len(marks) != maxUnlocksPerReport+2 {
		t.Fatalf("expected every new tied unlock after the boundary, got %d marks", len(marks))
	}
}
