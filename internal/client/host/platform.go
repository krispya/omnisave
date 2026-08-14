// Package host identifies the machine the Client runs on.
package host

import (
	"os"
	"runtime"
	"strings"
)

// PlatformSteamDeck is reported instead of "linux" on Steam Deck hardware.
const PlatformSteamDeck = "steamdeck"

// Platform is the platform string a Device reports to the server: runtime.GOOS,
// refined to "steamdeck" when the client runs on a Steam Deck.
func Platform() string {
	return platform(runtime.GOOS, os.ReadFile)
}

// platform takes the OS and filesystem as inputs so tests can supply hosts.
func platform(goos string, readFile func(string) ([]byte, error)) string {
	if goos == "linux" && isSteamDeck(readFile) {
		return PlatformSteamDeck
	}
	return goos
}

// isSteamDeck recognizes Steam Deck hardware by its DMI product name (Jupiter
// is the LCD model, Galileo the OLED), falling back to the SteamOS os-release
// variant for environments that expose no DMI data.
func isSteamDeck(readFile func(string) ([]byte, error)) bool {
	if product, err := readFile("/sys/class/dmi/id/product_name"); err == nil {
		switch strings.TrimSpace(string(product)) {
		case "Jupiter", "Galileo":
			return true
		}
	}
	release, err := readFile("/etc/os-release")
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(release)) {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && key == "VARIANT_ID" && strings.Trim(value, `"`) == "steamdeck" {
			return true
		}
	}
	return false
}
