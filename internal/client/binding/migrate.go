package binding

import (
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// MirrorLocation is the location identity the retired mirror representation
// minted lineages under: Steam's per-app cloud staging folder, which is a
// transport and never a save (FDR-003, decision 10). It survives only as
// the vocabulary migration renames away from.
const MirrorLocation = "remote"

// LocationMigration is a proven rename from the mirror vocabulary into the
// local save's own: `remote/rest` becomes `To/Prefix/rest`.
type LocationMigration struct {
	From   string
	To     string
	Prefix string
	// Corroborated counts name matches whose content hash also agreed —
	// evidence strength a report can show, not a gate.
	Corroborated int
}

// SpeaksMirror reports whether a lineage's history is written in the mirror
// vocabulary — the precondition for proposing a migration at all.
func SpeaksMirror(history []omnisave.Revision) bool {
	for _, revision := range history {
		for _, file := range revision.Files {
			if strings.HasPrefix(file.Path, MirrorLocation+"/") {
				return true
			}
		}
	}
	return false
}

// ProveLocationMigration maps a mirror-vocabulary lineage into the local
// save's vocabulary using the save's manifest as evidence. The standard is
// the one every store-facing decision here uses: nothing is guessed. Each
// lineage name that matches exactly one manifest path by suffix nominates a
// (location, prefix) anchoring, every nomination must agree, and at least
// one is required; a name matching several manifest paths nominates
// nothing. Hash agreement between a nominated pair is counted as
// corroboration. No anchoring, or a disagreement, proves no migration —
// renaming on a guess would mint history under names the game never used.
func ProveLocationMigration(
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
			rest, isMirror := strings.CutPrefix(file.Path, MirrorLocation+"/")
			if !isMirror || rest == "" {
				// A lineage speaking anything but the mirror alone is not a
				// pure mirror lineage; the server would refuse the rename,
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

	proof := LocationMigration{From: MirrorLocation}
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
	return proof, true
}
