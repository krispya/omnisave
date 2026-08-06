package running_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
)

type fixedLister struct {
	processes []running.Process
	err       error
}

func (l fixedLister) Processes(context.Context) ([]running.Process, error) {
	return l.processes, l.err
}

func native(path string) string {
	return filepath.FromSlash(path)
}

func TestAProcessInsideAGamesRootMeansTheGameIsPlaying(t *testing.T) {
	detector := running.NewDetector(fixedLister{processes: []running.Process{
		{PID: 7, Executable: native("/games/elden-ring/game.exe")},
		{PID: 9, Executable: native("/usr/bin/zsh")},
	}})

	playing, err := detector.Playing(context.Background(), []running.Game{
		{ID: "elden-ring", Roots: []string{native("/games/elden-ring")}},
		{ID: "hades", Roots: []string{native("/games/hades")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !playing["elden-ring"] || playing["hades"] {
		t.Fatalf("expected only elden-ring playing, got %v", playing)
	}
}

func TestATargetRootClaimsAnEmulatedGamesFrontendProcess(t *testing.T) {
	detector := running.NewDetector(fixedLister{processes: []running.Process{
		{PID: 3, Executable: native("/apps/retroarch/retroarch")},
	}})

	playing, err := detector.Playing(context.Background(), []running.Game{
		{ID: "chrono-trigger", Roots: []string{
			native("/roms/chrono-trigger"),
			native("/apps/retroarch"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !playing["chrono-trigger"] {
		t.Fatalf("expected the frontend process to count, got %v", playing)
	}
}

func TestARootNeverClaimsItsSimilarlyNamedSibling(t *testing.T) {
	detector := running.NewDetector(fixedLister{processes: []running.Process{
		{PID: 4, Executable: native("/games/hades-two/game")},
	}})

	playing, err := detector.Playing(context.Background(), []running.Game{
		{ID: "hades", Roots: []string{native("/games/hades")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if playing["hades"] {
		t.Fatalf("expected the sibling directory to stay unclaimed, got %v", playing)
	}
}

func TestEmptyRootsAndEmptyExecutablesNeverMatch(t *testing.T) {
	detector := running.NewDetector(fixedLister{processes: []running.Process{
		{PID: 4, Executable: ""},
		{PID: 5, Executable: native("/games/hades/game")},
	}})

	playing, err := detector.Playing(context.Background(), []running.Game{
		{ID: "unrooted", Roots: []string{""}},
		{ID: "rootless"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(playing) != 0 {
		t.Fatalf("expected nothing to match, got %v", playing)
	}
}

func TestCaseFoldingFollowsThePlatform(t *testing.T) {
	detector := running.NewDetector(fixedLister{processes: []running.Process{
		{PID: 4, Executable: native("/Games/Hades/Game")},
	}})

	playing, err := detector.Playing(context.Background(), []running.Game{
		{ID: "hades", Roots: []string{native("/games/hades")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	folded := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	if playing["hades"] != folded {
		t.Fatalf("expected case folding %v on %s, got %v", folded, runtime.GOOS, playing)
	}
}

func TestAFailedSweepSurfacesItsError(t *testing.T) {
	failure := errors.New("process list unavailable")
	detector := running.NewDetector(fixedLister{err: failure})

	if _, err := detector.Playing(context.Background(), nil); !errors.Is(err, failure) {
		t.Fatalf("expected the lister's error, got %v", err)
	}
}
