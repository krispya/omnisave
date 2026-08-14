package steam

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/keyvalues"
)

// Steam caches every app's achievement state under its install root:
// UserGameStats_<account>_<app>.bin holds an AchievementTimes map of unlock
// times keyed by the achievement's bit, and UserGameStatsSchema_<app>.bin
// names those bits. Both are rewritten when the client syncs stats with
// Valve, which is what makes a local read enough — no key, no account, and no
// network. The times come from Valve's servers, so they are the same instant
// on every device the account plays on.
const statsDirectory = "appcache/stats"

// Caches are small: the largest schema in a long Steam library is a few
// hundred kilobytes. The limit keeps a damaged or hostile file from being
// read into memory whole.
const maxCacheBytes = 8 << 20

// preferredLanguage is the display language read out of a schema's localized
// name and description. Steam writes every language it has into every cache;
// picking one keeps a mark's text stable across devices, which matters
// because the mark is shared state.
const preferredLanguage = "english"

// UnlockedAchievements reports every achievement Steam's cache shows unlocked
// for the account this save belongs to, in unlock order. It is best-effort by
// contract: no cache, no schema, a shape Steam changed, and an achievement
// the schema no longer names all read as nothing rather than as failure.
func (a *Adapter) UnlockedAchievements(
	ctx context.Context,
	discovered target.Target,
	game target.InstalledGame,
	save target.Save,
) ([]target.Achievement, error) {
	appID, hasAppID := game.Identity.Identifier("steam.app")
	if discovered.Adapter != adapterName || discovered.Root == "" || game.TargetID != discovered.ID || !hasAppID {
		return nil, fmt.Errorf("invalid Steam game")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stats := filepath.Join(discovered.Root, filepath.FromSlash(statsDirectory))
	account, found, err := statsAccount(stats, appID, save.Metadata["account_id"])
	if err != nil || !found {
		return nil, err
	}
	unlocked, err := readCache(filepath.Join(stats, "UserGameStats_"+account+"_"+appID+".bin"))
	if err != nil || unlocked == nil {
		return nil, err
	}
	// The unlock cache is a few hundred bytes and the schema that names its
	// bits is up to a thousand times that, so a game with nothing unlocked —
	// most of a library, most of the time — is answered without opening it.
	times := unlockTimes(unlocked)
	if len(times) == 0 {
		return nil, nil
	}
	schema, err := readCache(filepath.Join(stats, "UserGameStatsSchema_"+appID+".bin"))
	if err != nil || schema == nil {
		return nil, err
	}

	var achievements []target.Achievement
	for _, unlock := range times {
		bit := schema.Child(appID, "stats", unlock.stat, "bits", unlock.bit)
		name := bit.Child("name").Text()
		// A bit the schema does not name cannot be shown to anyone, and a
		// stale schema is the ordinary reason for one: the cache outlives
		// achievements a game update removed.
		if name == "" {
			continue
		}
		achievements = append(achievements, target.Achievement{
			ID:          name,
			Name:        localized(bit.Child("display", "name"), name),
			Description: localized(bit.Child("display", "desc"), ""),
			UnlockedAt:  unlock.at,
		})
	}
	sort.Slice(achievements, func(left, right int) bool {
		if !achievements[left].UnlockedAt.Equal(achievements[right].UnlockedAt) {
			return achievements[left].UnlockedAt.Before(achievements[right].UnlockedAt)
		}
		return achievements[left].ID < achievements[right].ID
	})
	return achievements, nil
}

// unlockTime is one entry of the unlock cache: the achievement stat and bit
// it belongs to, which is how the schema names it, and when it happened.
type unlockTime struct {
	stat string
	bit  string
	at   time.Time
}

// unlockTimes reads every AchievementTimes entry a cache holds. A game's
// achievements are spread across numbered stat blocks, and only the blocks
// with unlocks carry the map.
func unlockTimes(cached *keyvalues.Value) []unlockTime {
	var unlocks []unlockTime
	cache := cached.Child("cache")
	for _, stat := range cache.Keys() {
		times := cache.Child(stat, "AchievementTimes")
		for _, bit := range times.Keys() {
			seconds, isTime := times.Child(bit).Int()
			if !isTime || seconds <= 0 {
				continue
			}
			unlocks = append(unlocks, unlockTime{stat: stat, bit: bit, at: time.Unix(seconds, 0).UTC()})
		}
	}
	return unlocks
}

// statsAccount resolves whose cache to read. A Steam Cloud save names its
// account outright; a save discovered another way (a save profile pointing at
// a path outside Cloud) does not, and is answered only when one account on
// this machine leaves no doubt.
func statsAccount(statsDirectory, appID, saveAccount string) (string, bool, error) {
	if saveAccount != "" {
		return saveAccount, true, nil
	}
	entries, err := os.ReadDir(statsDirectory)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	suffix := "_" + appID + ".bin"
	var accounts []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "UserGameStats_") || !strings.HasSuffix(name, suffix) {
			continue
		}
		accounts = append(accounts, strings.TrimSuffix(strings.TrimPrefix(name, "UserGameStats_"), suffix))
	}
	if len(accounts) != 1 {
		return "", false, nil
	}
	return accounts[0], true, nil
}

// readCache parses one Steam cache. An absent file is not an error: caches
// exist only for apps the client has synced stats for.
func readCache(path string) (*keyvalues.Value, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	document, err := keyvalues.Read(file, maxCacheBytes)
	if err != nil {
		// A cache Steam is midway through rewriting, or one whose shape moved
		// on, costs this pass its marks and nothing else.
		return nil, nil
	}
	return document, nil
}

// localized picks one language out of a schema's translated text. Steam also
// stores the game's own localization token beside the translations, which is
// never a name anyone should read.
func localized(value *keyvalues.Value, fallback string) string {
	if text := strings.TrimSpace(value.Text()); text != "" {
		return text
	}
	if text := strings.TrimSpace(value.Child(preferredLanguage).Text()); text != "" {
		return text
	}
	for _, language := range value.Keys() {
		if language == "token" {
			continue
		}
		if text := strings.TrimSpace(value.Child(language).Text()); text != "" {
			return text
		}
	}
	return fallback
}

var _ target.Achievements = (*Adapter)(nil)
