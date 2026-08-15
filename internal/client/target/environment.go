package target

import (
	"os"
	"runtime"
)

const (
	RuntimeNative = "native"
	RuntimeProton = "proton"
)

// CurrentEnvironment describes the native process environment for one store root.
func CurrentEnvironment(storeRoot string) Environment {
	home, _ := os.UserHomeDir()
	variables := make(map[string]string)
	for _, name := range []string{"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PUBLIC", "WINDIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		if value := os.Getenv(name); value != "" {
			variables[name] = value
		}
	}
	// DOCUMENTS and SAVED_GAMES have no environment variables; Windows
	// resolves them through the shell so OneDrive folder moves are honored.
	knownFolders(variables)
	return Environment{
		HostOS:    runtime.GOOS,
		Runtime:   RuntimeNative,
		Home:      home,
		StoreRoot: storeRoot,
		Variables: variables,
	}
}
