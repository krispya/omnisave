package tui

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/discovery"
)

// PromptServerURL asks where the server is. It is the fallback for a network
// that carried no announcement — a bridged container, a segmented network,
// anywhere across the internet — and the flow after it is the same one
// discovery leads to.
func PromptServerURL(defaultURL string) (string, error) {
	url := defaultURL
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server URL").
				Value(&url),
		).Title("Connect to your Omnisave server"),
	).WithTheme(trackingTheme())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return strings.TrimSpace(url), nil
}

// ChooseServer asks which announcing server to connect to. Every one of them
// is a claim rather than a credential — anything on the network can say it is
// an Omnisave server — so the list shows the URL each name resolved to.
func ChooseServer(servers []discovery.Server) (discovery.Server, error) {
	options := make([]huh.Option[int], 0, len(servers))
	for index, server := range servers {
		options = append(options, huh.NewOption(server.Name+"  "+server.URL, index))
	}
	chosen := 0
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Server").
				Options(options...).
				Value(&chosen),
		).Title("Found more than one Omnisave server"),
	).WithTheme(trackingTheme())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return discovery.Server{}, ErrAborted
		}
		return discovery.Server{}, err
	}
	return servers[chosen], nil
}

// ServerFound names the one server that answered, so a command with no
// arguments still says what it connected to.
func ServerFound(server discovery.Server) {
	fmt.Println(mutedStyle.Render("Found") + " " + plainTitle(server.Name) + " " +
		mutedStyle.Render("at "+server.URL))
}

// NoServersFound reports a local network that announced nothing. It is the
// ordinary answer in a bridged container, so it asks for an address rather
// than treating it as a failure.
func NoServersFound() {
	fmt.Println(mutedStyle.Render("No Omnisave server announced itself on this network."))
}

// ConnectDenied reports a request the owner refused.
func ConnectDenied() {
	fmt.Println(errorStyle.Render("✗") + " Not connected  " +
		mutedStyle.Render("the request was denied in the Dash"))
}

// ConnectExpired reports a request nobody answered in time. Nothing is left
// behind on the server, so the only thing to say is to try again.
func ConnectExpired() {
	fmt.Println(errorStyle.Render("✗") + " Not connected  " +
		mutedStyle.Render("the code expired; run omnisave connect again"))
}

// ConnectSuccess confirms the persisted connection and the device it created.
func ConnectSuccess(url, deviceName string) {
	for _, line := range connectSuccessLines(url, deviceName) {
		fmt.Println(line)
	}
}

func connectSuccessLines(url, deviceName string) []string {
	return []string{
		titleStyle.Render("▲ Omnisave"),
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
		" rejected this token; run omnisave connect")
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
