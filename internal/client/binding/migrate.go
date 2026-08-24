package binding

import (
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// LocationMigration is a proven rename out of a retired location vocabulary
// into the local save's own: `remote/rest` becomes `To/Prefix/rest`. The
// retired spelling itself lives in the lineage's migration rule
// (omnisave.PathFormatMigrations), so proof and rename can never drift.
type LocationMigration struct {
	To     string
	Prefix string
	// Corroborated counts name matches whose content hash also agreed —
	// evidence strength a report can show, not a gate.
	Corroborated int
	// ContentMatched means one complete historical snapshot agrees exactly
	// with the local files after applying this mapping, including the absence
	// of extra local files. An unbound save needs this stronger lineage
	// association before it may nominate a migration.
	ContentMatched bool
}

// ProveLocationMigration maps a lineage speaking the retired location into
// the local save's vocabulary using the save's manifest as evidence. The
// retired location comes from the lineage's migration rule
// (omnisave.MigrationFrom), so any location rename in the chain is provable
// with this one prover. The standard is the one every store-facing decision
// here uses: nothing is guessed. Each lineage name that matches exactly one
// manifest path by suffix nominates a (location, prefix) anchoring, every
// nomination must agree, and at least one is required; a name matching
// several manifest paths nominates nothing. Hash agreement between a
// nominated pair is counted as corroboration. No anchoring, or a
// disagreement, proves no migration — renaming on a guess would mint
// history under names the game never used.
func ProveLocationMigration(
	retired string,
	manifest []omnisave.RevisionFile,
	history []omnisave.Revision,
) (LocationMigration, bool) {
	type entry struct {
		location string
		relative string
		hash     string
	}
	entries := make([]entry, 0, len(manifest))
	for _, file := range manifest {
		location, relative, found := strings.Cut(file.Path, "/")
		if !found {
			continue
		}
		entries = append(entries, entry{
			location: location,
			relative: relative,
			hash:     file.Artifact.SHA256,
		})
	}

	names := make(map[string]string)
	for _, revision := range history {
		for _, file := range revision.Files {
			rest, speaks := strings.CutPrefix(file.Path, retired+"/")
			if !speaks || rest == "" {
				// A lineage speaking anything but the retired location alone
				// is not single-voiced; the server would refuse the rename,
				// so no proof is offered for it.
				return LocationMigration{}, false
			}
			// The newest hash a name carried; corroboration only needs one.
			names[rest] = file.Artifact.SHA256
		}
	}
	if len(names) == 0 {
		return LocationMigration{}, false
	}

	proof := LocationMigration{}
	nominated := false
	for name, hash := range names {
		matched := entry{}
		matches := 0
		for _, candidate := range entries {
			if candidate.relative == name ||
				strings.HasSuffix(candidate.relative, "/"+name) {
				matched = candidate
				matches++
			}
		}
		if matches != 1 {
			continue
		}
		prefix := strings.TrimSuffix(strings.TrimSuffix(matched.relative, name), "/")
		if nominated && (matched.location != proof.To || prefix != proof.Prefix) {
			return LocationMigration{}, false
		}
		proof.To = matched.location
		proof.Prefix = prefix
		nominated = true
		if matched.hash != "" && strings.EqualFold(matched.hash, hash) {
			proof.Corroborated++
		}
	}
	if !nominated {
		return LocationMigration{}, false
	}
	for _, revision := range history {
		rewritten := make([]omnisave.RevisionFile, 0, len(revision.Files))
		for _, file := range revision.Files {
			rest, _ := strings.CutPrefix(file.Path, retired+"/")
			target := proof.To + "/"
			if proof.Prefix != "" {
				target += proof.Prefix + "/"
			}
			target += rest
			file.Path = target
			rewritten = append(rewritten, file)
		}
		if len(rewritten) > 0 && sameManifest(manifest, rewritten) {
			proof.ContentMatched = true
			break
		}
	}
	return proof, true
}
