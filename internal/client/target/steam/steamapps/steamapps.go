// Package steamapps reads the games a Steam library has installed. The layout
// is Steam's own, so every client that writes it — Valve's, and third-party
// clients such as GameHub — is read the same way.
package steamapps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var manifestRow = regexp.MustCompile(`(?m)^\s*"([^"]+)"\s+"([^"]*)"`)

// App is one game installed in a Steam library.
type App struct {
	// ID is the Steam app ID.
	ID    string
	Title string
	// InstallRoot is the game's own directory under steamapps/common.
	InstallRoot string
}

// Installed reports the games a library has installed: every app manifest
// whose install directory exists, in manifest file order. A library with no
// steamapps directory has nothing installed.
func Installed(library string) ([]App, error) {
	steamApps := filepath.Join(library, "steamapps")
	entries, err := os.ReadDir(steamApps)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "appmanifest_") || !strings.HasSuffix(name, ".acf") {
			continue
		}
		app, installDirectory, err := readAppManifest(filepath.Join(steamApps, name))
		if err != nil {
			return nil, err
		}
		if app.ID == "" || installDirectory == "" {
			continue
		}
		app.InstallRoot = filepath.Join(steamApps, "common", installDirectory)
		info, err := os.Stat(app.InstallRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

// readAppManifest reads one manifest's app and the name of its directory
// under steamapps/common. A manifest without a numeric app ID describes no
// game and reads as empty.
func readAppManifest(path string) (App, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return App{}, "", fmt.Errorf("read Steam app manifest %s: %w", path, err)
	}
	values := make(map[string]string)
	for _, match := range manifestRow.FindAllStringSubmatch(string(data), -1) {
		values[strings.ToLower(match[1])] = strings.ReplaceAll(match[2], `\\`, `\`)
	}
	if _, err := strconv.ParseUint(values["appid"], 10, 64); err != nil {
		return App{}, "", nil
	}
	title := values["name"]
	if title == "" {
		title = values["appid"]
	}
	return App{ID: values["appid"], Title: title}, values["installdir"], nil
}
