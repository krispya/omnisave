package httpapi

import (
	"testing"
	"time"
)

func TestAPlayingReportAgesOutInsteadOfBlockingForever(t *testing.T) {
	now := time.Now()
	presence := newDevicePresence()
	presence.now = func() time.Time { return now }

	if !presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected the first playing report to read as a change")
	}
	if _, playing := presence.playing("device-1", "game-1"); !playing {
		t.Fatal("expected a fresh report to read as playing")
	}

	now = now.Add(presenceTTL)
	if _, playing := presence.playing("device-1", "game-1"); playing {
		t.Fatal("expected a report older than the credibility window to clear itself")
	}
	// A stale report already reads as "not playing", so clearing it is not
	// news — only a live session is.
	if presence.report("device-1", nil) {
		t.Fatal("expected clearing a stale report to publish nothing")
	}
	if !presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected a new session after staleness to read as a change")
	}
}

func TestReaffirmingTheSamePictureIsNotAChange(t *testing.T) {
	presence := newDevicePresence()
	presence.report("device-1", []string{"game-1"})
	if presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected a re-affirmation to publish nothing")
	}
	if !presence.report("device-1", nil) {
		t.Fatal("expected the session ending to read as a change")
	}
	if _, playing := presence.playing("device-1", "game-1"); playing {
		t.Fatal("expected the cleared report to stop reading as playing")
	}
}
