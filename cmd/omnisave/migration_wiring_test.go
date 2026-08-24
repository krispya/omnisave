package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
		PathFormatVersion: omnisave.PathFormatMirror,
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
			writeTestJSON(t, response, omnisave.MigrationResult{
				PathFormatVersion: omnisave.PathFormatNative, Revisions: 1, Files: 1,
			})
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
	if request.ExpectedPathFormatVersion != omnisave.PathFormatMirror ||
		request.To != "battery" || request.Prefix != "" {
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
		PathFormatVersion: omnisave.PathFormatMirror,
	}
	foreign := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path:     "remote/NameThisDeviceHasNeverSeen.bin",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "00", Size: 2},
		}},
	}
	migrateAsked := false
	normalMutationAsked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Revision{foreign})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			normalMutationAsked = true
			http.Error(response, "legacy lineage was mutated", http.StatusConflict)
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
	if normalMutationAsked || outcome.Failed != 0 {
		t.Fatalf("a held lineage entered normal sync: mutation=%v outcome=%+v",
			normalMutationAsked, outcome)
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "not migrated — this device's save gives no evidence") {
		t.Fatalf("expected a held sentence in the report:\n%s", lines)
	}
}

// A server that reports no path-format version at all — one that predates
// the field, or a recovery that has not classified the lineage yet — holds
// the lineage outright. Only a persisted version may admit ordinary sync or
// select a migration; the client never substitutes its own classification.
func TestALineageIsHeldWhenTheServerReportsNoPathFormat(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	digest := sha256.Sum256(fixture.content)
	contentHash := hex.EncodeToString(digest[:])
	currentID := "revision-1"
	// No PathFormatVersion: decoded from an old server's JSON it is zero.
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
	migrateAsked := false
	normalMutationAsked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Revision{mirror})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			normalMutationAsked = true
			http.Error(response, "legacy lineage was mutated", http.StatusConflict)
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
		t.Fatal("an unpersisted version must not select a migration")
	}
	if normalMutationAsked || outcome.Failed != 0 {
		t.Fatalf("a version-less lineage entered normal sync: mutation=%v outcome=%+v",
			normalMutationAsked, outcome)
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "not migrated — the server does not report the lineage's path format") {
		t.Fatalf("expected the unreported-version hold in the report:\n%s", lines)
	}
}

// The server's structured refusal reason reaches the report, so the user
// can tell a permanent hold apart from a transient one. A refusal is also
// never remembered: it can heal without the save or the history changing.
func TestARefusedMigrationReportsTheServersReason(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	digest := sha256.Sum256(fixture.content)
	contentHash := hex.EncodeToString(digest[:])
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
		PathFormatVersion: omnisave.PathFormatMirror,
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
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Revision{mirror})
		case strings.HasSuffix(r.URL.Path, "/migrate-locations"):
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusConflict)
			response.Write([]byte(`{"error":"migration_refused","status":409,"reason":"fork_family"}`))
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
	if outcome.Failed != 0 {
		t.Fatalf("a refused migration is a hold, not a failure: %+v", outcome)
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "not migrated — the lineage shares history with a fork") {
		t.Fatalf("expected the refusal reason in the report:\n%s", lines)
	}
	if len(fixture.state.HeldProofs) != 0 {
		t.Fatalf("server refusals must not be remembered as proof verdicts: %+v", fixture.state.HeldProofs)
	}
}

