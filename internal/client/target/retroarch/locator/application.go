package locator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

type application struct {
	paths []string
}

// NewApplication finds RetroArch itself, even before it creates a configuration.
func NewApplication(paths ...string) Locator {
	return application{paths: paths}
}

func (l application) Locate(ctx context.Context) ([]Candidate, error) {
	paths := l.paths
	useDefaultConfiguration := len(paths) == 0
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		paths = defaultApplicationPathsFor(runtime.GOOS, home)
	}

	configPath := ""
	if useDefaultConfiguration {
		configPaths, err := defaultInstallerConfigPaths()
		if err != nil {
			return nil, err
		}
		configPath, err = firstExistingFile(ctx, configPaths)
		if err != nil {
			return nil, err
		}
	}

	var candidates []Candidate
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		binaryPath := filepath.Join(path, "Contents", "MacOS", "RetroArch")
		info, err := os.Stat(binaryPath)
		if err == nil && info.Mode().IsRegular() {
			candidates = append(candidates, Candidate{
				Source:     "installer",
				Root:       path,
				ConfigPath: configPath,
			})
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return candidates, nil
}

func defaultApplicationPathsFor(hostOS, home string) []string {
	if hostOS != "darwin" {
		return nil
	}
	return []string{
		"/Applications/RetroArch.app",
		filepath.Join(home, "Applications", "RetroArch.app"),
	}
}

func firstExistingFile(ctx context.Context, paths []string) (string, error) {
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}
