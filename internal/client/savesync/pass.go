package savesync

import (
	"context"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// PassOptions tune one sync or watch pass.
type PassOptions struct {
	// Detector sweeps processes once per pass, for playing presence and to
	// defer pulls under a running game. Nil turns both off.
	Detector *running.Detector
	// Prompts answer the questions the pass meets; the zero value is a
	// headless pass that leaves every question waiting.
	Prompts Prompts
	// PushFloor spaces commits of one save; zero commits every change.
	PushFloor time.Duration
}

// PassResult is what one pass leaves its caller.
type PassResult struct {
	Outcome Outcome
	// Watched is every path whose change should start the next pass (see
	// WatchedFiles). It is empty when the pass failed before it could scan.
	Watched []string
	// Played is the pass's process sweep. Its Presence is unreportable when
	// the pass failed before it knew its games.
	Played Played
}

// Pass is one non-interactive run of Save Sync over every tracked game: it
// untracks games deleted on the server, scans, syncs tracking, sweeps for
// running games, and reconciles every confirmed save. It records what it did
// in state and ports.Report; the caller persists state. The pass scans
// through ports.Adapters, so it needs them.
//
// A pass fails only when it cannot scan, or when a prompt returns an error
// — then with what it had learned beside the error. Everything else,
// including an unreachable server, is reported and counted, and the next
// pass retries it.
func Pass(ctx context.Context, ports Ports, state *tracking.State, options PassOptions) (PassResult, error) {
	activity.Report(ctx, "checking library")
	reconciled := ReconcileDeletedGames(ctx, ports.Server, state, ports.Report)
	activity.Report(ctx, "scanning")
	scans, err := ports.Adapters.Scan(ctx)
	if err != nil {
		return PassResult{}, err
	}
	outcome, confirmed := SyncTracking(ctx, ports.Server, state, scans, nil, ports.Report)
	// Presence is built after Library identities resolve, so every game it
	// reports has one.
	played := Played{Presence: TrackedPresence(ports.Adapters, state, scans)}
	// Pull gating fails open, while presence omits a failed sweep.
	var gate *PullGate
	if options.Detector != nil {
		if playing, sweepErr := played.Presence.Sweep(ctx, options.Detector); sweepErr == nil {
			played.Playing = playing
			played.Swept = true
			gate = NewPullGate(playing)
		}
	}
	if outcome.Synced {
		err := Reconcile(ctx, ports, state, scans, confirmed, &outcome, Options{
			Prompts:   options.Prompts,
			Gate:      gate,
			PushFloor: options.PushFloor,
		})
		if err != nil {
			return PassResult{Outcome: outcome, Played: played}, err
		}
	}
	played.Waiting = gate.Waiting()
	outcome.Untracked += reconciled
	return PassResult{Outcome: outcome, Watched: WatchedFiles(state, scans), Played: played}, nil
}
