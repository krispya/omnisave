package saveprofile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// traced resolves a profile against a macOS game rooted at home and returns
// each rule's outcome keyed by its id.
func traced(t *testing.T, home string, rules []saveprofile.Rule) map[string]saveprofile.RuleOutcome {
	t.Helper()
	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{
			Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}},
		},
		Environment: target.Environment{HostOS: saveprofile.OSMacOS, Home: home},
	}
	_, trace, err := saveprofile.ResolveWithTrace(game, saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123", Title: "Example", Rules: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]saveprofile.RuleOutcome, len(trace))
	for _, outcome := range trace {
		byID[outcome.Rule.ID] = outcome
	}
	if len(byID) != len(rules) {
		t.Fatalf("expected one outcome per rule, got %d for %d rules", len(byID), len(rules))
	}
	return byID
}

func TestTraceDistinguishesWhyEachRuleFoundNothing(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, "Library", "Application Support", "Empty")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	filled := filepath.Join(home, "Library", "Application Support", "Filled")
	if err := os.MkdirAll(filled, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filled, "progress.sav"), []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	outcomes := traced(t, home, []saveprofile.Rule{
		{ID: "found", Path: "<macAppSupport>/Filled", Kind: "save"},
		{ID: "empty", Path: "<macAppSupport>/Empty", Kind: "save"},
		{ID: "missing", Path: "<macAppSupport>/Absent", Kind: "save"},
		{ID: "windows", Path: "<winAppData>/Example", OS: saveprofile.OSWindows, Kind: "save"},
		{ID: "store", Path: "<macAppSupport>/Filled", Store: "epic", Kind: "save"},
		{ID: "relative", Path: "Example/Saves", Kind: "save"},
	})

	expected := map[string]saveprofile.Outcome{
		"found":    saveprofile.OutcomeFound,
		"empty":    saveprofile.OutcomeEmpty,
		"missing":  saveprofile.OutcomeMissing,
		"windows":  saveprofile.OutcomeInapplicable,
		"store":    saveprofile.OutcomeInapplicable,
		"relative": saveprofile.OutcomeUnexpandable,
	}
	for id, want := range expected {
		if got := outcomes[id].Outcome; got != want {
			t.Errorf("rule %q: expected outcome %q, got %q", id, want, got)
		}
	}

	// A rule discovery never tried has no path to report, and one it did
	// reach must name the absolute place it looked so a reader can go there.
	if outcomes["windows"].Path != "" {
		t.Errorf("expected an inapplicable rule to report no path, got %q", outcomes["windows"].Path)
	}
	if outcomes["missing"].Path != filepath.Join(home, "Library", "Application Support", "Absent") {
		t.Errorf("expected the searched path to be reported, got %q", outcomes["missing"].Path)
	}
	if outcomes["found"].Files != 1 || outcomes["found"].Bytes != int64(len("progress")) {
		t.Errorf("expected the found rule to count its file, got %+v", outcomes["found"])
	}
}

func TestTraceReportsAmbiguousCasingRatherThanAbsence(t *testing.T) {
	home := t.TempDir()
	support := filepath.Join(home, "Library", "Application Support")
	for _, spelling := range []string{"example", "EXAMPLE"} {
		if err := os.MkdirAll(filepath.Join(support, spelling), 0755); err != nil {
			t.Skipf("filesystem folds case, so ambiguity cannot arise: %v", err)
		}
	}
	entries, err := os.ReadDir(support)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Skip("filesystem folds case, so ambiguity cannot arise")
	}

	outcomes := traced(t, home, []saveprofile.Rule{
		{ID: "ambiguous", Path: "<macAppSupport>/Example", Kind: "save"},
	})
	if outcomes["ambiguous"].Outcome != saveprofile.OutcomeAmbiguous {
		t.Fatalf("expected ambiguous casing to be named, got %q", outcomes["ambiguous"].Outcome)
	}
}

func TestTraceReportsUnreadableLocationsRatherThanAbsence(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission modes do not make directories unreadable here")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "Locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0700) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("filesystem does not enforce the directory permission mode")
	}

	outcomes := traced(t, home, []saveprofile.Rule{
		{ID: "literal", Path: "<home>/Locked/progress.sav", Kind: "save"},
		{ID: "glob", Path: "<home>/Locked/*.sav", Kind: "save"},
	})
	for _, id := range []string{"literal", "glob"} {
		if got := outcomes[id].Outcome; got != saveprofile.OutcomeUnreadable {
			t.Errorf("rule %q: expected unreadable, got %q", id, got)
		}
	}
}

// The trace must be what discovery did, not a second opinion about it.
func TestTraceAgreesWithTheSavesItExplains(t *testing.T) {
	home := t.TempDir()
	filled := filepath.Join(home, "Library", "Application Support", "Filled")
	if err := os.MkdirAll(filled, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.sav", "b.sav"} {
		if err := os.WriteFile(filepath.Join(filled, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	game := target.InstalledGame{
		ID:          "steam:123",
		TargetID:    "steam",
		Identity:    target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{HostOS: saveprofile.OSMacOS, Home: home},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "found", Path: "<macAppSupport>/Filled", Kind: "save"}},
	}

	saves, trace, err := saveprofile.ResolveWithTrace(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != len(plain) || len(saves) != 1 || len(saves[0].Files) != len(plain[0].Files) {
		t.Fatalf("expected the traced resolve to match the plain one, got %+v and %+v", saves, plain)
	}
	if trace[0].Files != len(saves[0].Files) {
		t.Fatalf("expected the trace to count the files the save holds, got %d for %d",
			trace[0].Files, len(saves[0].Files))
	}
}
