package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

// A Device reports what it watched a game unlock; the Dash reads the marks
// back and finds each one on a revision.
func TestADeviceReportsUnlocksAndTheyComeBackPlaced(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json",
		bytes.NewBufferString(`{"game_id":"firewatch"}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var save omnisave.Omnisave
	decodeResponse(t, response, &save)

	artifact := uploadArtifact(t, handler, "day one")
	response = request(t, handler, http.MethodPost, "/api/v1/omnisaves/"+save.ID+"/revisions",
		"application/json", bytes.NewBufferString(`{"upserts":[{"path":"save.dat","artifact":{
			"format":"`+artifact.Format+`","sha256":"`+artifact.SHA256+`","size":7}}]}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("commit returned %d: %s", response.Code, response.Body.String())
	}
	var revision omnisave.Revision
	decodeResponse(t, response, &revision)

	unlockedAt := revision.CreatedAt.Add(-time.Minute).Truncate(time.Second).UTC()
	report := `{"achievements":[{"id":"VG_DAY1","name":"Good first day.",
		"description":"Completed Day 1","unlocked_at":"` + unlockedAt.Format(time.RFC3339) + `"}]}`
	response = request(t, handler, http.MethodPost, "/api/v1/omnisaves/"+save.ID+"/achievements",
		"application/json", bytes.NewBufferString(report))
	if response.Code != http.StatusOK {
		t.Fatalf("report returned %d: %s", response.Code, response.Body.String())
	}
	var added struct {
		Achievements []omnisave.Achievement `json:"achievements"`
	}
	decodeResponse(t, response, &added)
	if len(added.Achievements) != 1 {
		t.Fatalf("expected the report to add one unlock, got %+v", added.Achievements)
	}

	// Repeating the report tells the server nothing new, and says so.
	response = request(t, handler, http.MethodPost, "/api/v1/omnisaves/"+save.ID+"/achievements",
		"application/json", bytes.NewBufferString(report))
	decodeResponse(t, response, &added)
	if response.Code != http.StatusOK || len(added.Achievements) != 0 {
		t.Fatalf("expected a repeat to add nothing, got %d: %s", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+save.ID+"/achievements", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	var marks []omnisave.Achievement
	decodeResponse(t, response, &marks)
	if len(marks) != 1 || marks[0].RevisionID == nil || *marks[0].RevisionID != revision.ID {
		t.Fatalf("expected the unlock marked on the revision, got %+v", marks)
	}
	if marks[0].Description != "Completed Day 1" || !marks[0].UnlockedAt.Equal(unlockedAt) {
		t.Fatalf("expected the reported detail kept, got %+v", marks[0])
	}
}

func TestReportingAgainstAnUnknownSaveIsNotFound(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves/no-such-save/achievements",
		"application/json", bytes.NewBufferString(
			`{"achievements":[{"id":"VG_DAY1","name":"Good first day.","unlocked_at":"2026-03-07T19:00:00Z"}]}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected not found, got %d: %s", response.Code, response.Body.String())
	}
}
