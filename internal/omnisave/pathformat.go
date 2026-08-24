package omnisave

import "time"

// Path-format versions distinguish the retired Steam mirror vocabulary from
// the native save-location vocabulary current clients read and write. The
// value belongs to a lineage so its upgrade state is explicit rather than
// inferred during every synchronization pass.
const (
	// PathFormatUnclassified marks a lineage whose vocabulary has no
	// persisted answer — a store recovery that has not classified it yet,
	// or a server that predates the field. Every layer holds an
	// unclassified lineage: only a persisted version may admit ordinary
	// synchronization or select a migration.
	PathFormatUnclassified = 0
	PathFormatMirror       = 1
	PathFormatNative       = 2
)

// MirrorLocation is the location identity the retired mirror representation
// minted lineages under: Steam's per-app cloud staging folder, which is a
// transport and never a save (FDR-003, decision 10). It survives only as the
// vocabulary its migration renames away from.
const MirrorLocation = "remote"

// MaxRevisionPathLength bounds every canonical save path the server stores,
// whether a commit minted it or a location migration renamed it into place.
const MaxRevisionPathLength = 1024

// MigrationKindLocationRename is the one migration strategy that exists: an
// atomic whole-history rename of every `Location/rest` path into the native
// location a Device's own save proves. A future format change introduces a
// new kind beside it rather than bending this one.
const MigrationKindLocationRename = "location_rename"

// PathFormatMigration is one step of the lineage upgrade chain: how a
// history at FromVersion advances to ToVersion, and which strategy performs
// it. Exactly one migration leaves each retired version.
type PathFormatMigration struct {
	FromVersion int
	ToVersion   int
	Kind        string
	// Location is the vocabulary a location rename retires; empty for any
	// future kind that is not a rename.
	Location string
}

// PathFormatMigrations is the ordered upgrade chain, oldest first. The
// service's refusals, the repository's rewrite, recovery's replay and
// classification, and the client's proof all dispatch on this table, so the
// mirror rename is one concrete migration rather than the framework itself.
// A new location rename is one added row; a new strategy adds its kind and
// the code that performs it at each dispatch site.
func PathFormatMigrations() []PathFormatMigration {
	return []PathFormatMigration{{
		FromVersion: PathFormatMirror,
		ToVersion:   PathFormatNative,
		Kind:        MigrationKindLocationRename,
		Location:    MirrorLocation,
	}}
}

// MigrationFrom finds the migration that advances a lineage at version, if
// version names a retired format at all.
func MigrationFrom(version int) (PathFormatMigration, bool) {
	for _, migration := range PathFormatMigrations() {
		if migration.FromVersion == version {
			return migration, true
		}
	}
	return PathFormatMigration{}, false
}

// IsRetiredLocation reports whether name is a retired format's vocabulary.
// A migration may never rename into one: the spelling must stay free to
// mean "not yet migrated".
func IsRetiredLocation(name string) bool {
	for _, migration := range PathFormatMigrations() {
		if migration.Location != "" && migration.Location == name {
			return true
		}
	}
	return false
}

// PathMigration is one applied migration step, recorded on the lineage as a
// durable fact. Snapshot manifests are immutable, so a rename cannot live
// in them: recovery replays these facts over imported manifests, and a
// rebuilt database then speaks the vocabulary the lineage reached rather
// than the one its manifests were minted in. Kind is the strategy that was
// applied. Both version endpoints are stored so replay does not have to
// infer historical facts from the current migration table.
type PathMigration struct {
	Kind        string    `json:"kind,omitempty"`
	FromVersion int       `json:"from_version"`
	ToVersion   int       `json:"to_version"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	MigratedAt  time.Time `json:"migrated_at"`
}
