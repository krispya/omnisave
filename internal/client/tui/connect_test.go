package tui

import (
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
