package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestTrackReportNestsEventSentencesUnderEachGameOnce(t *testing.T) {
	report := &TrackReport{}
	report.Added("Slay the Spire 2", "Slay the Spire II")
	report.Seeded("Slay the Spire 2", "Slay the Spire II save")
	report.Linked("Project Zomboid", "")
	report.Unbound("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Slay the Spire 2",
		"      Added to the library as Slay the Spire II",
		"      Save seeded as Slay the Spire II save",
		"  ✓ Project Zomboid",
		"      Linked to the library",
		"      Save needs omnisave-client bind",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected glyphed game titles with capitalized event sentences beneath, got:\n%s", rendered)
	}
}

func TestTrackReportSeedingWithoutADisplayNameStaysGeneric(t *testing.T) {
	report := &TrackReport{}
	report.Seeded("Slay the Spire 2", "")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save seeded to a new Omnisave") {
		t.Fatalf("expected an unnamed seed to fall back to the generic sentence, got %q", rendered)
	}
}

func TestTrackReportExplainsWhenAnAddedGameHasNoSave(t *testing.T) {
	report := &TrackReport{}
	report.Added("Project Zomboid", "")
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Project Zomboid",
		"      Added to the library",
		"      No save available to sync",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected the missing save beneath the added game, got:\n%s", rendered)
	}
}

func TestTrackReportShowsAnAutomaticExistingSaveMatch(t *testing.T) {
	report := &TrackReport{}
	report.Rebound("Chrono Trigger", "New Game+")

	rendered := report.render()
	if len(rendered) != 2 || rendered[0] != "  ✓ Chrono Trigger" {
		t.Fatalf("expected a quietly synced game to earn the success glyph, got %q", rendered)
	}
	if !strings.Contains(rendered[1], "Save matches the head of New Game+ and is resyncing") {
		t.Fatalf("expected the automatic match beneath the game, got %q", rendered)
	}
}

func TestTrackReportNamesBothStaleMatchOutcomes(t *testing.T) {
	report := &TrackReport{}
	report.FastForwarded("Chrono Trigger", "New Game+")
	report.Forked("Chrono Trigger", "New Game+ (fork)")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save jumped to the head of New Game+") ||
		!strings.Contains(rendered, "Save forked as New Game+ (fork)") {
		t.Fatalf("expected both stale-match outcomes to name their Omnisaves, got %q", rendered)
	}
}

func TestTrackReportExplainsAServerDeletionAboveItsUntrackLine(t *testing.T) {
	report := &TrackReport{}
	report.SaveDeleted("Chrono Trigger")
	report.Removed("Chrono Trigger")

	rendered := report.render()
	if len(rendered) != 3 || rendered[0] != "  - Chrono Trigger" {
		t.Fatalf("expected the removal glyph on the game title, got %q", rendered)
	}
	if !strings.Contains(rendered[1], "Bound Omnisave deleted on the server") ||
		!strings.Contains(rendered[2], "Untracked from this device") {
		t.Fatalf("expected the deletion reason above the untrack line, got %q", rendered)
	}
}

func TestTrackReportKeepsRunWideFailuresAboveTheGameGroups(t *testing.T) {
	report := &TrackReport{}
	report.Failed("Chrono Trigger", errors.New("resolve timed out"))
	report.BindingFailed(errors.New("listing Omnisaves failed"))

	rendered := report.render()
	if len(rendered) != 3 || !strings.Contains(rendered[0], "✗ Save binding failed — listing Omnisaves failed") {
		t.Fatalf("expected the run-wide failure first, got %q", rendered)
	}
	if rendered[1] != "  ✗ Chrono Trigger" || !strings.Contains(rendered[2], "Failed — resolve timed out") {
		t.Fatalf("expected the game failure glyphed on its title with the sentence beneath, got %q", rendered)
	}
}

func TestTrackReportLetsASaveFailureClaimTheGameGlyph(t *testing.T) {
	report := &TrackReport{}
	report.Added("Stardew Valley", "")
	report.SaveFailed("Stardew Valley", errors.New("upload rejected"))

	rendered := report.render()
	if len(rendered) != 3 || rendered[0] != "  ✗ Stardew Valley" {
		t.Fatalf("expected the failure to claim the game glyph, got %q", rendered)
	}
	if !strings.Contains(rendered[2], "Save failed — upload rejected") {
		t.Fatalf("expected the save failure sentence beneath the game, got %q", rendered)
	}
}
