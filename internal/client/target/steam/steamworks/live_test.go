package steamworks

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestLiveDryRun exercises the real Steamworks connection when a developer
// points it at a game. It writes nothing — the request is a dry run — and
// skips everywhere else, since it needs a running, signed-in Steam client.
//
//	OMNISAVE_LIVE_LIB=<steam_api library> \
//	OMNISAVE_LIVE_APPID=<app id> \
//	OMNISAVE_LIVE_ROOT=<native save folder> \
//	go test ./internal/client/target/steam/steamworks/ -run LiveDryRun -v
func TestLiveDryRun(t *testing.T) {
	library := os.Getenv("OMNISAVE_LIVE_LIB")
	appID := os.Getenv("OMNISAVE_LIVE_APPID")
	root := os.Getenv("OMNISAVE_LIVE_ROOT")
	if library == "" || appID == "" || root == "" {
		t.Skip("set OMNISAVE_LIVE_LIB, OMNISAVE_LIVE_APPID, OMNISAVE_LIVE_ROOT to run")
	}
	var placed []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		placed = append(placed, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(Request{Library: library, AppID: appID, Files: placed, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("skipped: %q anchor: %q", result.Skipped, result.Anchor)
	t.Logf("would write: %d unchanged: %d ineligible: %d extras: %d outside: %d",
		len(result.Written), len(result.Unchanged), len(result.Ineligible), len(result.Extras), result.Outside)
	for index, name := range result.Written {
		if index == 5 {
			t.Logf("  … and %d more", len(result.Written)-5)
			break
		}
		t.Logf("  would write %s", name)
	}
}
