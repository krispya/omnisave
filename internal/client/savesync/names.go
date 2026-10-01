package savesync

import (
	"strings"
	"unicode/utf8"

	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// maxDisplayName is the server's display name limit, in runes. A name past it
// is rejected outright, so the client trims rather than let a Device with a
// long hostname fail the same way on every retry.
const maxDisplayName = 100

func omnisaveDisplayName(save omnisave.Omnisave) string {
	if name := strings.TrimSpace(save.DisplayName); name != "" {
		return name
	}
	id := save.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return "Omnisave " + id
}

// deviceDisplayName is the Device's name as it may name a branch revision on
// its own: trimmed to what the server accepts.
func deviceDisplayName(state *tracking.State) string {
	return truncateRunes(strings.TrimSpace(state.Device.Name), maxDisplayName)
}

// deconflictName names a preservation fork or seed for the lineage it left
// and the Device whose progress it keeps — "Save 1 (Steam Deck)". A branch
// revision can carry the Device's name alone because the tree around it says
// what it belongs to; a fork sits beside its source on the poster wall, so
// the source's name is the only link the name can carry. A Device without a
// name has no provenance to record, so the empty result lets the server pick
// its own default. The server numbers a repeat — a second divergence becomes
// "Save 1 (Steam Deck) 2" (FDR-003, decision 8). A name too long for the
// server's limit loses base name, never the Device: the Device is the whole
// point of the name, and two Devices trimmed to the same text would be
// indistinguishable.
func deconflictName(source omnisave.Omnisave, deviceName string) string {
	if deviceName == "" {
		return ""
	}
	base := strings.TrimSpace(source.DisplayName)
	suffix := " (" + deviceName + ")"
	room := maxDisplayName - utf8.RuneCountInString(suffix)
	if base == "" || room < 1 {
		return truncateRunes(deviceName, maxDisplayName)
	}
	return truncateRunes(base, room) + suffix
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
