package running_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
)

func TestThePlatformDetectorSeesThisVeryProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Skipf("cannot resolve this test binary: %v", err)
	}

	roots := running.ResolveRoots([]string{filepath.Dir(executable)})
	playing, err := running.PlatformDetector().Playing(context.Background(),
		func(_ context.Context, snapshot *running.Snapshot) (map[string]bool, error) {
			for _, process := range snapshot.Processes() {
				for _, root := range roots {
					if running.UnderRoot(process.Executable, root) {
						return map[string]bool{"this-test": true}, nil
					}
				}
			}
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !playing["this-test"] {
		t.Fatal("expected the sweep to find the test binary's own process")
	}
}

func TestThePlatformProviderAnswersThisProcessesCmdline(t *testing.T) {
	pid := int32(os.Getpid())

	_, err := running.PlatformDetector().Playing(context.Background(),
		func(ctx context.Context, snapshot *running.Snapshot) (map[string]bool, error) {
			if len(snapshot.Cmdline(ctx, pid)) == 0 {
				t.Fatal("expected this process's own cmdline to be readable")
			}
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
}
