package locator

import (
	"path/filepath"
	"testing"
)

func TestMacInstallerUsesTheSteamApplicationSupportRoot(t *testing.T) {
	home := t.TempDir()
	roots := defaultRootsFor("darwin", home)
	expected := filepath.Join(home, "Library", "Application Support", "Steam")
	if len(roots) != 1 || roots[0] != expected {
		t.Fatalf("expected the macOS Steam root, got %+v", roots)
	}
}
