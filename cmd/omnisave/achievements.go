package main

import (
	"context"
	"slices"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// maxUnlocksPerReport is what one report may carry, matching what the server
// accepts. A Device with more to say sends the oldest first and the rest on
// the passes that follow.
const maxUnlocksPerReport = 256

// reportAchievements tells the server what this Device has watched a bound
// save's game unlock since the last pass looked. The server decides which
// revision each unlock lands on, so a report is worth sending whether or not
// this pass goes on to commit anything.
//
// The first look at a save only records where its unlock history already
// stood. Achievements earned before Omnisave watched the game belong to
// sessions it has no revisions for, and pinning them all to the oldest
// revision would say something false about that snapshot.
//
// Everything here is best-effort: a Device that cannot read its achievements,
// or cannot report them, still syncs saves exactly as it always did. A failed
// report leaves the watermark where it was, so the next pass retries.
func reportAchievements(
	ctx context.Context,
	scanner *client.Scanner,
	server *remote.Client,
	state *tracking.State,
	local tracking.LocalSave,
	save target.Save,
	discovered target.Target,
	game target.InstalledGame,
	omnisaveID string,
	report *tui.TrackReport,
) {
	// Interactive binding passes no scanner: the sync pass that follows it is
	// where a newly bound save first has its achievements looked at.
	if scanner == nil {
		return
	}
	unlocked, err := scanner.UnlockedAchievements(ctx, discovered, game, save)
	if err != nil || len(unlocked) == 0 {
		return
	}
	slices.SortFunc(unlocked, func(left, right target.Achievement) int {
		if !left.UnlockedAt.Equal(right.UnlockedAt) {
			return left.UnlockedAt.Compare(right.UnlockedAt)
		}
		return strings.Compare(left.ID, right.ID)
	})
	through, hasLooked := state.AchievementsSeen(local)
	if !hasLooked {
		state.RecordAchievementsSeen(local, unlocked[len(unlocked)-1].UnlockedAt)
		return
	}
	// An unlock is new when it happened strictly after everything this Device
	// had already accounted for.
	unlocks := make([]omnisave.AchievementUnlock, 0, len(unlocked))
	for _, achievement := range unlocked {
		if !achievement.UnlockedAt.After(through) {
			continue
		}
		unlocks = append(unlocks, omnisave.AchievementUnlock{
			ID:          achievement.ID,
			Name:        achievement.Name,
			Description: achievement.Description,
			UnlockedAt:  achievement.UnlockedAt,
		})
	}
	if len(unlocks) == 0 {
		return
	}
	// A Device catching up after a long time away can have more to say than
	// one report may carry. Oldest first, a batch at a time: the watermark
	// only ever advances over what was actually accepted, so the next pass
	// picks up exactly where this one stopped.
	if len(unlocks) > maxUnlocksPerReport {
		unlocks = unlocks[:maxUnlocksPerReport]
	}
	recorded, err := server.ReportAchievements(ctx, omnisaveID, unlocks)
	if err != nil {
		return
	}
	state.RecordAchievementsSeen(local, unlocks[len(unlocks)-1].UnlockedAt)
	// Only what the server had not already recorded is worth a line: another
	// Device may have reported the same unlock first.
	names := make([]string, 0, len(recorded))
	for _, achievement := range recorded {
		names = append(names, achievement.Name)
	}
	report.Unlocked(local.GameTitle, names)
}
