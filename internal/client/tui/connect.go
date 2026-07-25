package tui

import (
	"errors"
	"fmt"
	"net"
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
	for _, line := range connectSuccessLines(url, deviceName) {
		fmt.Println(line)
	}
}

func connectSuccessLines(url, deviceName string) []string {
	return []string{
		accentStyle.Render("▲") + " " + titleStyle.Render("Omnisave"),
		mutedStyle.Render("- Server:") + "  " + plainTitle(url),
		mutedStyle.Render("- Device:") + "  " + plainTitle(deviceName),
		"",
		successStyle.Render("✓") + " Connected",
		"",
	}
}

// ConnectFailed reports a connection attempt that changed nothing, in the
// shape its success prints: the glyph, the state, then the cause.
func ConnectFailed(err error) {
	fmt.Println(errorStyle.Render("✗") + " Could not connect  " + mutedStyle.Render(Cause(err)))
}

// ServerUnreachable reports a command that stopped before doing any work
// because its server never answered.
func ServerUnreachable(serverURL string, err error) {
	fmt.Println(serverUnreachableLine(serverURL, err))
}

func serverUnreachableLine(serverURL string, err error) string {
	return FailureLine("cannot reach the Omnisave server at " + serverURL + " — " + Cause(err))
}

// ServerRejectedToken reports a saved connection the server no longer
// accepts — the one connection failure waiting will not fix.
func ServerRejectedToken(serverURL string) {
	fmt.Println(serverRejectedTokenLine(serverURL))
}

func serverRejectedTokenLine(serverURL string) string {
	return FailureLine("the Omnisave server at " + serverURL +
		" rejected this token; run omnisave-client connect")
}

// Cause reduces a failed request to the part worth reading: the line already
// names the server, so "connection refused" is the rest.
func Cause(err error) string {
	if err == nil {
		return "no response"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "the request timed out"
	}
	deepest := err
	for {
		unwrapped := errors.Unwrap(deepest)
		if unwrapped == nil {
			break
		}
		deepest = unwrapped
	}
	if cause := strings.TrimSpace(deepest.Error()); cause != "" {
		return cause
	}
	return strings.TrimSpace(err.Error())
}
