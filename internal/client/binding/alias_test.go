package binding_test

import (
	"context"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// One logical location, two OS spellings: a manifest matches a revision
// minted under another spelling only when that spelling is among the save's
// known location identities, and only when both sides spell one location.
func TestManifestsMatchAcrossAliasedSpellings(t *testing.T) {
	manifest := []omnisave.RevisionFile{revisionFile("linux1/file0", "progress", "application/octet-stream")}
	revision := omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "progress", "application/octet-stream"),
	}}
	if !binding.MatchesManifest(manifest, []string{"linux1", "mac1"}, revision) {
		t.Fatal("expected the aliased spelling to match")
	}
	if binding.MatchesManifest(manifest, nil, revision) {
		t.Fatal("expected a foreign spelling without aliases to stay unmatched")
	}
	if binding.MatchesManifest(manifest, []string{"windows1"}, revision) {
		t.Fatal("expected an unrelated alias set to stay unmatched")
	}
	several := append([]omnisave.RevisionFile{
		revisionFile("other/config.ini", "settings", "application/octet-stream"),
	}, manifest...)
	if binding.MatchesManifest(several, []string{"linux1", "mac1", "other"}, revision) {
		t.Fatal("expected a several-location manifest to refuse translation")
	}
}

// A commit onto a lineage spelled by another OS keeps the lineage's spelling,
// so one Omnisave never mixes vocabularies however many OSes commit to it.
func TestPushKeepsTheLineageSpelling(t *testing.T) {
	directory := t.TempDir()
	file := writeFile(t, directory, "file0", "deck-progress")
	save := target.Save{
		Files:           []target.File{{Path: file.Path, LocationID: "linux1", RelativePath: "file0", Size: file.Size}},
		LocationAliases: []string{"linux1", "mac1"},
	}
	parent := revisionFile("mac1/file0", "mac-progress", "application/octet-stream")
	server := newFakeServer()

	revision, err := binding.Push(context.Background(), server, "omnisave-1", save, "revision-0",
		[]omnisave.RevisionFile{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Files) != 1 || revision.Files[0].Path != "mac1/file0" {
		t.Fatalf("expected the commit spelled in the lineage's vocabulary, got %+v", revision.Files)
	}
	committed := server.committed["omnisave-1"]
	if len(committed.Deletes) != 0 {
		t.Fatalf("expected the respelled path to cover the parent's, got deletes %v", committed.Deletes)
	}
	if server.uploadCalls != 1 {
		t.Fatalf("expected the respelled upload to find its local file, got %d uploads", server.uploadCalls)
	}
}

// CanApply accepts a current revision spelled by another OS when the save's
// one location answers to that identity, and still refuses a spelling the
// save has never heard of.
func TestCanApplyAcceptsAliasedSpellings(t *testing.T) {
	local := target.Save{
		Files: []target.File{{
			Path:       "/nowhere/UNDERTALE/file0",
			LocationID: "linux1", RelativePath: "file0",
		}},
		LocationAliases: []string{"linux1", "mac1"},
	}
	foreign := omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "content", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, foreign); err != nil {
		t.Fatalf("expected the aliased spelling to be appliable, got %v", err)
	}
	unknown := omnisave.Revision{ID: "revision-2", Files: []omnisave.RevisionFile{
		revisionFile("windows9/file0", "content", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, unknown); err == nil {
		t.Fatal("expected an unknown spelling to be refused")
	}
}

// A save shaped like one flat file accepts an aliased spelling only under
// its own name: a foreign name would be placed beside the real file and the
// game would keep reading the old one — or nothing.
func TestCanApplyRefusesRenamingASingleFileSave(t *testing.T) {
	local := target.Save{
		Files: []target.File{{
			Path:       "/nowhere/saves/GameSave.dat",
			LocationID: "mac1", RelativePath: "GameSave.dat",
		}},
		LocationAliases: []string{"mac1", "win1"},
	}
	renamed := omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{
		revisionFile("win1/save.dat", "content", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, renamed); err == nil {
		t.Fatal("expected a foreign filename to be refused for a single-file save")
	}
	sameName := omnisave.Revision{ID: "revision-2", Files: []omnisave.RevisionFile{
		revisionFile("win1/GameSave.dat", "content", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, sameName); err != nil {
		t.Fatalf("expected the native filename to be accepted, got %v", err)
	}
}

// A directory that happens to contain one file is not frozen in that shape:
// a later revision may add another file beneath the same aliased location.
func TestCanApplyAllowsAnAliasedDirectoryToGainFiles(t *testing.T) {
	local := target.Save{
		Files: []target.File{{
			Path:       "/nowhere/UNDERTALE/file0",
			LocationID: "linux1", RelativePath: "file0",
		}},
		LocationAliases: []string{"linux1", "mac1", "win1"},
	}
	expanded := omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "progress", "application/octet-stream"),
		revisionFile("mac1/undertale.ini", "settings", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, expanded); err != nil {
		t.Fatalf("expected the aliased directory to accept another file, got %v", err)
	}
	severalLocations := omnisave.Revision{ID: "revision-2", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "progress", "application/octet-stream"),
		revisionFile("win1/undertale.ini", "settings", "application/octet-stream"),
	}}
	if err := binding.CanApply(local, severalLocations); err == nil {
		t.Fatal("expected a several-location revision to refuse translation")
	}
}

// A fresh Device can materialize a lineage spelled by another OS onto its
// own destination when that destination's one location answers to the
// lineage's identity. A destination that does not exist yet cannot say
// whether it is a file or a directory, so a foreign single file under a
// name that is not the destination's own is refused rather than buried in
// a directory wearing the save file's name.
func TestCanMaterializeAcceptsAliasedSpellings(t *testing.T) {
	destination := target.SaveDestination{
		ID: "save-1", TargetID: "target-1", GameID: "game-1", Kind: "local",
		Locations: []target.SaveLocation{{
			ID: "linux1", Path: "/nowhere/UNDERTALE", Kind: target.SaveLocationUnknown,
		}},
		LocationAliases: []string{"linux1", "mac1"},
	}
	directoryShaped := omnisave.Revision{ID: "revision-1", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "content", "application/octet-stream"),
		revisionFile("mac1/undertale.ini", "settings", "application/octet-stream"),
	}}
	if err := binding.CanMaterialize(destination, directoryShaped); err != nil {
		t.Fatalf("expected the aliased directory save to be placeable, got %v", err)
	}
	oneForeignName := omnisave.Revision{ID: "revision-2", Files: []omnisave.RevisionFile{
		revisionFile("mac1/save.dat", "content", "application/octet-stream"),
	}}
	if err := binding.CanMaterialize(destination, oneForeignName); err == nil {
		t.Fatal("expected one foreign-named file into an unproven location to be refused")
	}
	ownName := omnisave.Revision{ID: "revision-3", Files: []omnisave.RevisionFile{
		revisionFile("mac1/UNDERTALE", "content", "application/octet-stream"),
	}}
	if err := binding.CanMaterialize(destination, ownName); err != nil {
		t.Fatalf("expected the destination's own name to place as its file, got %v", err)
	}
	severalLocations := omnisave.Revision{ID: "revision-4", Files: []omnisave.RevisionFile{
		revisionFile("mac1/file0", "content", "application/octet-stream"),
		revisionFile("windows9/undertale.ini", "settings", "application/octet-stream"),
	}}
	if err := binding.CanMaterialize(destination, severalLocations); err == nil {
		t.Fatal("expected a several-location revision to refuse translation")
	}
	unknown := omnisave.Revision{ID: "revision-5", Files: []omnisave.RevisionFile{
		revisionFile("windows9/file0", "content", "application/octet-stream"),
	}}
	if err := binding.CanMaterialize(destination, unknown); err == nil {
		t.Fatal("expected an unknown spelling to be refused")
	}
}
