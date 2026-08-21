package steam

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/steamcloud"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/steamworks"
)

// FinishPlacement settles Steam's cloud file registry after files reach a
// game's own save folder. A game that moves its saves through the store's
// API trusts that registry — not the folder — for whether live state
// exists, so a restored file the registry no longer lists would be deleted
// at the next launch (FDR-005). Folder-replicated games are Steam's own job
// to notice and are left alone.
//
// The work runs in a helper process because a Steamworks library speaks as
// one game per process, and while connected the account shows as playing
// the game — the helper holds the connection only as long as the writes
// take.
func (a *Adapter) FinishPlacement(ctx context.Context, discovered target.Target, game target.InstalledGame, save target.Save) (target.PlacementReport, error) {
	if err := validateGame(discovered, game); err != nil {
		return target.PlacementReport{}, err
	}
	appID, _ := game.Identity.Identifier("steam.app")
	if !steamcloud.StoresThroughAPI(discovered.Root, appID) {
		return target.PlacementReport{}, nil
	}
	library, err := steamworks.FindLibrary(game.InstallRoot)
	if err != nil {
		return target.PlacementReport{}, fmt.Errorf("locate the game's steamworks library: %w", err)
	}
	files := make([]string, 0, len(save.Files))
	for _, file := range save.Files {
		files = append(files, file.Path)
	}
	run := a.runHelper
	if run == nil {
		run = execHelper
	}
	result, err := run(ctx, steamworks.Request{Library: library, AppID: appID, Files: files})
	if err != nil {
		return target.PlacementReport{}, err
	}
	// Unchanged entries are deliberately not reported: a registry that
	// already agreed with the placement has nothing worth a sentence.
	report := target.PlacementReport{
		Registered: result.Written,
		Extras:     result.Extras,
		Skipped:    result.Skipped,
	}
	if len(result.Failed) > 0 {
		report.Failed = make(map[string]string, len(result.Failed))
		for _, failure := range result.Failed {
			report.Failed[failure.Name] = failure.Cause
		}
	}
	return report, nil
}

// execHelper re-executes this client as its reconciliation helper. The
// Steamworks library prints its own diagnostics, so the result is the first
// stdout line that parses as one rather than the stream taken whole.
func execHelper(ctx context.Context, request steamworks.Request) (steamworks.Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return steamworks.Result{}, fmt.Errorf("locate the omnisave executable: %w", err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return steamworks.Result{}, err
	}
	command := exec.CommandContext(ctx, executable, steamworks.HelperCommand)
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return steamworks.Result{}, fmt.Errorf("steam cloud helper: %w%s", err, stderrTail(stderr.String()))
	}
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var result steamworks.Result
		if err := json.Unmarshal(scanner.Bytes(), &result); err == nil {
			return result, nil
		}
	}
	return steamworks.Result{}, fmt.Errorf("steam cloud helper returned no result%s", stderrTail(stderr.String()))
}

// stderrTail keeps the last meaningful line of helper noise for an error.
func stderrTail(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line != "" && !strings.HasPrefix(line, "[S_API]") {
			return ": " + line
		}
	}
	return ""
}

var _ target.PlacementFinisher = (*Adapter)(nil)
