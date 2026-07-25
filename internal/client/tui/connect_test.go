package tui

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestConnectSuccessSeparatesConnectionMetadataFromFollowingEvents(t *testing.T) {
	lines := connectSuccessLines("http://localhost:8080", "Kriss-MacBook-Pro.local")
	expected := []string{
		"▲ Omnisave",
		"- Server:  http://localhost:8080",
		"- Device:  Kriss-MacBook-Pro.local",
		"",
		"✓ Connected",
		"",
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d connection lines, got %d", len(expected), len(lines))
	}
	for index, line := range lines {
		if stripped := ansi.Strip(line); stripped != expected[index] {
			t.Fatalf("expected connection line %d to be %q, got %q", index, expected[index], stripped)
		}
	}
}

func TestServerUnreachableNamesTheServerAndTheCauseWithoutTheHTTPPlumbing(t *testing.T) {
	refused := fmt.Errorf("contact Omnisave server: %w", &url.Error{
		Op:  "Get",
		URL: "http://localhost:8080/api/v1/omnisaves",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
		},
	})

	line := ansi.Strip(serverUnreachableLine("http://localhost:8080", refused))

	expected := "  ✗ Cannot reach the Omnisave server at http://localhost:8080 — connection refused"
	if line != expected {
		t.Fatalf("expected %q, got %q", expected, line)
	}
}

func TestServerUnreachableReportsASilentServerAsATimeout(t *testing.T) {
	silent := fmt.Errorf("contact Omnisave server: %w", &url.Error{
		Op:  "Get",
		URL: "http://nas.local:8080/api/v1/omnisaves",
		Err: context.DeadlineExceeded,
	})

	line := ansi.Strip(serverUnreachableLine("http://nas.local:8080", silent))

	expected := "  ✗ Cannot reach the Omnisave server at http://nas.local:8080 — the request timed out"
	if line != expected {
		t.Fatalf("expected %q, got %q", expected, line)
	}
}

func TestServerRejectedTokenPointsAtTheConnectCommand(t *testing.T) {
	line := ansi.Strip(serverRejectedTokenLine("http://localhost:8080"))

	expected := "  ✗ The Omnisave server at http://localhost:8080 rejected this token; run omnisave-client connect"
	if line != expected {
		t.Fatalf("expected %q, got %q", expected, line)
	}
}
