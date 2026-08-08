package retroarch_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
)

type fakeProvider struct {
	processes []running.Process
	cmdlines  map[int32][]string
	openPaths map[int32][]string
}

func (p *fakeProvider) Processes(context.Context) ([]running.Process, error) {
	return p.processes, nil
}

func (p *fakeProvider) Cmdline(_ context.Context, pid int32) ([]string, error) {
	return p.cmdlines[pid], nil
}

func (p *fakeProvider) OpenPaths(_ context.Context, pid int32) ([]string, error) {
	return p.openPaths[pid], nil
}

func native(path string) string {
	return filepath.FromSlash(path)
}

func retroarchTarget() target.Target {
	return target.Target{
		ID:      "retroarch:" + native("/config/retroarch"),
		Adapter: "retroarch",
		Root:    native("/config/retroarch"),
	}
}

func snesGame(discovered target.Target, contentPath, saveDirectory string) target.InstalledGame {
	return target.InstalledGame{
		ID:       discovered.ID + ":snes:" + contentPath,
		TargetID: discovered.ID,
		Identity: target.GameIdentity{
			Platform:    "snes",
			ContentPath: contentPath,
		},
		InstallRoot: filepath.Dir(contentPath),
		Metadata:    map[string]string{"save_directory": saveDirectory},
	}
}

func detect(t *testing.T, provider running.Provider, discovered target.Target, games []target.InstalledGame) map[string]bool {
	t.Helper()
	adapter := retroarch.New()
	playing, err := running.NewDetector(provider).Playing(context.Background(),
		func(ctx context.Context, snapshot *running.Snapshot) (map[string]bool, error) {
			return adapter.RunningGames(ctx, snapshot, discovered, games)
		})
	if err != nil {
		t.Fatal(err)
	}
	return playing
}

func TestAFrontendFoundByNameClaimsTheGameInItsArguments(t *testing.T) {
	discovered := retroarchTarget()
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), native("/saves"))
	other := snesGame(discovered, native("/roms/earthbound.sfc"), native("/saves"))

	// The installer-discovered layout: the target root is a config
	// directory, and the frontend binary is a system path only its name
	// identifies.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 3, Name: "retroarch", Executable: native("/usr/bin/retroarch")},
		},
		cmdlines: map[int32][]string{
			3: {"retroarch", "-L", native("/cores/snes9x.so"), native("/roms/chrono-trigger.sfc")},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game, other})
	if !playing[game.ID] || playing[other.ID] {
		t.Fatalf("expected only the launched game playing, got %v", playing)
	}
}

func TestAFrontendUnderTheTargetRootClaimsTheGameWhoseSaveItHolds(t *testing.T) {
	discovered := target.Target{
		ID:      "retroarch:" + native("/apps/retroarch"),
		Adapter: "retroarch",
		Root:    native("/apps/retroarch"),
	}
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), native("/saves"))

	// Launched from the frontend's own menu: no content in the arguments,
	// but the battery save it flushes to sits open.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 3, Name: "retroarch", Executable: native("/apps/retroarch/retroarch")},
		},
		openPaths: map[int32][]string{
			3: {native("/saves/chrono-trigger.srm")},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if !playing[game.ID] {
		t.Fatalf("expected the held save to claim its game, got %v", playing)
	}
}

func TestAMenuLaunchedGameInASymlinkedSaveDirectoryIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevation on Windows")
	}
	base := t.TempDir()
	realSaves := filepath.Join(base, "data", "saves")
	if err := os.MkdirAll(realSaves, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedSaves := filepath.Join(base, "saves")
	if err := os.Symlink(realSaves, linkedSaves); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	canonicalSaves, err := filepath.EvalSymlinks(realSaves)
	if err != nil {
		t.Fatal(err)
	}

	discovered := retroarchTarget()
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), linkedSaves)

	// Launched from the frontend's own menu: the config names the save
	// directory through its symlink, while the kernel reports the held
	// save fully resolved.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 3, Name: "retroarch", Executable: native("/usr/bin/retroarch")},
		},
		openPaths: map[int32][]string{
			3: {filepath.Join(canonicalSaves, "chrono-trigger.srm")},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if !playing[game.ID] {
		t.Fatalf("expected the save held through the symlinked directory to claim its game, got %v", playing)
	}
}

func TestAFrontendUnderASymlinkedTargetRootIsRecognized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevation on Windows")
	}
	base := t.TempDir()
	realRoot := filepath.Join(base, "apps", "frontend")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(base, "retroarch")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}

	discovered := target.Target{
		ID:      "retroarch:" + linkedRoot,
		Adapter: "retroarch",
		Root:    linkedRoot,
	}
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), native("/saves"))

	// The binary's name does not say retroarch, so only executing under
	// the target root — known through a link — identifies the frontend.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 3, Name: "ra64", Executable: filepath.Join(canonicalRoot, "ra64")},
		},
		cmdlines: map[int32][]string{
			3: {"ra64", "-L", native("/cores/snes9x.so"), native("/roms/chrono-trigger.sfc")},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if !playing[game.ID] {
		t.Fatalf("expected the frontend under the linked root to claim its game, got %v", playing)
	}
}

func TestAFrontendWithNoEvidenceClaimsNothing(t *testing.T) {
	discovered := retroarchTarget()
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), native("/saves"))

	provider := &fakeProvider{processes: []running.Process{
		{PID: 3, Name: "retroarch", Executable: native("/usr/bin/retroarch")},
	}}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if len(playing) != 0 {
		t.Fatalf("expected a frontend in its menu to claim nothing, got %v", playing)
	}
}

func TestEvidenceFromNonFrontendProcessesIsIgnored(t *testing.T) {
	discovered := retroarchTarget()
	game := snesGame(discovered, native("/roms/chrono-trigger.sfc"), native("/saves"))

	// An editor holding the ROM open is not the frontend playing it.
	provider := &fakeProvider{
		processes: []running.Process{
			{PID: 8, Name: "hexeditor", Executable: native("/usr/bin/hexeditor")},
		},
		openPaths: map[int32][]string{
			8: {native("/roms/chrono-trigger.sfc")},
		},
	}

	playing := detect(t, provider, discovered, []target.InstalledGame{game})
	if len(playing) != 0 {
		t.Fatalf("expected non-frontend evidence ignored, got %v", playing)
	}
}

func TestAForeignTargetIsRejected(t *testing.T) {
	adapter := retroarch.New()
	_, err := adapter.RunningGames(context.Background(), nil, target.Target{Adapter: "steam"}, nil)
	if err == nil {
		t.Fatal("expected a foreign target to be rejected")
	}
}
