package running

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type fakeProvider struct {
	processes    []Process
	cmdlines     map[int32][]string
	openPaths    map[int32][]string
	err          error
	cmdlineReads map[int32]int
}

func (p *fakeProvider) Processes(context.Context) ([]Process, error) {
	return p.processes, p.err
}

func (p *fakeProvider) Cmdline(_ context.Context, pid int32) ([]string, error) {
	if p.cmdlineReads == nil {
		p.cmdlineReads = make(map[int32]int)
	}
	p.cmdlineReads[pid]++
	return p.cmdlines[pid], nil
}

func (p *fakeProvider) OpenPaths(_ context.Context, pid int32) ([]string, error) {
	return p.openPaths[pid], nil
}

func native(path string) string {
	return filepath.FromSlash(path)
}

func claim(ids ...string) Matcher {
	return func(context.Context, *Snapshot) (map[string]bool, error) {
		claimed := make(map[string]bool)
		for _, id := range ids {
			claimed[id] = true
		}
		return claimed, nil
	}
}

func TestEveryMatcherAnswersFromOneSharedSweep(t *testing.T) {
	provider := &fakeProvider{
		processes: []Process{{PID: 7, Name: "game", Executable: native("/games/elden-ring/game")}},
		cmdlines:  map[int32][]string{7: {"game", "--windowed"}},
	}
	detector := NewDetector(provider)

	asks := 0
	ask := func(ctx context.Context, snapshot *Snapshot) (map[string]bool, error) {
		asks++
		if len(snapshot.Cmdline(ctx, 7)) == 0 {
			t.Fatal("expected the snapshot to answer the cmdline")
		}
		return map[string]bool{}, nil
	}

	playing, err := detector.Playing(context.Background(), ask, ask, claim("elden-ring"))
	if err != nil {
		t.Fatal(err)
	}
	if asks != 2 || !playing["elden-ring"] {
		t.Fatalf("expected both matchers consulted and claims merged, got asks=%d playing=%v", asks, playing)
	}
	if provider.cmdlineReads[7] != 1 {
		t.Fatalf("expected one cmdline read shared across matchers, got %d", provider.cmdlineReads[7])
	}
}

func TestMatcherClaimsMerge(t *testing.T) {
	detector := NewDetector(&fakeProvider{})

	playing, err := detector.Playing(context.Background(), claim("elden-ring"), claim("hades"))
	if err != nil {
		t.Fatal(err)
	}
	if !playing["elden-ring"] || !playing["hades"] {
		t.Fatalf("expected both games playing, got %v", playing)
	}
}

func TestAStoppedGameLingersOnlyThroughTheStopGrace(t *testing.T) {
	now := time.Unix(1000, 0)
	detector := NewDetector(&fakeProvider{})
	detector.grace = 6 * time.Second
	detector.now = func() time.Time { return now }

	if playing, err := detector.Playing(context.Background(), claim("hades")); err != nil || !playing["hades"] {
		t.Fatalf("expected hades playing, got %v, %v", playing, err)
	}

	now = now.Add(3 * time.Second)
	if playing, err := detector.Playing(context.Background()); err != nil || !playing["hades"] {
		t.Fatalf("expected hades held through the grace, got %v, %v", playing, err)
	}

	now = now.Add(10 * time.Second)
	if playing, err := detector.Playing(context.Background()); err != nil || playing["hades"] {
		t.Fatalf("expected hades released after the grace, got %v, %v", playing, err)
	}
}

func TestAZeroGraceReleasesImmediately(t *testing.T) {
	detector := NewDetector(&fakeProvider{})
	detector.grace = 0

	if playing, err := detector.Playing(context.Background(), claim("hades")); err != nil || !playing["hades"] {
		t.Fatalf("expected hades playing, got %v, %v", playing, err)
	}
	if playing, err := detector.Playing(context.Background()); err != nil || playing["hades"] {
		t.Fatalf("expected hades released, got %v, %v", playing, err)
	}
}

func TestAFailedSweepSurfacesItsError(t *testing.T) {
	failure := errors.New("process list unavailable")
	detector := NewDetector(&fakeProvider{err: failure})

	if _, err := detector.Playing(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("expected the provider's error, got %v", err)
	}
}

func TestAFailedMatcherSurfacesItsError(t *testing.T) {
	failure := errors.New("adapter broke")
	detector := NewDetector(&fakeProvider{})

	_, err := detector.Playing(context.Background(), func(context.Context, *Snapshot) (map[string]bool, error) {
		return nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("expected the matcher's error, got %v", err)
	}
}

func TestUnderRootCrossesWholeSegmentsOnly(t *testing.T) {
	if !UnderRoot(native("/games/hades/game"), native("/games/hades")) {
		t.Fatal("expected a path inside the root to match")
	}
	if UnderRoot(native("/games/hades-two/game"), native("/games/hades")) {
		t.Fatal("expected the similarly-named sibling to stay unclaimed")
	}
	if !UnderRoot(native("/games/hades"), native("/games/hades")) {
		t.Fatal("expected the root itself to match")
	}
}

func TestUnderRootClaimsBeneathAFilesystemRoot(t *testing.T) {
	root := native("/")
	if !UnderRoot(native("/games/hades/game"), root) {
		t.Fatal("expected a filesystem root to claim everything beneath it")
	}
}

func TestUnderRootIgnoresEmptyPaths(t *testing.T) {
	if UnderRoot("", native("/games")) || UnderRoot(native("/games/game"), "") {
		t.Fatal("expected empty paths to never match")
	}
}

func TestPathComparisonsFoldCaseWithThePlatform(t *testing.T) {
	folded := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	if UnderRoot(native("/Games/Hades/Game"), native("/games/hades")) != folded {
		t.Fatalf("expected UnderRoot case folding %v on %s", folded, runtime.GOOS)
	}
	if SamePath(native("/Roms/Game.sfc"), native("/roms/game.sfc")) != folded {
		t.Fatalf("expected SamePath case folding %v on %s", folded, runtime.GOOS)
	}
}

func TestResolveRootsSkipsEmptiesAndKeepsOriginals(t *testing.T) {
	roots := ResolveRoots([]string{"", native("/games/hades")})
	if len(roots) == 0 || roots[0] != native("/games/hades") {
		t.Fatalf("expected the original root kept and empties dropped, got %v", roots)
	}
}
