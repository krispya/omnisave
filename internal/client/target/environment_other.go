//go:build !windows

package target

// knownFolders is Windows-only; other platforms derive these from home.
func knownFolders(map[string]string) {}
