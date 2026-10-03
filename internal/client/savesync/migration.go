package savesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// HoldReason is why the client, on its own evidence, keeps a lineage with a
// retired path format out of ordinary reconciliation (FDR-005, decision 14).
// It is an error so a Reporter formats it like any other cause. A failed
// migration proof is remembered in tracking state by its text, and a
// remembered reason compares equal to the constant it came from.
type HoldReason string

func (r HoldReason) Error() string { return string(r) }

const (
	// HoldUnreportedFormat: the server reports no path-format version, and
	// only a persisted version may admit sync or select a migration.
	HoldUnreportedFormat HoldReason = "the server does not report the lineage's path format"
	// HoldNoKnownMigration: nothing this client knows advances the version.
	HoldNoKnownMigration HoldReason = "no known migration advances this lineage's path format"
	// HoldNoMappingEvidence: this Device's save cannot prove the rename.
	HoldNoMappingEvidence HoldReason = "this device's save gives no evidence for the mapping"
	// HoldNoMatchingRevision: an unbound save may only claim a lineage whose
	// history holds its exact content.
	HoldNoMatchingRevision HoldReason = "this unbound save matches no complete revision in the lineage"
	// HoldNotAdvanced: the server accepted the migration but did not report
	// the version it leads to.
	HoldNotAdvanced HoldReason = "the server did not advance the lineage path format"
	// HoldNoNativeSave: placement onto an empty Device has no manifest to
	// prove a mapping with.
	HoldNoNativeSave HoldReason = "this device has no native save to prove the mapping"
)

// errMigrationHeld is an internal control result: the reason is already in
// the report, and the legacy lineage must not enter ordinary reconciliation.
var errMigrationHeld = errors.New("lineage path-format migration held")

// historyLoader returns one candidate save's history reader. A lineage's
// persisted path-format version decides whether it may enter ordinary
// reconciliation. Only native admits; a retired version is migrated or held;
// and a version nobody persisted — a server that predates the field, or a
// recovery that has not classified yet — holds outright, because
// classification can pick a migration but never stand in for the server's
// answer. A held lineage never reaches ordinary reconciliation: mixing a
// native commit into it would destroy the total rename the upgrade requires.
// The loader returns errMigrationHeld for a held lineage, after reporting it.
func (r *reconciliation) historyLoader(ctx context.Context, c candidate) func(string) ([]omnisave.Revision, error) {
	p, state, local := r.lineages, r.state, c.local
	// held is per candidate: another local save of the same game may hold
	// different evidence and must get its own attempt.
	held := make(map[string]bool)
	return func(omnisaveID string) ([]omnisave.Revision, error) {
		remoteSave, tracked := p.save(omnisaveID)
		if !tracked {
			return p.history(ctx, omnisaveID)
		}
		if held[omnisaveID] {
			return nil, errMigrationHeld
		}
		title, name := local.DisplayTitle(), omnisaveDisplayName(remoteSave)
		hold := func(reason error) ([]omnisave.Revision, error) {
			r.Report.MigrationHeld(title, name, reason)
			r.outcome.Held++
			held[omnisaveID] = true
			return nil, errMigrationHeld
		}
		for remoteSave.PathFormatVersion != omnisave.PathFormatNative {
			if remoteSave.PathFormatVersion == omnisave.PathFormatUnclassified {
				return hold(HoldUnreportedFormat)
			}
			migration, migratable := omnisave.MigrationFrom(remoteSave.PathFormatVersion)
			if !migratable || migration.Kind != omnisave.MigrationKindLocationRename {
				return hold(HoldNoKnownMigration)
			}
			history, err := p.history(ctx, omnisaveID)
			if err != nil {
				return nil, err
			}
			signature := saveSignature(c.save)
			digest := historyProofDigest(history)
			bound, isBound := state.BindingFor(local)
			boundHere := isBound && bound.OmnisaveID == omnisaveID
			if recorded, exists := state.HeldProofFor(local, omnisaveID); exists {
				if recorded.LocalSignature == signature &&
					recorded.HistoryDigest == digest && recorded.Bound == boundHere {
					// The exact evidence that failed the proof still stands;
					// re-reading the save could only reach the same hold.
					return hold(HoldReason(recorded.Cause))
				}
				state.ClearHeldProof(local, omnisaveID)
			}
			holdProven := func(reason HoldReason) ([]omnisave.Revision, error) {
				// A proof verdict is a function of the save's files, the
				// lineage's history, and whether the save is already bound
				// here; it is remembered against all three so later passes
				// skip re-reading a save that cannot answer differently,
				// and a save that since bound to this lineage — which
				// waives the content match — is proven again.
				state.RecordHeldProof(local, omnisaveID, tracking.HeldProof{
					LocalSignature: signature,
					HistoryDigest:  digest,
					Bound:          boundHere,
					Cause:          string(reason),
				})
				return hold(reason)
			}
			manifest, err := c.readManifest()
			if err != nil {
				return hold(err)
			}
			proof, proven := binding.ProveLocationMigration(migration.Location, manifest, history)
			if !proven {
				return holdProven(HoldNoMappingEvidence)
			}
			if !boundHere && !proof.ContentMatched {
				return holdProven(HoldNoMatchingRevision)
			}
			result, err := r.Server.MigrateLocations(ctx, omnisaveID, omnisave.MigrateLocations{
				ExpectedPathFormatVersion: remoteSave.PathFormatVersion,
				To:                        proof.To, Prefix: proof.Prefix,
			})
			if err != nil {
				// Server refusals are held but never remembered: they can
				// heal without the save or the history changing.
				return hold(err)
			}
			if result.PathFormatVersion != migration.ToVersion {
				return hold(HoldNotAdvanced)
			}
			// The rewritten history and advanced version are what the rest of
			// this pass must see, whichever consumer reads next. Looping here
			// makes the ordered table a chain rather than a one-step switch.
			p.migrated(omnisaveID, result.PathFormatVersion)
			state.ClearHeldProof(local, omnisaveID)
			r.Report.Migrated(title, name)
			remoteSave.PathFormatVersion = result.PathFormatVersion
		}
		return p.history(ctx, omnisaveID)
	}
}

// historyProofDigest summarizes everything a migration proof reads from a
// lineage's history — the revision set and each file's path and content
// identity — so a remembered verdict expires on any history change,
// including a deletion that never moves Current Revision.
func historyProofDigest(history []omnisave.Revision) string {
	digest := sha256.New()
	for _, revision := range history {
		fmt.Fprintf(digest, "%s\n", revision.ID)
		for _, file := range revision.Files {
			fmt.Fprintf(digest, "%s\x00%s\x00%d\n", file.Path, file.Artifact.SHA256, file.Artifact.Size)
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}
