package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

// An achievement is an event on the play timeline, not part of a snapshot's
// bytes. Placing it means finding the first save committed at or after it:
// that revision is the earliest one known to carry the achievement, and
// everything before it in the history predates the unlock.
//
// These stories need commits minutes apart, so the history is written through
// the repository, where each revision's time is stated rather than taken from
// the clock. Every rule under test still runs through the service.

func session() time.Time {
	return time.Date(2026, 3, 7, 19, 0, 0, 0, time.UTC)
}

type history struct {
	saves      omnisave.Service
	repository *storagetest.MemoryRepository
	saveID     string
	current    *string
}

func playedSave(t *testing.T) *history {
	t.Helper()
	repository := storagetest.NewMemoryRepository()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(context.Background(), omnisave.CreateOmnisave{GameID: "firewatch"})
	if err != nil {
		t.Fatal(err)
	}
	return &history{saves: saves, repository: repository, saveID: save.ID}
}

// commitAt writes one revision at a stated time and makes it current.
func (h *history) commitAt(t *testing.T, id string, at time.Time) string {
	t.Helper()
	revision := omnisave.Revision{
		ID:         id,
		OmnisaveID: h.saveID,
		ParentID:   h.current,
		CreatedAt:  at,
	}
	if err := h.repository.CommitRevision(context.Background(), h.current, revision, false); err != nil {
		t.Fatal(err)
	}
	h.current = &revision.ID
	return revision.ID
}

func (h *history) record(t *testing.T, unlocks ...omnisave.AchievementUnlock) []omnisave.Achievement {
	t.Helper()
	recorded, err := h.saves.RecordAchievements(context.Background(), h.saveID, unlocks)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}

func (h *history) marks(t *testing.T) []omnisave.Achievement {
	t.Helper()
	marks, err := h.saves.ListAchievements(context.Background(), h.saveID)
	if err != nil {
		t.Fatal(err)
	}
	return marks
}

func unlock(id, name string, at time.Time) omnisave.AchievementUnlock {
	return omnisave.AchievementUnlock{ID: id, Name: name, UnlockedAt: at}
}

func TestAnUnlockIsMarkedOnTheFirstSaveCommittedAfterIt(t *testing.T) {
	played := playedSave(t)
	played.commitAt(t, "before-lunch", session())
	afterwards := played.commitAt(t, "after-dinner", session().Add(4*time.Hour))

	// Earned in the afternoon: the evening's save is the first snapshot that
	// can be said to carry it, and the morning's plainly predates it.
	played.record(t, unlock("VG_DAY1", "Good first day.", session().Add(2*time.Hour)))

	marks := played.marks(t)
	if len(marks) != 1 || marks[0].RevisionID == nil || *marks[0].RevisionID != afterwards {
		t.Fatalf("expected the unlock marked on the following save, got %+v", marks)
	}
	if !marks[0].UnlockedAt.Equal(session().Add(2 * time.Hour)) {
		t.Fatalf("expected the reported unlock time kept, got %s", marks[0].UnlockedAt)
	}
}

func TestAnUnlockReportedBeforeTheSaveIsWrittenWaitsForTheNextCommit(t *testing.T) {
	played := playedSave(t)
	played.commitAt(t, "before-lunch", session())

	// The game recorded the unlock and has not written its save yet, which is
	// the ordinary order: the toast comes before the autosave.
	played.record(t, unlock("VG_DAY2", "Back to work.", session().Add(time.Hour)))
	if waiting := played.marks(t); len(waiting) != 1 || waiting[0].RevisionID != nil {
		t.Fatalf("expected the unlock to wait for a revision, got %+v", waiting)
	}

	next := played.commitAt(t, "after-dinner", session().Add(4*time.Hour))
	claimed := played.marks(t)
	if len(claimed) != 1 || claimed[0].RevisionID == nil || *claimed[0].RevisionID != next {
		t.Fatalf("expected the next commit to claim the waiting unlock, got %+v", claimed)
	}
}

func TestRepeatingAReportNeverMovesAMark(t *testing.T) {
	played := playedSave(t)
	first := played.commitAt(t, "before-lunch", session())
	earned := unlock("VG_DAY1", "Good first day.", session().Add(-time.Hour))

	if recorded := played.record(t, earned); len(recorded) != 1 {
		t.Fatalf("expected the first report to add the unlock, got %+v", recorded)
	}
	played.commitAt(t, "after-dinner", session().Add(4*time.Hour))

	// A second Device — or the same one after a crash — repeats the report.
	// The mark stays on the save it was first placed against.
	if again := played.record(t, earned); len(again) != 0 {
		t.Fatalf("expected a repeated report to add nothing, got %+v", again)
	}
	marks := played.marks(t)
	if len(marks) != 1 || marks[0].RevisionID == nil || *marks[0].RevisionID != first {
		t.Fatalf("expected the original placement kept, got %+v", marks)
	}
}

func TestAReportWithNothingUsableIsRefused(t *testing.T) {
	played := playedSave(t)
	at := session()
	refusals := map[string][]omnisave.AchievementUnlock{
		"nothing reported": {},
		"no identity":      {unlock("", "Good first day.", at)},
		"no name":          {unlock("VG_DAY1", "", at)},
		"no unlock time":   {unlock("VG_DAY1", "Good first day.", time.Time{})},
		"same id twice":    {unlock("VG_DAY1", "Good first day.", at), unlock("VG_DAY1", "Again", at)},
	}
	for reason, unlocks := range refusals {
		_, err := played.saves.RecordAchievements(context.Background(), played.saveID, unlocks)
		if !errors.Is(err, omnisave.ErrInvalid) {
			t.Fatalf("expected %s refused, got %v", reason, err)
		}
	}
	_, err := played.saves.RecordAchievements(context.Background(), "no-such-save",
		[]omnisave.AchievementUnlock{unlock("VG_DAY1", "Good first day.", at)})
	if !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("expected an unknown save refused, got %v", err)
	}
}
