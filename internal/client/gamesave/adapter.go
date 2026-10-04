// Package gamesave owns the contract for Game Save Adapters: game-specific
// knowledge of which files in a save sync ignores, and of save slots: the
// profiles, characters, or slots a game keeps independently. Location
// providers find whole saves; sandboxed extensions refine them.
package gamesave

import (
	"context"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Adapter identifies coherent save slots without changing native files.
// Launcher and sync adapters consume its result, never interpret a game's schema.
// Discovery returns complete, non-overlapping slots, or none with an error.
type Adapter interface {
	ID() string
	Supports(target.GameIdentity) bool
	// Ignored names the files sync leaves out of this game's saves, whole or
	// slot. Discovered slots already carry it.
	Ignored() target.IgnoredFiles
	Discover(context.Context, target.InstalledGame, []target.SaveDestination) ([]Slot, error)
}

// Slot pairs a save slot's content with its destination. An empty slot
// has no Files and remains a destination; it must never seed an empty history.
type Slot struct {
	Save        target.Save
	Destination target.SaveDestination
}

// Discovery keeps support distinct from availability. Errors never permit sync
// to silently broaden a previously selected independent boundary.
type Discovery struct {
	Adapter string
	Found   []Slot
	Err     error
}
