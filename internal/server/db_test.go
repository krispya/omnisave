package server

import (
	"os"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	f, err := os.CreateTemp("", "omnisave-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	db, err := NewDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAddVersion_FirstPush(t *testing.T) {
	db := newTestDB(t)
	isActive, conflict, err := db.AddVersion("snes", "zelda", "abc123", "client1", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !isActive {
		t.Error("first push should be active")
	}
	if conflict {
		t.Error("first push should not be a conflict")
	}
}

func TestAddVersion_SameParent_NoConflict(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Now()

	// First push (no parent).
	_, _, err := db.AddVersion("snes", "zelda", "sha1", "client1", "", t0)
	if err != nil {
		t.Fatal(err)
	}

	// Second push with correct parent — no conflict.
	isActive, conflict, err := db.AddVersion("snes", "zelda", "sha2", "client1", "sha1", t0.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		t.Error("same parent should not be a conflict")
	}
	if !isActive {
		t.Error("newer version should be active")
	}
}

func TestAddVersion_DivergentParent_Conflict(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Now()

	// Push v1 (no parent).
	_, _, err := db.AddVersion("snes", "zelda", "sha1", "client1", "", t0)
	if err != nil {
		t.Fatal(err)
	}

	// Push v2 from client1 — advances head.
	_, _, err = db.AddVersion("snes", "zelda", "sha2", "client1", "sha1", t0.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// Push from client2 with stale parent (sha1 instead of sha2) — conflict.
	_, conflict, err := db.AddVersion("snes", "zelda", "sha3", "client2", "sha1", t0.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !conflict {
		t.Error("diverged parent should be a conflict")
	}
}

func TestAddVersion_NewerTimestampWins(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Now()

	// Push v1.
	_, _, err := db.AddVersion("snes", "zelda", "sha1", "client1", "", t0)
	if err != nil {
		t.Fatal(err)
	}

	// Push v2 from client2 with older timestamp — v1 should stay active.
	isActive, _, err := db.AddVersion("snes", "zelda", "sha2", "client2", "sha1", t0.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if isActive {
		t.Error("older timestamp should not become active")
	}

	v, err := db.GetActiveVersion("snes", "zelda")
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.SHA256 != "sha1" {
		t.Errorf("active version should be sha1, got %v", v)
	}
}

func TestListVersions(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Now()

	db.AddVersion("snes", "zelda", "sha1", "c1", "", t0)
	db.AddVersion("snes", "zelda", "sha2", "c1", "sha1", t0.Add(time.Second))
	db.AddVersion("snes", "zelda", "sha3", "c1", "sha2", t0.Add(2*time.Second))

	versions, err := db.ListVersions("snes", "zelda")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be ordered newest first.
	if versions[0].SHA256 != "sha3" {
		t.Errorf("expected sha3 first, got %s", versions[0].SHA256)
	}
}

func TestSetActive(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Now()

	db.AddVersion("snes", "zelda", "sha1", "c1", "", t0)
	db.AddVersion("snes", "zelda", "sha2", "c1", "sha1", t0.Add(time.Second))

	// Restore sha1 as active.
	if err := db.SetActive("snes", "zelda", "sha1"); err != nil {
		t.Fatal(err)
	}

	v, err := db.GetActiveVersion("snes", "zelda")
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.SHA256 != "sha1" {
		t.Errorf("expected sha1 active, got %v", v)
	}
}
