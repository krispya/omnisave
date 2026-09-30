package steamworks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeRegistry struct {
	files   map[string][]byte
	writes  []string
	deletes []string
	refuse  map[string]bool
}

func (f *fakeRegistry) Registry() []RegistryFile {
	var listed []RegistryFile
	for name, content := range f.files {
		listed = append(listed, RegistryFile{Name: name, Size: int64(len(content))})
	}
	return listed
}

func (f *fakeRegistry) Holds(name string, content []byte) bool {
	held, exists := f.files[name]
	return exists && bytes.Equal(held, content)
}

func (f *fakeRegistry) WriteFile(name string, content []byte) error {
	if f.refuse[name] {
		return fmt.Errorf("quota exceeded")
	}
	f.files[name] = append([]byte(nil), content...)
	f.writes = append(f.writes, name)
	return nil
}

func (f *fakeRegistry) DeleteFile(name string) error {
	if f.refuse[name] {
		return fmt.Errorf("connectivity lost")
	}
	delete(f.files, name)
	f.deletes = append(f.deletes, name)
	return nil
}

func placeFiles(t *testing.T, root string, files map[string]string) []string {
	t.Helper()
	var placed []string
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		placed = append(placed, full)
	}
	return placed
}

func TestReconcileRegistersAndRefreshes(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save":                    "rewound profile",
		"profile1/saves/progress.save":    "rewound progress",
		"profile1/saves/current_run.save": "rewound run",
		"profile1/saves/prefs.save":       "same prefs",
	})
	store := &fakeRegistry{files: map[string][]byte{
		"profile.save":                 []byte("newer profile"),
		"profile1/saves/progress.save": []byte("newer progress"),
		"profile1/saves/prefs.save":    []byte("same prefs"),
	}}
	result := Reconcile(store, Request{Files: placed})
	if result.Skipped != "" {
		t.Fatalf("skipped: %s", result.Skipped)
	}
	wantWritten := []string{"profile.save", "profile1/saves/current_run.save", "profile1/saves/progress.save"}
	if !reflect.DeepEqual(result.Written, wantWritten) {
		t.Fatalf("written = %v", result.Written)
	}
	if !reflect.DeepEqual(result.Unchanged, []string{"profile1/saves/prefs.save"}) {
		t.Fatalf("unchanged = %v", result.Unchanged)
	}
	if string(store.files["profile1/saves/current_run.save"]) != "rewound run" {
		t.Fatal("live state did not reach the registry")
	}
}

func TestReconcileDeletesEntriesForRemovedFiles(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save":                 "rewound profile",
		"profile1/saves/progress.save": "rewound progress",
	})
	store := &fakeRegistry{files: map[string][]byte{
		"profile.save":                          []byte("rewound profile"),
		"profile1/saves/progress.save":          []byte("rewound progress"),
		"profile1/saves/history/1784516859.run": []byte("premature archive"),
		"profile1/saves/history/kept-extra.run": []byte("no removal vouches"),
	}}
	removed := []string{filepath.Join(root, "profile1", "saves", "history", "1784516859.run")}
	result := Reconcile(store, Request{Files: placed, Removed: removed})
	if result.Skipped != "" {
		t.Fatalf("skipped: %s", result.Skipped)
	}
	if !reflect.DeepEqual(result.Deleted, []string{"profile1/saves/history/1784516859.run"}) {
		t.Fatalf("deleted = %v", result.Deleted)
	}
	if !reflect.DeepEqual(store.deletes, []string{"profile1/saves/history/1784516859.run"}) {
		t.Fatalf("store deletes = %v", store.deletes)
	}
	if !reflect.DeepEqual(result.Extras, []string{"profile1/saves/history/kept-extra.run"}) {
		t.Fatalf("extras = %v", result.Extras)
	}
	if _, exists := store.files["profile1/saves/history/kept-extra.run"]; !exists {
		t.Fatal("an unvouched extra must stay in the registry")
	}
}

