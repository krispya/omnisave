package locator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type installer struct {
	configPaths []string
}

// NewInstaller finds conventional standalone RetroArch configurations.
func NewInstaller(configPaths ...string) Locator {
	return installer{configPaths: configPaths}
}

func (l installer) Locate(ctx context.Context) ([]Candidate, error) {
	configPaths := l.configPaths
	if len(configPaths) == 0 {
		var err error
		configPaths, err = defaultInstallerConfigPaths()
		if err != nil {
			return nil, err
		}
	}
	return existingCandidates(ctx, "installer", configPaths)
}

func defaultInstallerConfigPaths() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return defaultInstallerConfigPathsFor(runtime.GOOS, home), nil
}

func defaultInstallerConfigPathsFor(hostOS, home string) []string {
	var configPaths []string
	switch hostOS {
	case "windows":
		if executable, err := exec.LookPath("retroarch.exe"); err == nil {
			configPaths = append(configPaths, filepath.Join(filepath.Dir(executable), "retroarch.cfg"))
		}
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root == "" {
				continue
			}
			configPaths = append(configPaths,
				filepath.Join(root, "RetroArch", "retroarch.cfg"),
				filepath.Join(root, "RetroArch-Win64", "retroarch.cfg"),
			)
		}
		if systemDrive := os.Getenv("SystemDrive"); systemDrive != "" {
			root := systemDrive + string(os.PathSeparator)
			configPaths = append(configPaths,
				filepath.Join(root, "RetroArch", "retroarch.cfg"),
				filepath.Join(root, "RetroArch-Win64", "retroarch.cfg"),
			)
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			configPaths = append(configPaths, filepath.Join(appData, "retroarch.cfg"))
		}
	case "darwin":
		configPaths = unixConfigPaths(home)
		configPaths = append(configPaths,
			filepath.Join(home, "Library", "Application Support", "RetroArch", "retroarch.cfg"),
		)
	default:
		configPaths = unixConfigPaths(home)
	}
	return configPaths
}

func unixConfigPaths(home string) []string {
	var paths []string
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		paths = append(paths, filepath.Join(configHome, "retroarch", "retroarch.cfg"))
	}
	return append(paths,
		filepath.Join(home, ".config", "retroarch", "retroarch.cfg"),
		filepath.Join(home, ".retroarch.cfg"),
		"/etc/retroarch.cfg",
	)
}