// A failed proof is a function of the save's files and the lineage's
// history. While neither changes, later passes hold the lineage from the
// recorded verdict instead of re-reading and re-hashing the save to reach
// the same answer — proven here by making the save unreadable: the second
// pass still reports the recorded cause because it never opens the files.
func TestAFailedProofIsRememberedAcrossPasses(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
		PathFormatVersion: omnisave.PathFormatMirror,
	}
	foreign := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path:     "remote/NameThisDeviceHasNeverSeen.bin",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "00", Size: 2},
		}},
	}
	historyFetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			historyFetches++
			writeTestJSON(t, response, []omnisave.Revision{foreign})
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
	for pass := 1; pass <= 2; pass++ {
		outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
		report := &tui.TrackReport{}
		err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
			map[string]bool{"local-game-1": true}, &outcome, report, nil, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Join(report.Lines(), "\n")
		if !strings.Contains(lines, "not migrated — this device's save gives no evidence") {
			t.Fatalf("pass %d lost the held sentence:\n%s", pass, lines)
		}
		if pass == 1 {
			if len(fixture.state.HeldProofs) != 1 {
				t.Fatalf("the failed proof was not recorded: %+v", fixture.state.HeldProofs)
			}
			// An unreadable save file changes neither its stat summary nor
			// the history, so the memo must answer without opening it; a
			// pass that re-read the save would hold with a read error
			// instead of the recorded cause.
			if err := os.Chmod(fixture.localPath, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(fixture.localPath, 0o600) })
		}
	}
	if historyFetches != 2 {
		t.Fatalf("history fetched %d times; each pass verifies the history the verdict is pinned to", historyFetches)
	}
}

// A game whose only lineage is held must never silently seed a second one:
// a new lineage can never rejoin the held history once it migrates, so the
// split is asked, and an unanswered or declined question just waits.
func TestAHeldOnlyGameNeverSeedsSilently(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
		PathFormatVersion: omnisave.PathFormatMirror,
	}
	foreign := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path:     "remote/NameThisDeviceHasNeverSeen.bin",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "00", Size: 2},
		}},
	}
	seedAsked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodPost:
			seedAsked = true
			http.Error(response, "seeding is not part of this story", http.StatusInternalServerError)
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Revision{foreign})
		default:
			http.NotFound(response, r)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	// Unbound: the pass has to decide what this local save belongs to.
	answered := false
	prompts := &reconcilePrompts{
		ambiguous: func(string, []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
			t.Fatal("the ambiguous question has no options to offer here")
			return tui.AmbiguousBindingChoice{}, nil
		},
		heldSeed: func(string) (bool, error) {
			answered = true
			return false, nil
		},
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, prompts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !answered {
		t.Fatal("the held-lineage question was never asked")
	}
	if seedAsked {
		t.Fatal("declining the question must not seed a new lineage")
	}
	if outcome.Unbound != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}

	// Answering "create" is what seeds — explicitly, never silently.
	prompts.heldSeed = func(string) (bool, error) { return true, nil }
	outcome = tui.TrackOutcome{Tracked: 1, Synced: true}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, prompts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !seedAsked {
		t.Fatal("an accepted question should reach the server to seed")
	}
}

