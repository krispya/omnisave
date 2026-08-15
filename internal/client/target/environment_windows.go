//go:build windows

package target

import "golang.org/x/sys/windows"

// knownFolders records the shell's Documents and Saved Games locations,
// which OneDrive Known Folder Move relocates away from the home directory.
func knownFolders(variables map[string]string) {
	if path, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && path != "" {
		variables["DOCUMENTS"] = path
	}
	if path, err := windows.KnownFolderPath(windows.FOLDERID_SavedGames, 0); err == nil && path != "" {
		variables["SAVED_GAMES"] = path
	}
}
