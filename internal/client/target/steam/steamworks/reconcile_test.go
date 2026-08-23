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
	files  map[string][]byte
	writes []string
	refuse map[string]bool
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

func TestReconcileDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	placed := placeFiles(t, root, map[string]string{
		"profile.save": "rewound profile",
	})
	store := &fakeRegistry{files: map[string][]byte{
		"profile.save": []byte("newer profile"),
	}}
	result := Reconcile(store, Request{Files: placed, DryRun: true})
	if !reflect.DeepEqual(result.Written, []string{"profile.save"}) {
		t.Fatalf("written = %v", result.Written)
	}
	if len(store.writes) != 0 {
		t.Fatalf("dry run wrote: %v", store.writes)
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