func TestReconcileReportsRefusedDeletes(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save": "same profile",
	})
	store := &fakeRegistry{
		files: map[string][]byte{
			"profile.save":     []byte("same profile"),
			"stale-extra.save": []byte("stale"),
		},
		refuse: map[string]bool{"stale-extra.save": true},
	}
	removed := []string{filepath.Join(root, "stale-extra.save")}
	result := Reconcile(store, Request{Files: placed, Removed: removed})
	if len(result.Failed) != 1 || result.Failed[0].Name != "stale-extra.save" {
		t.Fatalf("failed = %+v", result.Failed)
	}
	if len(result.Deleted) != 0 {
		t.Fatalf("deleted = %v", result.Deleted)
	}
}

func TestReconcileDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save": "rewound profile",
	})
	store := &fakeRegistry{files: map[string][]byte{
		"profile.save": []byte("newer profile"),
		"stale.save":   []byte("stale"),
	}}
	removed := []string{filepath.Join(root, "stale.save")}
	result := Reconcile(store, Request{Files: placed, Removed: removed, DryRun: true})
	if !reflect.DeepEqual(result.Written, []string{"profile.save"}) {
		t.Fatalf("written = %v", result.Written)
	}
	if !reflect.DeepEqual(result.Deleted, []string{"stale.save"}) {
		t.Fatalf("deleted = %v", result.Deleted)
	}
	if len(store.writes) != 0 {
		t.Fatalf("dry run wrote: %v", store.writes)
	}
	if len(store.deletes) != 0 {
		t.Fatalf("dry run deleted: %v", store.deletes)
	}
}

func TestReconcileReportsRefusedWrites(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save": "rewound profile",
	})
	store := &fakeRegistry{
		files:  map[string][]byte{"profile.save": []byte("newer profile")},
		refuse: map[string]bool{"profile.save": true},
	}
	result := Reconcile(store, Request{Files: placed})
	if len(result.Failed) != 1 || result.Failed[0].Name != "profile.save" {
		t.Fatalf("failed = %+v", result.Failed)
	}
	if len(result.Written) != 0 {
		t.Fatalf("written = %v", result.Written)
	}
}

func TestReconcileSkipsWithoutAnchor(t *testing.T) {
	store := &fakeRegistry{files: map[string][]byte{}}
	result := Reconcile(store, Request{Files: []string{"/nowhere/file.save"}})
	if result.Skipped == "" {
		t.Fatal("expected a skip report")
	}
	if len(store.writes) != 0 {
		t.Fatalf("wrote despite no anchor: %v", store.writes)
	}
}

func TestFindLibrary(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "Game.app", "Contents", "Resources", "data")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range libraryNames() {
		if err := os.WriteFile(filepath.Join(deep, name), []byte("lib"), 0o644); err != nil {
			t.Fatal(err)
		}
		break
	}
	found, err := FindLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(found) != deep {
		t.Fatalf("found = %s", found)
	}
	if _, err := FindLibrary(t.TempDir()); err == nil {
		t.Fatal("expected an error for a game without the library")
	}
}

// A failed replacement must not retire the old cloud state. Once writes can
// succeed, replaying the same request can finish both halves of the restore.
func TestReconcileKeepsRemovalsUntilWritesSucceed(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{"profile.save": "restored"})
	store := &fakeRegistry{
		files: map[string][]byte{
			"profile.save":         []byte("newer"),
			"history/finished.run": []byte("finished"),
		},
		refuse: map[string]bool{"profile.save": true},
	}
	request := Request{Files: placed, Removed: []string{filepath.Join(root, "history/finished.run")}}

	failed := Reconcile(store, request)
	if len(failed.Failed) != 1 || len(store.deletes) != 0 {
		t.Fatalf("a failed write must leave cloud history intact: failures=%v deletions=%v", failed.Failed, store.deletes)
	}
	if !reflect.DeepEqual(failed.Extras, []string{"history/finished.run"}) {
		t.Fatalf("deferred removals must remain visible: %v", failed.Extras)
	}
	store.refuse = nil
	completed := Reconcile(store, request)
	if len(completed.Failed) != 0 || !reflect.DeepEqual(completed.Deleted, []string{"history/finished.run"}) {
		t.Fatalf("retry did not finish the restore: %+v", completed)
	}
}
