package savesync

import (
	"context"
	"io"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/device"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Server is the Omnisave server as the domain uses it: the Library a Device
// registers its games with, and the lineages its saves sync against.
// *remote.Client satisfies it.
//
// The domain recognizes errors only by the domain values they match with
// errors.Is:
//   - TrackGame and UntrackGame report a game the server no longer holds
//     with an error matching catalog.ErrNotFound.
//   - CommitRevision reports a commit whose expected Current Revision has
//     moved with an error matching omnisave.ErrConflict.
//
// Any other error fails only the call that returned it.
type Server interface {
	RegisterDevice(ctx context.Context, id string, input device.Registration) error
	ReportDeviceStatus(ctx context.Context, deviceID string, playingGameIDs []string) error
	ListGameIDs(ctx context.Context) ([]string, error)
	ResolveGame(ctx context.Context, input catalog.Evidence) (*catalog.Resolution, error)
	TrackGame(ctx context.Context, gameID, deviceID string, input catalog.TrackGame) error
	UntrackGame(ctx context.Context, gameID, deviceID string) error

	ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error)
	ForkOmnisave(ctx context.Context, id string, input omnisave.ForkOmnisave) (*omnisave.ForkResult, error)
	// MigrateLocations refuses with *omnisave.MigrationRefused when the
	// server will not rename the lineage; that error reaches the Reporter as
	// the reason the lineage stays held.
	MigrateLocations(ctx context.Context, id string, input omnisave.MigrateLocations) (*omnisave.MigrationResult, error)
	ReportAchievements(ctx context.Context, omnisaveID string, unlocks []omnisave.AchievementUnlock) ([]omnisave.Achievement, error)

	// The content half: what binding needs to seed, commit, and place saves
	// (binding.Server and binding.ArtifactSource).
	CreateOmnisave(ctx context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error)
	CommitRevision(ctx context.Context, omnisaveID string, input omnisave.CreateRevision) (*omnisave.Revision, error)
	UploadArtifact(ctx context.Context, artifact omnisave.Artifact, content io.Reader) error
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
	DeleteOmnisave(ctx context.Context, id string) error
}

// Adapters is this Device's save discovery as the domain uses it.
// *client.Scanner satisfies it. A nil Adapters is allowed where a caller has
// no adapters to offer: placements then finish with nothing to settle, and no
// achievements are read.
type Adapters interface {
	// Scan discovers targets, games, and saves without changing them.
	Scan(ctx context.Context) ([]client.TargetScan, error)
	// Adapter returns the adapter a scan result came from, so a placement
	// can be finished in its store (target.PlacementFinisher).
	Adapter(name string) (target.Adapter, bool)
	// UnlockedAchievements is what the save's own target records as
	// unlocked; a target that cannot see achievements reports none.
	UnlockedAchievements(ctx context.Context, discovered target.Target, game target.InstalledGame, save target.Save) ([]target.Achievement, error)
	// PlayingMatchers builds the process matchers that tell whether a
	// tracked game is running.
	PlayingMatchers(scans []client.TargetScan, tracked func(gameID string) bool) []running.Matcher
}

// Reporter hears every decision a pass makes, one game at a time, keyed by
// the game's display title. Omnisaves are named by display name. Errors and
// reasons arrive as values, never as prose; presenting them is the
// Reporter's job. *tui.TrackReport satisfies it.
type Reporter interface {
	// Working names the game the pass has in hand; it is called where work
	// begins, never where a pass merely considers a game. Idle says the pass
	// holds no game.
	Working(title string)
	Idle()

	// Library and tracking.
	Added(title, canonical string)
	Linked(title, canonical string)
	Pending(title string)
	Removed(title string)
	DeletedOnServer(title string)
	Failed(title string, err error)
	SyncFailed(err error)

	// Binding and placement onto a Device.
	BindingFailed(err error)
	SaveDeleted(title string)
	NoSave(title string)
	SaveAvailable(title string)
	SaveLocationUnavailable(title string)
	Stale(title, omnisaveName string)
	Unbound(title string)

	// Sync of a bound save.
	SyncedWith(title, omnisaveName string, at time.Time)
	CurrentMoved(title, omnisaveName string)
	PullDeferred(title, omnisaveName string)
	Branched(title, omnisaveName string)
	BranchKept(title, omnisaveName string)
	Forked(title, omnisaveName string)
	// Diverged leaves a save waiting for a person; forkName is what
	// answering "fork" would create, empty when the server picks the name.
	Diverged(title, omnisaveName, forkName string)
	SaveFailed(title string, err error)

	// Path-format migration. reason is a HoldReason, or the server's error
	// when it refused or could not be asked (*omnisave.MigrationRefused for a
	// refusal).
	Migrated(title, omnisaveName string)
	MigrationHeld(title, omnisaveName string, reason error)

	// Store registration after a placement (target.PlacementReport).
	StoreRegistered(title string, count int)
	StoreRegistrationSkipped(title, reason string)
	StoreRegistrationFailed(title string, err error)
	StoreRegistrationIncomplete(title string, count int)
	StoreDeleted(title string, deleted int)
	StoreExtras(title string, extras int)

	// Unlocked names achievements the server recorded from this pass.
	Unlocked(title string, names []string)
}

// Ports are the collaborators one reconciliation works through.
type Ports struct {
	// Checkpoint persists the placement journal before filesystem or cloud
	// mutations. Production callers must supply it; nil is for in-memory tests.
	Checkpoint func(tracking.State) error
	Server     Server
	Adapters   Adapters
	Report     Reporter
}
