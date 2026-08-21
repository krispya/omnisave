// Package saveprofile defines provider-neutral game save-location knowledge.
package saveprofile

import (
	"context"
	"errors"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

var ErrNotFound = errors.New("save profile: not found")

// ErrNoSaveFolder is a source saying it knows the game and knows it keeps no
// save folder on this Device — a Steam game whose cloud saves live only
// behind the store's API, say. It is not a miss: a miss leaves open that
// some other source knows where the saves are, and this closes it.
var ErrNoSaveFolder = errors.New("save profile: game keeps no save folder on this device")

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

// FallbackProvider is a Provider that can answer a second time, for Devices
// where its first answer located nothing that applies. A scan asks for the
// fallback only after resolving the primary answer, because whether a rule
// applies is something only resolution against this Device can say.
type FallbackProvider interface {
	Provider
	FindFallback(context.Context, target.GameIdentity) (*Profile, error)
}

// Fallback pairs a primary source of save-location knowledge with a second
// one consulted only when the primary has no rule that applies here. Order
// is what keeps location identities stable: the primary answers wherever it
// can, so a lineage minted under its spelling is never renamed by a fallback
// that happens to know the same game.
type Fallback struct {
	Primary   Provider
	Secondary Provider
}

func (f Fallback) Find(ctx context.Context, identity target.GameIdentity) (*Profile, error) {
	return f.Primary.Find(ctx, identity)
}

func (f Fallback) FindFallback(ctx context.Context, identity target.GameIdentity) (*Profile, error) {
	return f.Secondary.Find(ctx, identity)
}
