package steamworks

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func slotScope(root string) *target.CloudScope {
	return &target.CloudScope{Root: root, Prefix: "profile2", AccountID: "fixture", Files: []string{"saves/progress.save", "saves/current_run.save"}, Directories: []target.CloudDirectory{{Path: "saves/history", Extension: ".run"}}}
}

func TestSlotCloudPlacementWorksWithoutRegistryPrecedent(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile2/saves/progress.save": "progress", "profile2/saves/current_run.save": "run", "profile2/saves/current_run.save.backup": "backup", "profile2/saves/history/finished.run": "history"})
	cloud := &fakeRegistry{files: map[string][]byte{"profile.save": []byte("selector"), "profile1/saves/progress.save": []byte("sibling")}}
	before := cloudBefore(cloud, root)
	result := Reconcile(cloud, Request{Files: placed, Scope: slotScope(root)})
	if result.Skipped != "" || len(result.Failed) > 0 || len(result.Extras) > 0 || len(result.Written) != 3 || len(result.Ineligible) != 1 {
		t.Fatalf("fresh slot did not register: %+v", result)
	}
	if !reflect.DeepEqual(cloudBefore(cloud, root)[filepath.Join(root, "profile1/saves/progress.save")], before[filepath.Join(root, "profile1/saves/progress.save")]) || string(cloud.files["profile.save"]) != "selector" {
		t.Fatal("placement touched shared or sibling cloud files")
	}
	// The same contract also admits a completely empty account registry.
	cloud = &fakeRegistry{files: map[string][]byte{}}
	result = Reconcile(cloud, Request{Files: placed, Scope: slotScope(root)})
	if result.Skipped != "" || len(result.Written) != 3 {
		t.Fatalf("empty registry was not supported: %+v", result)
	}
}

func TestSlotCloudRewindDeletesOnlyPreservedOwnedFiles(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile2/saves/progress.save": "older", "profile2/saves/current_run.save": "restored run"})
	cloud := &fakeRegistry{files: map[string][]byte{"profile.save": []byte("selector"), "profile1/saves/history/finished.run": []byte("sibling"), "profile2/saves/progress.save": []byte("newer"), "profile2/saves/history/finished.run": []byte("terminal")}}
	before := cloudBefore(cloud, root)
	removed := filepath.Join(root, "profile2/saves/history/finished.run")
	result := Reconcile(cloud, Request{Files: placed, Removed: []string{removed}, Before: before, Scope: slotScope(root)})
	if len(result.Failed) > 0 || len(result.Extras) > 0 || !reflect.DeepEqual(result.Deleted, []string{"profile2/saves/history/finished.run"}) {
		t.Fatalf("scoped rewind failed: %+v", result)
	}
	if !cloud.Exists("profile1/saves/history/finished.run") || !cloud.Exists("profile.save") {
		t.Fatal("rewind deleted unrelated registry entries")
	}
	// An extra inside the selected slot still blocks completion and survives;
	// a scope is never permission to delete unknown progress.
	cloud.files["profile2/saves/history/unknown.run"] = []byte("unpreserved")
	result = Reconcile(cloud, Request{Files: placed, Scope: slotScope(root)})
	if !reflect.DeepEqual(result.Extras, []string{"profile2/saves/history/unknown.run"}) || !cloud.Exists("profile2/saves/history/unknown.run") {
		t.Fatal("unknown owned cloud content was lost")
	}
}

func TestSlotCloudPlanRefusesFilesOutsideOwnership(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile1/saves/progress.save": "sibling", "profile.save": "selector", "profile2/saves/history/not-a-run.save": "not cloud history"})
	plan, ok := PlanScopedReconciliation(nil, placed, nil, *slotScope(root))
	if !ok || len(plan.Outside) != 2 || len(plan.Ineligible) != 1 || len(plan.Writes) != 0 {
		t.Fatalf("scope admitted unrelated content: %+v", plan)
	}
	scope := slotScope(root)
	scope.Prefix = "../profile1"
	if _, ok := PlanScopedReconciliation(nil, placed, nil, *scope); ok {
		t.Fatal("unsafe cloud namespace was accepted")
	}
}

func TestSlotCloudPlacementRefusesAnotherConnectedAccount(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile2/saves/progress.save": "restored"})
	cloud := &fakeRegistry{accountID: "another-fixture-account", files: map[string][]byte{"profile2/saves/progress.save": []byte("other account progress")}}
	result := Reconcile(cloud, Request{Files: placed, Scope: slotScope(root)})
	if result.Skipped == "" || len(cloud.writes) > 0 || len(cloud.deletes) > 0 || string(cloud.files["profile2/saves/progress.save"]) != "other account progress" {
		t.Fatal("slot restore mutated another connected account")
	}
}

// Steam names can differ in case from local paths. An existing entry keeps
// the registry's spelling, so a rewind neither duplicates nor strands it.
func TestSlotCloudRewindMatchesRegistryNamesInAnyCase(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile2/saves/progress.save": "older"})
	cloud := &fakeRegistry{files: map[string][]byte{"Profile2/Saves/Progress.save": []byte("newer"), "Profile2/Saves/History/Finished.run": []byte("terminal")}}
	progressDigest, _ := cloud.Digest("Profile2/Saves/Progress.save")
	historyDigest, _ := cloud.Digest("Profile2/Saves/History/Finished.run")
	removed := filepath.Join(root, "profile2/saves/history/finished.run")
	before := map[string]string{placed[0]: progressDigest, removed: historyDigest}
	result := Reconcile(cloud, Request{Files: placed, Removed: []string{removed}, Before: before, Scope: slotScope(root)})
	if len(result.Failed) > 0 || len(result.Extras) > 0 || !reflect.DeepEqual(result.Written, []string{"Profile2/Saves/Progress.save"}) || !reflect.DeepEqual(result.Deleted, []string{"Profile2/Saves/History/Finished.run"}) {
		t.Fatalf("registry casing split the slot: %+v", result)
	}
	if len(cloud.files) != 1 || string(cloud.files["Profile2/Saves/Progress.save"]) != "older" {
		t.Fatalf("expected one entry in the registry's spelling, got %v", cloud.files)
	}
}
