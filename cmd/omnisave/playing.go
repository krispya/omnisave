package main

import (
	"context"
	"sort"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// trackedRunningGames lists the tracked games a process sweep can identify:
// each with its own install root and its target's, so an emulated game is
// recognized by its frontend's process.
func trackedRunningGames(state *tracking.State, scans []client.TargetScan) []running.Game {
	var games []running.Game
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if _, tracked := state.Games[discovered.Game.ID]; !tracked {
				continue
			}
			games = append(games, running.Game{
				ID:    discovered.Game.ID,
				Roots: []string{discovered.Game.InstallRoot, scan.Target.Root},
			})
		}
	}
	return games
}

// playingNow reports which of the games are being played, from one process
// sweep. Detection is best-effort and fails open: a sweep that errors gates
// nothing, because the staged placement re-verifies local content anyway and
// a platform whose process list is unreadable must not defer pulls forever.
func playingNow(ctx context.Context, detector *running.Detector, games []running.Game) map[string]bool {
	if detector == nil || len(games) == 0 {
		return nil
	}
	playing, err := detector.Playing(ctx, games)
	if err != nil {
		return nil
	}
	return playing
}

// presenceWatch is what a pass hands the watch loop so the server's picture
// of playing games stays fresh between passes: the tracked games to sweep,
// their Library identities, and the device doing the reporting.
type presenceWatch struct {
	deviceID string
	games    []running.Game
	serverID map[string]string
}

// playingServerGameIDs maps a playing sweep to the Library games it reports,
// sorted so equal pictures compare equal.
func playingServerGameIDs(serverID map[string]string, playing map[string]bool) []string {
	ids := make([]string, 0, len(playing))
	for localID, isPlaying := range playing {
		if isPlaying && serverID[localID] != "" {
			ids = append(ids, serverID[localID])
		}
	}
	sort.Strings(ids)
	return ids
}

// reportPlaying tells the server what this device sees being played right
// now. Best-effort: presence is a courtesy the Dash shows and ages out on
// its own, so a failed report is not worth failing a pass over.
func reportPlaying(ctx context.Context, server *remote.Client, presence presenceWatch, playing map[string]bool) {
	if presence.deviceID == "" {
		return
	}
	_ = server.ReportDeviceStatus(ctx, presence.deviceID, playingServerGameIDs(presence.serverID, playing))
}
