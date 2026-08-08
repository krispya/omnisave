package steam_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
)

type fakeProvider struct {
	processes []running.Process
	cmdlines  map[int32][]string
}

func (p *fakeProvider) Processes(context.Context) ([]running.Process, error) {
	return p.processes, nil
}

func (p *fakeProvider) Cmdline(_ context.Context, pid int32) ([]string, error) {
	return p.cmdlines[pid], nil
}

func (p *fakeProvider) OpenPaths(context.Context, int32) ([]string, error) {
	return nil, nil
}

func native(path string) string {
	return filepath.FromSlash(path)
}

func steamTarget() target.Target {
	return target.Target{
		ID:      "steam:" + native("/steam"),
		Adapter: "steam",
		Root:    native("/steam"),
	}
}

func steamGame(discovered target.Target, appID, installRoot string) target.InstalledGame {
	return target.InstalledGame{
		ID:       discovered.ID + ":" + appID,
		TargetID: discovered.ID,
		Identity: target.GameIdentity{
			Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: appID}},
		},
		InstallRoot: installRoot,
	}
}

func detect(t *testing.T, provider running.Provider, discovered target.Target, games []target.InstalledGame) map[string]bool {
	t.Helper()
	adapter := steam.New()
	playing, err := running.NewDetector(provider).Playing(context.Background(),
		func(ctx context.Context, snapshot *running.Snapshot) (map[string]bool, error) {
			return adapter.RunningGames(ctx, snapshot, discovered, games)
		})
	if err != nil {
		t.Fatal(err)
	}
	return playing
}

func TestTheLaunchWrapperNamesTheAppBeingPlayed(t *testing.T) {
	discovered := steamTarget()
	game := steamGame(discovered, "1091500", native("/steam/steamapps/common/Cyberpunk 2077"))
	other := steamGame(discovered, "1245620", native("/steam/steamapps/common/ELDEN RING"))

	// A Proton title: the live process is a Wine loader nowhere near the
	// install root, and only the wrapper's arguments say what is running.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 4, Name: "reaper", Executable: native("/steam/ubuntu12_32/reaper")},
			{PID: 5, Name: "Cyberpunk2077.exe", Executable: native("/usr/lib/wine/wine64-preloader")},
		},
		cmdlines: map[int32][]string{
			4: {"reaper", "SteamLaunch", "AppId=1091500", "--", "proton", "waitforexitandrun"},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game, other})
	if !playing[game.ID] || playing[other.ID] {
		t.Fatalf("expected only the wrapped app playing, got %v", playing)
	}
}

func TestAProcessUnderTheInstallRootCorroboratesWithoutAWrapper(t *testing.T) {
	discovered := steamTarget()
	game := steamGame(discovered, "1145360", native("/steam/steamapps/common/Hades"))

	provider := &fakeProvider{processes: []running.Process{
		{PID: 9, Name: "Hades", Executable: native("/steam/steamapps/common/Hades/Hades")},
	}}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if !playing[game.ID] {
		t.Fatalf("expected the natively-launched game playing, got %v", playing)
	}
}

func TestAGameKnownThroughASymlinkedLibraryIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevation on Windows")
	}
	base := t.TempDir()
	realLibrary := filepath.Join(base, "drive", "SteamLibrary")
	if err := os.MkdirAll(filepath.Join(realLibrary, "steamapps", "common", "Hades"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedLibrary := filepath.Join(base, "library")
	if err := os.Symlink(realLibrary, linkedLibrary); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(filepath.Join(realLibrary, "steamapps", "common", "Hades"))
	if err != nil {
		t.Fatal(err)
	}

	discovered := steamTarget()
	game := steamGame(discovered, "1145360", filepath.Join(linkedLibrary, "steamapps", "common", "Hades"))

	// Process sweeps report fully resolved executables; the adapter knows
	// the install only through the linked library.
	provider := &fakeProvider{processes: []running.Process{
		{PID: 9, Name: "Hades", Executable: filepath.Join(canonicalRoot, "Hades")},
	}}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if !playing[game.ID] {
		t.Fatalf("expected the game under the linked library playing, got %v", playing)
	}
}

func TestAnotherTargetsGamesAreNotClaimed(t *testing.T) {
	discovered := steamTarget()
	foreign := steamGame(discovered, "1145360", native("/steam/steamapps/common/Hades"))
	foreign.TargetID = "steam:" + native("/elsewhere")

	provider := &fakeProvider{processes: []running.Process{
		{PID: 9, Name: "Hades", Executable: native("/steam/steamapps/common/Hades/Hades")},
	}}

	playing := detect(t, provider, discovered, []target.InstalledGame{foreign})
	if len(playing) != 0 {
		t.Fatalf("expected another target's game left alone, got %v", playing)
	}
}

func TestAForeignTargetIsRejected(t *testing.T) {
	adapter := steam.New()
	_, err := adapter.RunningGames(context.Background(), nil, target.Target{Adapter: "retroarch"}, nil)
	if err == nil {
		t.Fatal("expected a foreign target to be rejected")
	}
}
