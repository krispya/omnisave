package steam_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
)

// Steam writes its caches as binary KeyValues: a type byte, a null-terminated
// key, then the value — objects being a run of children closed by 0x08.

func object(key string, children ...[]byte) []byte {
	node := append([]byte{0x00}, append([]byte(key), 0x00)...)
	for _, child := range children {
		node = append(node, child...)
	}
	return append(node, 0x08)
}

func text(key, value string) []byte {
	node := append([]byte{0x01}, append([]byte(key), 0x00)...)
	return append(node, append([]byte(value), 0x00)...)
}

func number(key string, value int32) []byte {
	node := append([]byte{0x02}, append([]byte(key), 0x00)...)
	return binary.LittleEndian.AppendUint32(node, uint32(value))
}

// achievementBit is one entry of a schema's achievement stat: its API name
// and the translated text Steam ships for every language at once.
func achievementBit(bit, name, english, description string) []byte {
	return object(bit,
		text("name", name),
		object("display",
			object("name", text("token", "NEW_ACHIEVEMENT_1_0_NAME"), text("english", english)),
			object("desc", text("english", description)),
		),
	)
}

// steamCaches lays out an install root holding one app's schema and one
// account's unlock times, and returns the target rooted there.
func steamCaches(t *testing.T, appID, accountID string, schema, unlocked []byte) target.Target {
	t.Helper()
	root := t.TempDir()
	stats := filepath.Join(root, "appcache", "stats")
	if err := os.MkdirAll(stats, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, content []byte) {
		if content == nil {
			return
		}
		if err := os.WriteFile(filepath.Join(stats, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("UserGameStatsSchema_"+appID+".bin", schema)
	write("UserGameStats_"+accountID+"_"+appID+".bin", unlocked)
	return target.Target{ID: "steam:" + root, Adapter: "steam", Root: root}
}

func cloudSave(game target.InstalledGame, accountID string) target.Save {
	return target.Save{
		ID:       game.ID + ":cloud:" + accountID,
		TargetID: game.TargetID,
		GameID:   game.ID,
		Kind:     "cloud",
		Metadata: map[string]string{"account_id": accountID},
	}
}

func unlockedAchievements(t *testing.T, discovered target.Target, game target.InstalledGame, save target.Save) []target.Achievement {
	t.Helper()
	achievements, err := steam.New().UnlockedAchievements(context.Background(), discovered, game, save)
	if err != nil {
		t.Fatal(err)
	}
	return achievements
}

func TestSteamsCachesNameEveryUnlockedAchievementAndWhenItHappened(t *testing.T) {
	schema := object("383870", object("stats", object("1",
		text("type", "ACHIEVEMENTS"),
		object("bits",
			achievementBit("0", "VG_DAY1", `"Good first day."`, "Completed Day 1"),
			achievementBit("1", "VG_DAY2", `"Back to work."`, "Completed Day 2"),
		),
	)))
	// Day 2 was earned first in this cache, so the reader's order proves it
	// reads unlock times rather than the schema's ordering.
	unlocked := object("cache", object("1", object("AchievementTimes",
		number("0", 1772322000),
		number("1", 1772318400),
	)))

	discovered := steamCaches(t, "383870", "67689364", schema, unlocked)
	game := steamGame(discovered, "383870", "")
	achievements := unlockedAchievements(t, discovered, game, cloudSave(game, "67689364"))

	if len(achievements) != 2 {
		t.Fatalf("expected both unlocks, got %v", achievements)
	}
	first := achievements[0]
	if first.ID != "VG_DAY2" || first.Name != `"Back to work."` || first.Description != "Completed Day 2" {
		t.Fatalf("expected the earlier unlock named from the schema, got %+v", first)
	}
	if !first.UnlockedAt.Equal(time.Unix(1772318400, 0).UTC()) {
		t.Fatalf("expected Steam's own unlock time, got %s", first.UnlockedAt)
	}
	if achievements[1].ID != "VG_DAY1" {
		t.Fatalf("expected unlocks in unlock order, got %v", achievements)
	}
}

func TestAnUnlockTheSchemaNoLongerNamesIsLeftOut(t *testing.T) {
	// A game update removed an achievement the account had already earned:
	// the time survives in the cache with nothing left to call it.
	schema := object("730", object("stats", object("2",
		object("bits", achievementBit("0", "PLAY_CS2", "A New Beginning", "Play a match")),
	)))
	unlocked := object("cache", object("2", object("AchievementTimes",
		number("0", 1772318400),
		number("19", 1660999374),
	)))

	discovered := steamCaches(t, "730", "67689364", schema, unlocked)
	game := steamGame(discovered, "730", "")
	achievements := unlockedAchievements(t, discovered, game, cloudSave(game, "67689364"))

	if len(achievements) != 1 || achievements[0].ID != "PLAY_CS2" {
		t.Fatalf("expected only the achievement the schema still names, got %v", achievements)
	}
}

func TestAGameSteamHasNoAchievementCacheForReportsNone(t *testing.T) {
	discovered := steamCaches(t, "2868840", "67689364", nil, nil)
	game := steamGame(discovered, "2868840", "")

	if achievements := unlockedAchievements(t, discovered, game, cloudSave(game, "67689364")); len(achievements) != 0 {
		t.Fatalf("expected no achievements without caches, got %v", achievements)
	}
}

func TestOnlyTheAccountThisSaveBelongsToIsRead(t *testing.T) {
	schema := object("383870", object("stats", object("1",
		object("bits", achievementBit("0", "VG_DAY1", "Good first day.", "Completed Day 1")),
	)))
	unlocked := object("cache", object("1", object("AchievementTimes", number("0", 1772318400))))
	discovered := steamCaches(t, "383870", "67689364", schema, unlocked)

	// A second account shares the machine and has never played the game.
	stats := filepath.Join(discovered.Root, "appcache", "stats")
	if err := os.WriteFile(filepath.Join(stats, "UserGameStats_11111111_383870.bin"),
		object("cache"), 0o644); err != nil {
		t.Fatal(err)
	}

	game := steamGame(discovered, "383870", "")
	if achievements := unlockedAchievements(t, discovered, game, cloudSave(game, "11111111")); len(achievements) != 0 {
		t.Fatalf("expected the other account's empty cache, got %v", achievements)
	}
	if achievements := unlockedAchievements(t, discovered, game, cloudSave(game, "67689364")); len(achievements) != 1 {
		t.Fatalf("expected the playing account's unlock, got %v", achievements)
	}
}

func TestACacheSteamIsRewritingCostsOnlyItsMarks(t *testing.T) {
	schema := object("383870", object("stats", object("1",
		object("bits", achievementBit("0", "VG_DAY1", "Good first day.", "Completed Day 1")),
	)))
	truncated := bytes.TrimRight(object("cache", object("1", object("AchievementTimes",
		number("0", 1772318400)))), "\x08")

	discovered := steamCaches(t, "383870", "67689364", schema, append(truncated, 0x02, 'p'))
	game := steamGame(discovered, "383870", "")

	achievements, err := steam.New().UnlockedAchievements(
		context.Background(), discovered, game, cloudSave(game, "67689364"))
	if err != nil {
		t.Fatalf("a half-written cache must not fail the pass: %v", err)
	}
	if len(achievements) != 0 {
		t.Fatalf("expected nothing readable, got %v", achievements)
	}
}
