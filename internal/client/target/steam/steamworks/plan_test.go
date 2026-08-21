package steamworks

import (
	"reflect"
	"testing"
)

// The fixture mirrors Slay the Spire 2, the measured case (FDR-005): the
// registry anchors at the account folder, live state was deregistered by an
// abandon, and the native folder carries files the game never registers.
const anchor = "/home/user/Library/Application Support/SlayTheSpire2/steam/76561198027955092"

func sts2Registry() []RegistryFile {
	return []RegistryFile{
		{Name: "profile.save", Size: 49},
		{Name: "profile1/saves/prefs.save", Size: 284},
		{Name: "profile1/saves/progress.save", Size: 214825},
		{Name: "profile1/saves/history/1778353234.run", Size: 54027},
	}
}

func TestPlanRegistersRestoredLiveState(t *testing.T) {
	placed := []string{
		anchor + "/profile.save",
		anchor + "/profile1/saves/prefs.save",
		anchor + "/profile1/saves/progress.save",
		anchor + "/profile1/saves/current_run.save",
		anchor + "/profile1/saves/history/1778353234.run",
	}
	plan, ok := PlanReconciliation(sts2Registry(), placed)
	if !ok {
		t.Fatal("expected an anchored plan")
	}
	if plan.Anchor != anchor {
		t.Fatalf("anchor = %q", plan.Anchor)
	}
	var names []string
	listed := map[string]bool{}
	for _, write := range plan.Writes {
		names = append(names, write.Name)
		listed[write.Name] = write.Listed
	}
	want := []string{
		"profile.save",
		"profile1/saves/current_run.save",
		"profile1/saves/history/1778353234.run",
		"profile1/saves/prefs.save",
		"profile1/saves/progress.save",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("writes = %v", names)
	}
	if listed["profile1/saves/current_run.save"] {
		t.Fatal("current_run.save is not in the registry and must register as new")
	}
	if !listed["profile1/saves/progress.save"] {
		t.Fatal("progress.save is in the registry and must refresh")
	}
	if len(plan.Extras) != 0 || len(plan.Ineligible) != 0 || len(plan.Outside) != 0 {
		t.Fatalf("unexpected leftovers: %+v", plan)
	}
}

func TestPlanRefusesFilesTheGameNeverRegisters(t *testing.T) {
	placed := []string{
		anchor + "/profile1/saves/progress.save",
		anchor + "/profile1/saves/progress.save.backup",
		anchor + "/profile1/replays/latest.mcr",
		anchor + "/settings.save",
	}
	plan, ok := PlanReconciliation(sts2Registry(), placed)
	if !ok {
		t.Fatal("expected an anchored plan")
	}
	var names []string
	for _, write := range plan.Writes {
		names = append(names, write.Name)
	}
	// settings.save is a root file the registry never carried — the game
	// keeps it local on purpose — and the backup twin and the replay match
	// nothing the registry has ever held.
	want := []string{"profile1/saves/progress.save"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("writes = %v", names)
	}
	wantIneligible := []string{"profile1/replays/latest.mcr", "profile1/saves/progress.save.backup", "settings.save"}
	if !reflect.DeepEqual(plan.Ineligible, wantIneligible) {
		t.Fatalf("ineligible = %v", plan.Ineligible)
	}
}

func TestPlanReportsExtrasItLeavesAlone(t *testing.T) {
	placed := []string{anchor + "/profile1/saves/progress.save"}
	plan, ok := PlanReconciliation(sts2Registry(), placed)
	if !ok {
		t.Fatal("expected an anchored plan")
	}
	want := []string{"profile.save", "profile1/saves/history/1778353234.run", "profile1/saves/prefs.save"}
	if !reflect.DeepEqual(plan.Extras, want) {
		t.Fatalf("extras = %v", plan.Extras)
	}
}

func TestPlanRefusesWithoutEvidence(t *testing.T) {
	if _, ok := PlanReconciliation(nil, []string{anchor + "/profile.save"}); ok {
		t.Fatal("an empty registry proves no anchor")
	}
	registry := []RegistryFile{{Name: "save.dat"}}
	if _, ok := PlanReconciliation(registry, []string{"/elsewhere/other.dat"}); ok {
		t.Fatal("a registry matching nothing proves no anchor")
	}
}

func TestPlanRefusesConflictingAnchors(t *testing.T) {
	registry := []RegistryFile{
		{Name: "profile.save"},
		{Name: "prefs.save"},
	}
	placed := []string{
		"/saves/a/profile.save",
		"/saves/b/prefs.save",
	}
	if _, ok := PlanReconciliation(registry, placed); ok {
		t.Fatal("names stripping to different directories prove no anchor")
	}
}

func TestPlanSkipsAmbiguousSuffixMatches(t *testing.T) {
	registry := []RegistryFile{{Name: "profile.save"}}
	placed := []string{
		"/saves/a/profile.save",
		"/saves/b/profile.save",
	}
	if _, ok := PlanReconciliation(registry, placed); ok {
		t.Fatal("a name matching two placed files nominates no anchor")
	}
}

func TestPlanKeepsRegistrySpelling(t *testing.T) {
	registry := []RegistryFile{{Name: "Profile1/Saves/Progress.save"}}
	placed := []string{anchor + "/profile1/saves/progress.save"}
	plan, ok := PlanReconciliation(registry, placed)
	if !ok {
		t.Fatal("expected an anchored plan")
	}
	if len(plan.Writes) != 1 || plan.Writes[0].Name != "Profile1/Saves/Progress.save" {
		t.Fatalf("writes = %+v", plan.Writes)
	}
}

func TestPlanReportsFilesOutsideTheAnchor(t *testing.T) {
	registry := []RegistryFile{{Name: "profile.save"}}
	placed := []string{
		anchor + "/profile.save",
		"/somewhere/else/entirely.save",
	}
	plan, ok := PlanReconciliation(registry, placed)
	if !ok {
		t.Fatal("expected an anchored plan")
	}
	if len(plan.Outside) != 1 || plan.Outside[0] != "/somewhere/else/entirely.save" {
		t.Fatalf("outside = %v", plan.Outside)
	}
}
