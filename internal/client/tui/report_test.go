package tui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTrackReportGivesEachGameOneAlignedLine(t *testing.T) {
	report := &TrackReport{}
	report.Added("Slay the Spire 2", "Slay the Spire II")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())
	report.Linked("Project Zomboid", "")
	report.Unbound("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Slay the Spire 2  Added to the library as Slay the Spire II · Save 1 · synced just now",
		"  ✓ Project Zomboid   Linked to the library · Save needs omnisave-client bind",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected one aligned line per game with the bound state last, got:\n%s", rendered)
	}
}

func TestQuietBoundSavesNameTheirOmnisaveAndAge(t *testing.T) {
	report := &TrackReport{}
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now().Add(-2*time.Minute))

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "✓ Slay the Spire 2  Save 1 · synced 2m ago") {
		t.Fatalf("expected the quiet line to name the Omnisave with its age, got:\n%s", rendered)
	}

	legacy := &TrackReport{}
	legacy.SyncedWith("Chrono Trigger", "New Game+", time.Time{})
	if !strings.Contains(strings.Join(legacy.render(), "\n"), "New Game+ · up to date") {
		t.Fatalf("expected a binding without a timestamp to fall back, got %q", legacy.render())
	}
}

func TestTrackReportShowsUpToDateGamesInsteadOfHidingThem(t *testing.T) {
	report := &TrackReport{}
	report.UpToDate("Slay the Spire 2")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "✓ Slay the Spire 2  Up to date") {
		t.Fatalf("expected a registered game with no binding info to show up to date, got:\n%s", rendered)
	}
}

func TestTrackReportStreamsSnapshotsToLiveViews(t *testing.T) {
	var snapshots []ReportSnapshot
	report := &TrackReport{OnUpdate: func(snapshot ReportSnapshot) {
		snapshots = append(snapshots, snapshot)
	}}
	report.UpToDate("Slay the Spire 2")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())

	if len(snapshots) < 2 {
		t.Fatalf("expected a snapshot per event, got %d", len(snapshots))
	}
	last := strings.Join(ComposeReport(snapshots[len(snapshots)-1], time.Now()), "\n")
	if !strings.Contains(last, "Save 1 · synced just now") {
		t.Fatalf("expected the final snapshot to carry the bound state, got:\n%s", last)
	}
}

func TestTrackReportExplainsWhenAnAddedGameHasNoSave(t *testing.T) {
	report := &TrackReport{}
	report.Added("Project Zomboid", "")
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	if rendered != "  + Project Zomboid  Added to the library · No save available" {
		t.Fatalf("expected the missing save beside the added game, got:\n%s", rendered)
	}
}

func TestTrackReportGivesSavelessGamesTheIdleGlyph(t *testing.T) {
	report := &TrackReport{}
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	if rendered != "  · Project Zomboid  No save available" {
		t.Fatalf("expected the idle glyph on a saveless game, got:\n%s", rendered)
	}
}

func TestTrackReportNamesTheJumpPreservationFork(t *testing.T) {
	report := &TrackReport{}
	report.Forked("Chrono Trigger", "New Game+ (diverged)")
	report.SyncedWith("Chrono Trigger", "New Game+", time.Now())

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save forked as New Game+ (diverged) · New Game+ · synced just now") {
		t.Fatalf("expected the preservation fork named beside the bound state, got:\n%s", rendered)
	}
}

func TestTrackReportSaysWhenABindingHasNothingSyncedYet(t *testing.T) {
	report := &TrackReport{}
	report.BoundUnsynced("Chrono Trigger", "New Game+")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save bound to New Game+ with nothing synced yet") {
		t.Fatalf("expected the unsynced binding sentence, got:\n%s", rendered)
	}
}

func TestTrackReportExplainsAServerDeletionOnTheUntrackLine(t *testing.T) {
	report := &TrackReport{}
	report.SaveDeleted("Chrono Trigger")
	report.Removed("Chrono Trigger")

	rendered := report.render()
	if len(rendered) != 1 || !strings.HasPrefix(rendered[0], "  - Chrono Trigger") {
		t.Fatalf("expected the removal glyph on the game line, got %q", rendered)
	}
	if !strings.Contains(rendered[0], "Bound Omnisave deleted on the server · Untracked from this device") {
		t.Fatalf("expected the deletion reason beside the untrack status, got %q", rendered)
	}
}

func TestTrackReportSpeaksSyncExceptions(t *testing.T) {
	report := &TrackReport{}
	report.Diverged("Chrono Trigger", "New Game+")
	report.Behind("Stardew Valley", "Farm run")

	rendered := strings.Join(report.render(), "\n")
	for _, sentence := range []string{
		"Save diverged from New Game+, run omnisave-client track to resolve",
		"Save is behind Farm run, run omnisave-client track to resolve",
	} {
		if !strings.Contains(rendered, sentence) {
			t.Fatalf("expected %q in the report, got:\n%s", sentence, rendered)
		}
	}
	if !strings.Contains(rendered, "  ○ Chrono Trigger") {
		t.Fatalf("expected the diverged game to carry the attention glyph, got:\n%s", rendered)
	}
}

func TestTrackReportKeepsRunWideFailuresAboveTheGameLines(t *testing.T) {
	report := &TrackReport{}
	report.Failed("Chrono Trigger", errors.New("resolve timed out"))
	report.BindingFailed(errors.New("listing Omnisaves failed"))

	rendered := report.render()
	if len(rendered) != 2 || !strings.Contains(rendered[0], "✗ Save binding failed — listing Omnisaves failed") {
		t.Fatalf("expected the run-wide failure first, got %q", rendered)
	}
	if !strings.HasPrefix(rendered[1], "  ✗ Chrono Trigger") ||
		!strings.Contains(rendered[1], "Failed — resolve timed out") {
		t.Fatalf("expected the game failure glyphed on its line, got %q", rendered)
	}
}

func TestTrackReportLetsASaveFailureClaimTheGameGlyph(t *testing.T) {
	report := &TrackReport{}
	report.Added("Stardew Valley", "")
	report.SaveFailed("Stardew Valley", errors.New("upload rejected"))

	rendered := report.render()
	if len(rendered) != 1 || !strings.HasPrefix(rendered[0], "  ✗ Stardew Valley") {
		t.Fatalf("expected the failure to claim the game glyph, got %q", rendered)
	}
	if !strings.Contains(rendered[0], "Save failed — upload rejected") {
		t.Fatalf("expected the save failure beside the game, got %q", rendered)
	}
}
