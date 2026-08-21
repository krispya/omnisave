package binding

import (
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func mirrorHistory(paths map[string]string) []omnisave.Revision {
	files := make([]omnisave.RevisionFile, 0, len(paths))
	for path, hash := range paths {
		files = append(files, omnisave.RevisionFile{
			Path: path, Artifact: omnisave.Artifact{SHA256: hash},
		})
	}
	return []omnisave.Revision{{ID: "revision-1", Files: files}}
}

func manifestOf(paths map[string]string) []omnisave.RevisionFile {
	manifest := make([]omnisave.RevisionFile, 0, len(paths))
	for path, hash := range paths {
		manifest = append(manifest, omnisave.RevisionFile{
			Path: path, Artifact: omnisave.Artifact{SHA256: hash},
		})
	}
	return manifest
}

// The measured case: the mirror anchors one segment below the rule — the
// account folder — so the proof must recover both location and prefix.
func TestProvesAnAccountPrefixedMapping(t *testing.T) {
	manifest := manifestOf(map[string]string{
		"aaaa1111/76561198027955092/profile.save":                        "hash-profile",
		"aaaa1111/76561198027955092/profile1/saves/progress.save":        "hash-progress",
		"aaaa1111/76561198027955092/profile1/saves/progress.save.backup": "hash-progress",
	})
	history := mirrorHistory(map[string]string{
		"remote/profile.save":                 "hash-profile",
		"remote/profile1/saves/progress.save": "old-progress",
	})
	proof, proven := ProveLocationMigration(manifest, history)
	if !proven {
		t.Fatal("expected a proof")
	}
	if proof.To != "aaaa1111" || proof.Prefix != "76561198027955092" {
		t.Fatalf("proof = %+v", proof)
	}
	if proof.Corroborated != 1 {
		t.Fatalf("corroborated = %d", proof.Corroborated)
	}
}

func TestProvesAFlatMapping(t *testing.T) {
	manifest := manifestOf(map[string]string{
		"e986be36/file0":         "hash-a",
		"e986be36/undertale.ini": "hash-b",
	})
	history := mirrorHistory(map[string]string{
		"remote/file0": "hash-a",
	})
	proof, proven := ProveLocationMigration(manifest, history)
	if !proven || proof.To != "e986be36" || proof.Prefix != "" {
		t.Fatalf("proof = %+v, proven = %v", proof, proven)
	}
}

func TestRefusesAmbiguousAndConflictingEvidence(t *testing.T) {
	// One name, two homes: nominates nothing, so nothing is proven.
	ambiguous := manifestOf(map[string]string{
		"loc/a/SaveData": "x",
		"loc/b/SaveData": "y",
	})
	if _, proven := ProveLocationMigration(ambiguous, mirrorHistory(map[string]string{
		"remote/SaveData": "x",
	})); proven {
		t.Fatal("an ambiguous name must not prove a mapping")
	}
	// Two names anchoring to different prefixes: disagreement refuses.
	conflicting := manifestOf(map[string]string{
		"loc/one/alpha.sav": "x",
		"loc/two/beta.sav":  "y",
	})
	if _, proven := ProveLocationMigration(conflicting, mirrorHistory(map[string]string{
		"remote/alpha.sav": "x",
		"remote/beta.sav":  "y",
	})); proven {
		t.Fatal("conflicting anchors must not prove a mapping")
	}
}

func TestRefusesImpureAndUnmatchedLineages(t *testing.T) {
	manifest := manifestOf(map[string]string{"loc/save.dat": "x"})
	if _, proven := ProveLocationMigration(manifest, mirrorHistory(map[string]string{
		"remote/save.dat": "x",
		"other/file.dat":  "y",
	})); proven {
		t.Fatal("a lineage speaking two locations is not a mirror lineage")
	}
	if _, proven := ProveLocationMigration(manifest, mirrorHistory(map[string]string{
		"remote/unheard-of.bin": "x",
	})); proven {
		t.Fatal("a lineage matching nothing proves no anchor")
	}
	if SpeaksMirror(mirrorHistory(map[string]string{"loc/save.dat": "x"})) {
		t.Fatal("a rule-vocabulary lineage does not speak the mirror")
	}
	if !SpeaksMirror(mirrorHistory(map[string]string{"remote/save.dat": "x"})) {
		t.Fatal("a mirror lineage speaks the mirror")
	}
}
