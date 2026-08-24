package omnisave

import "testing"

// The upgrade chain is the one table the service's refusals, the
// repository's rewrite, recovery's replay and classification, and the
// client's proof all dispatch on (ADR-019). Its shape is what makes a
// version select exactly one step and a lineage cross several in order, so
// the invariants are asserted here rather than left to each dispatch site
// to rediscover.
func TestTheUpgradeChainIsOrderedAndSingleVoiced(t *testing.T) {
	chain := PathFormatMigrations()
	if len(chain) == 0 {
		t.Fatal("the chain must name at least the current format's predecessor")
	}
	leaves := make(map[int]bool, len(chain))
	for index, migration := range chain {
		if migration.FromVersion == PathFormatUnclassified {
			t.Errorf("step %d leaves the unclassified state, which no evidence can advance", index)
		}
		if migration.ToVersion <= migration.FromVersion {
			t.Errorf("step %d does not advance: %d -> %d", index, migration.FromVersion, migration.ToVersion)
		}
		if leaves[migration.FromVersion] {
			t.Errorf("step %d is a second step leaving version %d; a version must select exactly one",
				index, migration.FromVersion)
		}
		leaves[migration.FromVersion] = true
		if migration.Kind == "" {
			t.Errorf("step %d names no strategy", index)
		}
		if index > 0 && chain[index-1].ToVersion != migration.FromVersion {
			t.Errorf("step %d starts at %d but the previous step ended at %d; the chain must be adjacent",
				index, migration.FromVersion, chain[index-1].ToVersion)
		}
	}
	if last := chain[len(chain)-1].ToVersion; last != PathFormatNative {
		t.Errorf("the chain ends at %d, not the current format %d", last, PathFormatNative)
	}
}

// A rename may never target a retired vocabulary: the retired spelling has
// to keep meaning "not yet migrated", or a migrated lineage would classify
// as legacy again on the next recovery.
func TestNoRenameTargetsARetiredVocabulary(t *testing.T) {
	for index, migration := range PathFormatMigrations() {
		if migration.Kind != MigrationKindLocationRename {
			if migration.Location != "" {
				t.Errorf("step %d is not a rename but names a location", index)
			}
			continue
		}
		if migration.Location == "" {
			t.Errorf("step %d renames but names no location to retire", index)
		}
		if !IsRetiredLocation(migration.Location) {
			t.Errorf("step %d retires %q, which IsRetiredLocation does not recognize",
				index, migration.Location)
		}
	}
}

// Selection is what every dispatch site relies on: a retired version finds
// its step, and the current one — along with anything unrecognized — finds
// none, so the caller fails closed instead of guessing.
func TestOnlyRetiredVersionsSelectAMigration(t *testing.T) {
	for _, migration := range PathFormatMigrations() {
		selected, found := MigrationFrom(migration.FromVersion)
		if !found || selected != migration {
			t.Errorf("version %d selected %+v (found %v)", migration.FromVersion, selected, found)
		}
	}
	for _, version := range []int{PathFormatUnclassified, PathFormatNative, PathFormatNative + 1, -1} {
		if _, found := MigrationFrom(version); found {
			t.Errorf("version %d selected a migration; it must fail closed", version)
		}
	}
}
