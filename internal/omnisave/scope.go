package omnisave

import "regexp"

// SaveScope describes a history's portable content boundary. The zero value is
// a whole save, including histories created before scope support. Local account
// and slot identities belong to Device bindings, never to this contract.
type SaveScope struct {
	Kind    string `json:"kind,omitempty"`
	Adapter string `json:"adapter,omitempty"`
}

// ScopeSlot is one save slot a Game Save Adapter can restore on its own: a
// profile, character, or slot, whatever the game calls it. The adapter ID
// carries the game-specific meaning.
const ScopeSlot = "slot"

var adapterIdentity = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)

// Valid refuses partial or unknown boundaries instead of treating them as whole saves.
func (s SaveScope) Valid() bool {
	return s == (SaveScope{}) || s.Kind == ScopeSlot && adapterIdentity.MatchString(s.Adapter)
}