// A held lineage is not an adoption candidate. Its history cannot be
// restored until it migrates, and binding to it would only earn refusals on
// the next commit, so the choice a device is offered must leave it out —
// even when its retired paths would happen to land in this save's layout,
// which an alias makes true here.
func TestAHeldLineageIsNeverOfferedForAdoption(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	// The save answers to the retired spelling too, so the layout check
	// alone would accept the held lineage's paths. Exclusion has to come
	// from the hold itself.
	fixture.save.LocationAliases = []string{omnisave.MirrorLocation}
	fixture.scans[0].Games[0].Saves[0].LocationAliases = []string{omnisave.MirrorLocation}

	heldCurrent, nativeCurrent := "revision-1", "revision-2"
	held := omnisave.Omnisave{
		ID: "omnisave-held", GameID: "server-game-1", DisplayName: "Mirror",
		CurrentRevisionID: &heldCurrent, PathFormatVersion: omnisave.PathFormatMirror,
	}
	native := omnisave.Omnisave{
		ID: "omnisave-native", GameID: "server-game-1", DisplayName: "Main",
		CurrentRevisionID: &nativeCurrent, PathFormatVersion: omnisave.PathFormatNative,
	}
	// Neither lineage holds this device's content, so both would be offered
	// on their layout alone; only the held one must be withheld.
	heldHistory := omnisave.Revision{
		ID: heldCurrent, OmnisaveID: held.ID,
		Files: []omnisave.RevisionFile{{
			Path:     omnisave.MirrorLocation + "/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "aa", Size: 2},
		}},
	}
	nativeHistory := omnisave.Revision{
		ID: nativeCurrent, OmnisaveID: native.ID,
		Files: []omnisave.RevisionFile{{
			Path:     "battery/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "bb", Size: 2},
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{held, native})
		case "/api/v1/omnisaves/omnisave-held/revisions":
			writeTestJSON(t, response, []omnisave.Revision{heldHistory})
		case "/api/v1/omnisaves/omnisave-native/revisions":
			writeTestJSON(t, response, []omnisave.Revision{nativeHistory})
		default:
			http.NotFound(response, r)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	var offered []tui.AmbiguousBindingOption
	// The question itself is the assertion, so the answer ends the pass
	// rather than sending it down an adoption this story does not serve.
	errOffered := errors.New("options captured")
	prompts := &reconcilePrompts{
		ambiguous: func(_ string, options []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
			offered = options
			return tui.AmbiguousBindingChoice{}, errOffered
		},
		heldSeed: func(string) (bool, error) {
			t.Fatal("an adoptable lineage remains, so the seed question is not this story")
			return false, nil
		},
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, prompts, nil, 0)
	if !errors.Is(err, errOffered) {
		t.Fatalf("expected the captured answer to end the pass, got %v", err)
	}
	if len(offered) != 1 || offered[0].OmnisaveID != native.ID {
		t.Fatalf("offered = %+v; only the native lineage is adoptable", offered)
	}
	if outcome.Held != 1 {
		t.Fatalf("the held lineage should be tallied as held, got %+v", outcome)
	}
}

// A proof's verdict depends on whether the save is already bound to the
// lineage — an unbound save must match a complete revision, a bound one
// need not — so binding expires a remembered hold even though neither the
// save nor the history moved.
func TestBindingExpiresARememberedHold(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &currentID,
		PathFormatVersion: omnisave.PathFormatMirror,
	}
	// The name proves the mapping, but the content does not agree, so an
	// unbound save cannot claim the lineage.
	mirror := omnisave.Revision{
		ID: currentID, OmnisaveID: remoteSave.ID,
		Files: []omnisave.RevisionFile{{
			Path:     omnisave.MirrorLocation + "/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "aa", Size: 2},
		}},
	}
	var mutex sync.Mutex
	migrations := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/migrate-locations":
			migrations++
			writeTestJSON(t, response, omnisave.MigrationResult{
				PathFormatVersion: omnisave.PathFormatNative, Revisions: 1, Files: 1,
			})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Revision{mirror})
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

	// Unbound: held, and the verdict is remembered.
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	report := &tui.TrackReport{}
	prompts := &reconcilePrompts{heldSeed: func(string) (bool, error) { return false, nil }}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, report, prompts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if migrations != 0 {
		t.Fatal("an unbound save that matches no revision must not migrate the lineage")
	}
	recorded, remembered := fixture.state.HeldProofFor(local, remoteSave.ID)
	if !remembered || recorded.Bound {
		t.Fatalf("held proof = %+v", recorded)
	}

	// Bound to that same lineage, nothing else changed: the recorded hold
	// no longer describes the question being asked, so it is proven again.
	if err := fixture.state.Bind(local, remoteSave.ID); err != nil {
		t.Fatal(err)
	}
	outcome = tui.TrackOutcome{Tracked: 1, Synced: true}
	err = reconcileSaves(context.Background(), nil, remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, prompts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("binding should have retried the proof; migrations = %d", migrations)
	}
	if _, stillHeld := fixture.state.HeldProofFor(local, remoteSave.ID); stillHeld {
		t.Fatal("a migrated lineage must not keep a recorded hold")
	}
}
