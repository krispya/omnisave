package locator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMacInstallerFindsRetroArchConfigurations(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	paths := defaultInstallerConfigPathsFor("darwin", home)

	for _, path := range []string{
		filepath.Join(configHome, "retroarch", "retroarch.cfg"),
		filepath.Join(home, "Library", "Application Support", "RetroArch", "retroarch.cfg"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := (installer{configPaths: paths}).Locate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected XDG and Application Support configurations, got %+v", candidates)
	}
}

func TestMacApplicationPathsIncludeSystemAndUserApplications(t *testing.T) {
	home := t.TempDir()
	paths := defaultApplicationPathsFor("darwin", home)
	expectedUserPath := filepath.Join(home, "Applications", "RetroArch.app")
	if len(paths) != 2 || paths[0] != "/Applications/RetroArch.app" || paths[1] != expectedUserPath {
		t.Fatalf("expected conventional macOS application paths, got %+v", paths)
	}
}

func TestMacSteamLocatorUsesTheSteamApplicationSupportRoot(t *testing.T) {
	home := t.TempDir()
	roots := defaultSteamRootsFor("darwin", home)
	expected := filepath.Join(home, "Library", "Application Support", "Steam")
	if len(roots) != 1 || roots[0] != expected {
		t.Fatalf("expected the macOS Steam root, got %+v", roots)
	}
}
