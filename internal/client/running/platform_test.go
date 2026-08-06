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

	playing, err := running.PlatformDetector().Playing(context.Background(), []running.Game{
		{ID: "this-test", Roots: []string{filepath.Dir(executable)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !playing["this-test"] {
		t.Fatal("expected the sweep to find the test binary's own process")
	}
}
