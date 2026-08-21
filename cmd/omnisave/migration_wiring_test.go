package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// A lineage minted under the retired mirror vocabulary cannot bind, verify,
// or restore until it is renamed into the save's own vocabulary. When this
// device's save proves the mapping, the pass migrates the lineage on the
// server and continues with the rewritten history — here all the way to a
// rebind, since the local content equals the migrated current revision.
func TestAMirrorLineageMigratesAndRebindsInOnePass(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	digest := sha256.Sum256(fixture.content)
	contentHash := hex.EncodeToString(digest[:])
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
	}
	mirror := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path: "remote/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{
				Format: "application/octet-stream", SHA256: contentHash, Size: int64(len(fixture.content)),
			},
		}},
	}

	var mutex sync.Mutex
	migrated := false
	var request omnisave.MigrateLocations
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/migrate-locations":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			migrated = true
			writeTestJSON(t, response, omnisave.MigrationResult{Revisions: 1, Files: 1})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			history := mirror
			if migrated {
				history.Files = []omnisave.RevisionFile{{
					Path: request.To + "/Chrono Trigger.srm", Artifact: mirror.Files[0].Artifact,
				}}
			}
			writeTestJSON(t, response, []omnisave.Revision{history})
		default:
			http.NotFound(response, r)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	// Bound to the mirror lineage with no baseline — the shape a device is
	// left in when its old mirror representation retired underneath it.
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, remoteSave.ID); err != nil {
		t.Fatal(err)
	}

	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("the pass never asked the server to migrate")
	}
	if request.From != "remote" || request.To != "battery" || request.Prefix != "" {
		t.Fatalf("migration request = %+v", request)
	}
	if outcome.Rebound != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the migrated lineage to rebind, got %+v", outcome)
	}
	bound, isBound := fixture.state.BindingFor(local)
	if !isBound || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != currentID {
		t.Fatalf("binding = %+v", bound)
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "migrated to the game's own save location") {
		t.Fatalf("expected a migration sentence in the report:\n%s", lines)
	}
}

// A lineage whose names this device's save cannot place stays unmigrated
// and says so — restorable history must never look in good standing when it
// is not restorable.
func TestAnUnprovableMirrorLineageIsReportedHeld(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
	}
	foreign := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path:     "remote/NameThisDeviceHasNeverSeen.bin",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "00", Size: 2},
		}},
	}
	migrateAsked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{foreign})
		case strings.HasSuffix(r.URL.Path, "/migrate-locations"):
			migrateAsked = true
			http.NotFound(response, r)
		default:
			http.NotFound(response, r)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, remoteSave.ID); err != nil {
		t.Fatal(err)
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if migrateAsked {
		t.Fatal("an unproven mapping must never reach the server")
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "not migrated — this device's save gives no evidence") {
		t.Fatalf("expected a held sentence in the report:\n%s", lines)
	}
}
