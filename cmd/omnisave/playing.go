package main

import (
	"context"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// presenceWatch is what a pass hands the watch loop so the server's picture
// of playing games stays fresh between passes: the adapter matchers to sweep
// with, each tracked game's Library identity and display title, and the
// device doing the reporting.
type presenceWatch struct {
	deviceID string
	matchers []running.Matcher
	serverID map[string]string
	titles   map[string]string
}

// trackedPresence assembles the presence picture from one pass's scans.
// Built after the library sync so Library identities are resolved and a
// freshly tracked game reports on its very first pass.
func trackedPresence(scanner *client.Scanner, state *tracking.State, scans []client.TargetScan) presenceWatch {
	presence := presenceWatch{
		deviceID: state.Device.ID,
		serverID: make(map[string]string),
		titles:   make(map[string]string),
	}
	tracked := func(gameID string) bool {
		_, ok := state.Games[gameID]
		return ok
	}
	presence.matchers = scanner.PlayingMatchers(scans, tracked)
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if !tracked(discovered.Game.ID) {
				continue
			}
			presence.serverID[discovered.Game.ID] = state.Games[discovered.Game.ID].ServerGameID
			presence.titles[discovered.Game.ID] = discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
		}
	}
	return presence
}

// passPlaying is what one pass learned about playing games: the presence
// picture it assembled, the process sweep it ran — shared by the pull gate
// and the presence report, so a pass costs one sweep — and the games whose
// pulls the gate held back for their exit. swept distinguishes an empty
// picture from a sweep that failed, which must not be reported as one.
type passPlaying struct {
	presence presenceWatch
	playing  map[string]bool
	swept    bool
	waiting  []string
}

// pullGate holds automatic pulls back while their game is being played
// (FDR-005, decision 13) and remembers which games are waiting, so the
// watch loop knows whose exit resolves them. A nil gate — no detector, or
// a sweep that failed — holds nothing: detection fails open, because a
// platform whose process list is unreadable must not defer pulls forever.
type pullGate struct {
	playing map[string]bool
	waiting []string
}

// holdPull reports whether the game's pull must wait for it to close,
// recording the game as waiting when it must.
func (g *pullGate) holdPull(gameID string) bool {
	if g == nil || !g.playing[gameID] {
		return false
	}
	if !slices.Contains(g.waiting, gameID) {
		g.waiting = append(g.waiting, gameID)
	}
	return true
}

// deferredGameExited reports whether a game holding back a pull has closed
// since the pass that deferred it — the moment that pull can safely land.
func deferredGameExited(deferred []string, playing map[string]bool) bool {
	for _, gameID := range deferred {
		if !playing[gameID] {
			return true
		}
	}
	return false
}

// playingNow reports which tracked games are being played, from one process
// sweep. A failed sweep is an error, not an empty picture: callers skip
// their report entirely, because reporting nobody playing would clear valid
// presence for a game that is still running.
func playingNow(ctx context.Context, detector *running.Detector, matchers []running.Matcher) (map[string]bool, error) {
	if detector == nil || len(matchers) == 0 {
		return nil, nil
	}
	return detector.Playing(ctx, matchers...)
}

// playingGames maps a playing sweep through a lookup — Library game IDs for
// the server's report, display titles for the watch view's playing marker —
// sorted so equal pictures compare equal.
func playingGames(lookup map[string]string, playing map[string]bool) []string {
	names := make([]string, 0, len(playing))
	for localID, isPlaying := range playing {
		if isPlaying && lookup[localID] != "" {
			names = append(names, lookup[localID])
		}
	}
	slices.Sort(names)
	return names
}

// reportPlaying tells the server what this device sees being played right
// now. Best-effort: presence is a courtesy the Dash shows and ages out on
// its own, so a failed report is not worth failing a pass over.
func reportPlaying(ctx context.Context, server *remote.Client, presence presenceWatch, playing map[string]bool) {
	if presence.deviceID == "" {
		return
	}
	_ = server.ReportDeviceStatus(ctx, presence.deviceID, playingGames(presence.serverID, playing))
}
