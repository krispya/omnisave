// Package saveprofile defines provider-neutral game save-location knowledge.
package saveprofile

import (
	"context"
	"errors"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

var ErrNotFound = errors.New("save profile: not found")

const (
	OSWindows = "windows"
	OSLinux   = "linux"
	OSMacOS   = "darwin"
)

// Rule identifies one possible save location and when it applies.
type Rule struct {
	ID    string
	Path  string
	OS    string
	Store string
	Kind  string
}

// Profile is normalized save-location knowledge for one game.
type Profile struct {
	Provider   string
	ProviderID string
	Title      string
	Rules      []Rule
}

// Provider finds save-location knowledge from game identity evidence.
type Provider interface {
	Find(context.Context, target.GameIdentity) (*Profile, error)
}
