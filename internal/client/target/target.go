// Package target defines installed applications, games, and native save data.
package target

import (
	"context"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
)

// Target is one application installation resolved on this machine.
type Target struct {
	ID       string
	Adapter  string
	Source   string
	Root     string
	Location string
}

// GameIdentity carries adapter evidence for identifying an installed game.
type GameIdentity struct {
	Identifiers  []catalog.GameIdentifier
	Fingerprints []catalog.GameFingerprint
	Title        string
	Platform     string
	ContentPath  string
}

// Identifier returns an external ID in the requested namespace.
func (identity GameIdentity) Identifier(namespace string) (string, bool) {
	for _, identifier := range identity.Identifiers {
		if strings.EqualFold(identifier.Namespace, namespace) {
			return identifier.Value, true
		}
	}
	return "", false
}

// StoreIdentifier returns the first store application identity.
func (identity GameIdentity) StoreIdentifier() (store, value string, ok bool) {
	for _, identifier := range identity.Identifiers {
		if strings.HasSuffix(identifier.Namespace, ".app") {
			return strings.TrimSuffix(identifier.Namespace, ".app"), identifier.Value, true
		}
	}
	return "", "", false
}

// DisplayTitle returns the discovered title or a local fallback.
func (identity GameIdentity) DisplayTitle(fallback string) string {
	if strings.TrimSpace(identity.Title) != "" {
		return identity.Title
	}
	return fallback
}

// Environment describes where an installed game runs on this machine.
type Environment struct {
	HostOS     string
	Runtime    string
	Home       string
	StoreRoot  string
	PrefixRoot string
	Variables  map[string]string
}

// InstalledGame is one game exposed through a local target.
type InstalledGame struct {
	ID          string
	TargetID    string
	Identity    GameIdentity
	InstallRoot string
	Environment Environment
	Metadata    map[string]string
}

// File is one native file belonging to a discovered save.
type File struct {
	Path         string
	LocationID   string
	RelativePath string
	Size         int64
	Modified     time.Time
}

// Save is one adapter-defined native save set, which may contain multiple files.
type Save struct {
	ID       string
	TargetID string
	GameID   string
	Kind     string
	Files    []File
	Metadata map[string]string
	// LocationAliases are every identity the save's logical location is known
	// under. Profile rules spell one location differently per OS, and a
	// revision minted on another OS names the location by that OS's spelling;
	// the aliases are what let the two be recognized as the same place.
	// Empty means the save's locations answer only to their own identities.
	LocationAliases []string
}

// SaveLocationKind describes how revision paths map into a prospective
// native save location.
type SaveLocationKind string

const (
	// SaveLocationDirectory treats Path as the root for revision-relative files.
	SaveLocationDirectory SaveLocationKind = "directory"
	// SaveLocationFile treats Path as the only file the location can contain.
	SaveLocationFile SaveLocationKind = "file"
	// SaveLocationUnknown is resolved from the selected revision. It is used
	// when profile knowledge names a path that does not exist yet.
	SaveLocationUnknown SaveLocationKind = "unknown"
)

// SaveLocation identifies one machine-local destination for canonical
// revision paths.
type SaveLocation struct {
	// ID is the canonical location prefix stored in revision file paths.
	ID string
	// Path is an absolute native directory, file, or not-yet-existing profile path.
	Path string
	Kind SaveLocationKind
}

// SaveDestination describes where an adapter-native save would live before any of
// its files exist. Its ID is the Local Save identity used after placement.
type SaveDestination struct {
	// ID remains the Local Save identity after the current revision is materialized.
	ID        string
	TargetID  string
	GameID    string
	Kind      string
	Locations []SaveLocation
	Metadata  map[string]string
	// LocationAliases mirrors Save.LocationAliases for a save that does not
	// exist yet, so a revision spelled by another OS can still be placed.
	LocationAliases []string
}

// Adapter discovers application targets, their games, current saves, and the
// prospective native destinations where saves can be materialized.
type Adapter interface {
	Name() string
	DiscoverTargets(context.Context) ([]Target, error)
	DiscoverGames(context.Context, Target) ([]InstalledGame, error)
	DiscoverSaves(context.Context, Target, InstalledGame) ([]Save, error)
	DiscoverSaveDestinations(context.Context, Target, InstalledGame) ([]SaveDestination, error)
}

// PlacementFinisher is an Adapter that has more to do after files reach a
// game's own save folder. A store can keep bookkeeping the game trusts over
// the folder itself — Steam's cloud file registry decides whether an API
// game's live state exists at all (FDR-005) — and placing files without
// settling that bookkeeping leaves a restore the game may discard on
// launch. Placement flows call this after files land; an adapter with
// nothing to settle is simply not a PlacementFinisher.
type PlacementFinisher interface {
	FinishPlacement(ctx context.Context, discovered Target, game InstalledGame, save Save) (PlacementReport, error)
}

// PlacementReport is what finishing a placement did, in the store's own
// vocabulary of file names.
type PlacementReport struct {
	// Registered are store entries created or refreshed from placed files.
	Registered []string
	// Extras are store entries the placement carried no file for, left in
	// place and surfaced so their effect on the game can be seen.
	Extras []string
	// Skipped is why nothing was attempted; empty when work ran.
	Skipped string
	// Failed are store writes that did not take, as name → cause.
	Failed map[string]string
}

// Activity detects active InstalledGame IDs from a shared process snapshot.
type Activity interface {
	RunningGames(ctx context.Context, snapshot *running.Snapshot, discovered Target, games []InstalledGame) (map[string]bool, error)
}

// Achievement is one achievement a target's own records show unlocked, named
// as the target names it. UnlockedAt is the target's time, not this machine's:
// it is what makes the same unlock the same instant on every Device.
type Achievement struct {
	// ID is the target's stable key for the achievement, unique within its
	// game — Steam's API name, for instance.
	ID          string
	Name        string
	Description string
	UnlockedAt  time.Time
}

// Achievements reports the achievements a target has recorded as unlocked for
// one discovered save. It is optional: an adapter that cannot see achievements
// simply does not implement it, and the save's history carries no marks.
//
// Implementations are best-effort. Anything unreadable — an absent record, a
// format that moved on — reports no achievements rather than an error, because
// a save syncs whether or not its achievements can be read.
type Achievements interface {
	UnlockedAchievements(ctx context.Context, discovered Target, game InstalledGame, save Save) ([]Achievement, error)
}
