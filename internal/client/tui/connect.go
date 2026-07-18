package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// PromptConnect asks for the server URL and API token. The token input is
// masked so it never echoes into the terminal or scrollback.
func PromptConnect(defaultURL string) (url, token string, err error) {
	url = defaultURL
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server URL").
				Value(&url),
			huh.NewInput().
				Title("API token").
				EchoMode(huh.EchoModePassword).
				Value(&token),
		).Title("Connect to your Omnisave server"),
	).WithTheme(trackingTheme())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", "", ErrAborted
		}
		return "", "", err
	}
	return strings.TrimSpace(url), strings.TrimSpace(token), nil
}

// ConnectSuccess confirms the persisted connection and the device it created.
func ConnectSuccess(url, deviceName string) {
	fmt.Println(successStyle.Render("✓") + " connected to " + plainTitle(url))
	fmt.Println("  " + mutedStyle.Render("this device: "+deviceName))
}

// ConnectFailed reports a connection attempt that changed nothing.
func ConnectFailed(err error) {
	fmt.Println(errorStyle.Render("✗") + " could not connect  " +
		mutedStyle.Render(strings.TrimSpace(err.Error())))
}
