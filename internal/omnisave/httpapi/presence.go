package httpapi

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"time"
)

// presenceTTL is how long a playing report stays credible without being
// re-affirmed. Devices re-affirm every minute while a game runs, so a report
// older than this belongs to a device that crashed or lost its network —
// staleness must unblock, never block.
const presenceTTL = 3 * time.Minute

// devicePresence remembers, in memory only, which games each device recently
// reported as being played. Presence is liveness, not provenance: a server
// restart forgets it, and the next re-affirmation rebuilds it.
//
// The server is the only clock. A report ages out here, actively: a timer
// fires at the earliest expiry, sweeps the stale reports, and announces the
// change through expired — so readers render what they were last told and
// never do time math of their own (ADR-001's authority extended to time).
type devicePresence struct {
	mu      sync.Mutex
	now     func() time.Time
	reports map[string]presenceReport
	timer   *time.Timer
	expired func()
}

type presenceReport struct {
	playing map[string]bool
	at      time.Time
}

// newDevicePresence builds the store; expired is called (from the timer's
// goroutine, without the lock) whenever aging alone changed what readers see.
func newDevicePresence(expired func()) *devicePresence {
	return &devicePresence{now: time.Now, reports: make(map[string]presenceReport), expired: expired}
}

// report stores one device's playing set and returns whether the effective
// picture changed — what decides if the change is worth an event.
func (p *devicePresence) report(deviceID string, gameIDs []string) bool {
	playing := make(map[string]bool, len(gameIDs))
	for _, gameID := range gameIDs {
		if gameID != "" {
			playing[gameID] = true
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	// Stale entries are already invisible to reads, so drop them while the
	// lock is held anyway — the map stays bounded by live devices.
	for id, report := range p.reports {
		if id != deviceID && now.Sub(report.at) >= presenceTTL {
			delete(p.reports, id)
		}
	}
	previous, existed := p.reports[deviceID]
	p.reports[deviceID] = presenceReport{playing: playing, at: now}
	p.armLocked(now)
	if !existed || now.Sub(previous.at) >= presenceTTL {
		// A stale report already reads as "not playing", so only a
		// non-empty successor changes what anyone sees.
		return len(playing) > 0
	}
	return !maps.Equal(previous.playing, playing)
}

// expireStale drops every report past its credibility window and announces
// the change when a live session aged away. The timer calls this; tests may
// call it directly.
func (p *devicePresence) expireStale() {
	p.mu.Lock()
	now := p.now()
	changed := false
	for deviceID, report := range p.reports {
		if now.Sub(report.at) >= presenceTTL {
			changed = changed || len(report.playing) > 0
			delete(p.reports, deviceID)
		}
	}
	p.armLocked(now)
	p.mu.Unlock()
	if changed && p.expired != nil {
		p.expired()
	}
}

// armLocked points the expiry timer at the earliest report's aging-out
// moment, or lets it rest when there is nothing left to age.
func (p *devicePresence) armLocked(now time.Time) {
	var earliest time.Time
	for _, report := range p.reports {
		expiresAt := report.at.Add(presenceTTL)
		if earliest.IsZero() || expiresAt.Before(earliest) {
			earliest = expiresAt
		}
	}
	if earliest.IsZero() {
		if p.timer != nil {
			p.timer.Stop()
		}
		return
	}
	delay := max(earliest.Sub(now), 0)
	if p.timer == nil {
		p.timer = time.AfterFunc(delay, p.expireStale)
		return
	}
	p.timer.Stop()
	p.timer.Reset(delay)
}

// presenceStatus is one device's credible playing picture at a moment.
type presenceStatus struct {
	deviceID string
	playing  []string
	at       time.Time
}

// live returns every device with a credible non-empty playing report, in
// stable order. One lock, one answer: this is what lets a reader learn the
// whole picture without walking games.
func (p *devicePresence) live() []presenceStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var statuses []presenceStatus
	for deviceID, report := range p.reports {
		if len(report.playing) == 0 || now.Sub(report.at) >= presenceTTL {
			continue
		}
		playing := slices.Sorted(maps.Keys(report.playing))
		statuses = append(statuses, presenceStatus{deviceID: deviceID, playing: playing, at: report.at})
	}
	slices.SortFunc(statuses, func(a, b presenceStatus) int {
		return cmp.Compare(a.deviceID, b.deviceID)
	})
	return statuses
}

// playing returns when deviceID last credibly reported gameID as being
// played; false once the report has gone stale.
func (p *devicePresence) playing(deviceID, gameID string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	report, exists := p.reports[deviceID]
	if !exists || !report.playing[gameID] || p.now().Sub(report.at) >= presenceTTL {
		return time.Time{}, false
	}
	return report.at, true
}
