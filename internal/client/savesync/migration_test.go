package savesync_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// mirrorLineage is a lineage minted under the retired mirror vocabulary,
// whose one revision is revision.
func mirrorLineage(revision omnisave.Revision) omnisave.Omnisave {
	return omnisave.Omnisave{
		ID: revision.OmnisaveID, GameID: "server-game-1", DisplayName: "Main", CurrentRevisionID: &revision.ID,
		PathFormatVersion: omnisave.PathFormatMirror,
	}
}

// mirrorRevision holds content at the mirror spelling of the fixture's file.
func mirrorRevision(content string) omnisave.Revision {
	return omnisave.Revision{
		ID: "revision-1", OmnisaveID: "omnisave-1",
		Files: []omnisave.RevisionFile{{Path: "remote/Chrono Trigger.srm", Artifact: artifactOf(content)}},
	}
}

// foreignRevision holds a file this Device's save has never heard of, which
// can prove no mapping.
func foreignRevision() omnisave.Revision {
	return omnisave.Revision{
		ID: "revision-1", OmnisaveID: "omnisave-1",
		Files: []omnisave.RevisionFile{{
			Path:     "remote/NameThisDeviceHasNeverSeen.bin",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "00", Size: 2},
		}},
	}
}

// heldFor is the reason the one lineage a pass held was held for.
func heldFor(t *testing.T, report *savesynctest.Recorder) error {
	t.Helper()
	held := report.Of("MigrationHeld")
	if len(held) != 1 {
		t.Fatalf("expected one held lineage, got %+v", report.Events)
	}
	return held[0].Err
}

// A lineage minted under the retired mirror vocabulary cannot bind, verify,
// or restore until it is renamed into the save's own vocabulary. When this
// device's save proves the mapping, the pass migrates the lineage on the
// server and continues with the rewritten history — here all the way to a
// rebind, since the local content equals the migrated current revision.
func TestAMirrorLineageMigratesAndRebindsInOnePass(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	mirror := mirrorRevision("saved-game-content")
	remoteSave := mirrorLineage(mirror)
	var mutex sync.Mutex
	migrated := false
	var request omnisave.MigrateLocations
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/migrate-locations":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			migrated = true
			writeJSON(t, response, omnisave.MigrationResult{
				PathFormatVersion: omnisave.PathFormatNative, Revisions: 1, Files: 1,
			})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			history := mirror
			if migrated {
				history.Files = []omnisave.RevisionFile{{
					Path: request.To + "/Chrono Trigger.srm", Artifact: mirror.Files[0].Artifact,
				}}
			}
			writeJSON(t, response, []omnisave.Revision{history})
		default:
			http.NotFound(response, r)
		}
	})
	// Bound to the mirror lineage with no baseline — the shape a device is
	// left in when its old mirror representation retired underneath it.
	if err := fixture.State.Bind(fixture.Local(), remoteSave.ID); err != nil {
		t.Fatal(err)
	}

	outcome, report := reconcile(t, server, &fixture, savesync.Options{})

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
	assertBinding(t, &fixture, remoteSave.ID, mirror.ID)
	if migrations := report.Of("Migrated"); len(migrations) != 1 || migrations[0].Omnisave != "Main" {
		t.Fatalf("expected the migration reported, got %+v", report.Events)
	}
}

// A lineage whose names this device's save cannot place stays unmigrated
// and says so — restorable history must never look in good standing when it
// is not restorable.
func TestAnUnprovableMirrorLineageIsReportedHeld(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	foreign := foreignRevision()
	migrateAsked := false
	normalMutationAsked := false
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{mirrorLineage(foreign)})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Revision{foreign})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			normalMutationAsked = true
			http.Error(response, "legacy lineage was mutated", http.StatusConflict)
		case strings.HasSuffix(r.URL.Path, "/migrate-locations"):
			migrateAsked = true
			http.NotFound(response, r)
		default:
			http.NotFound(response, r)
		}
	})
	if err := fixture.State.Bind(fixture.Local(), "omnisave-1"); err != nil {
		t.Fatal(err)
	}

	outcome, report := reconcile(t, server, &fixture, savesync.Options{})

	if migrateAsked {
		t.Fatal("an unproven mapping must never reach the server")
	}
	if normalMutationAsked || outcome.Failed != 0 {
		t.Fatalf("a held lineage entered normal sync: mutation=%v outcome=%+v",
			normalMutationAsked, outcome)
	}
	if reason := heldFor(t, report); !errors.Is(reason, savesync.HoldNoMappingEvidence) {
		t.Fatalf("expected the lineage held for want of evidence, got %v", reason)
	}
}

// A server that reports no path-format version at all — one that predates
// the field, or a recovery that has not classified the lineage yet — holds
// the lineage outright. Only a persisted version may admit ordinary sync or
// select a migration; the client never substitutes its own classification.
func TestALineageIsHeldWhenTheServerReportsNoPathFormat(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	mirror := mirrorRevision("saved-game-content")
	// No PathFormatVersion: decoded from an old server's JSON it is zero.
	remoteSave := mirrorLineage(mirror)
	remoteSave.PathFormatVersion = omnisave.PathFormatUnclassified
	migrateAsked := false
	normalMutationAsked := false
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Revision{mirror})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			normalMutationAsked = true
			http.Error(response, "legacy lineage was mutated", http.StatusConflict)
		case strings.HasSuffix(r.URL.Path, "/migrate-locations"):
			migrateAsked = true
			http.NotFound(response, r)
		default:
			http.NotFound(response, r)
		}
	})
	if err := fixture.State.Bind(fixture.Local(), remoteSave.ID); err != nil {
		t.Fatal(err)
	}

	outcome, report := reconcile(t, server, &fixture, savesync.Options{})

	if migrateAsked {
		t.Fatal("an unpersisted version must not select a migration")
	}
	if normalMutationAsked || outcome.Failed != 0 {
		t.Fatalf("a version-less lineage entered normal sync: mutation=%v outcome=%+v",
			normalMutationAsked, outcome)
	}
	if reason := heldFor(t, report); !errors.Is(reason, savesync.HoldUnreportedFormat) {
		t.Fatalf("expected the unreported-version hold, got %v", reason)
	}
}

// The server's structured refusal reason reaches the report, so the user
// can tell a permanent hold apart from a transient one. A refusal is also
// never remembered: it can heal without the save or the history changing.
func TestARefusedMigrationReportsTheServersReason(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	mirror := mirrorRevision("saved-game-content")
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{mirrorLineage(mirror)})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Revision{mirror})
		case strings.HasSuffix(r.URL.Path, "/migrate-locations"):
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusConflict)
			response.Write([]byte(`{"error":"migration_refused","status":409,"reason":"fork_family"}`))
		default:
			http.NotFound(response, r)
		}
	})
	if err := fixture.State.Bind(fixture.Local(), "omnisave-1"); err != nil {
		t.Fatal(err)
	}

	outcome, report := reconcile(t, server, &fixture, savesync.Options{})

	if outcome.Failed != 0 {
		t.Fatalf("a refused migration is a hold, not a failure: %+v", outcome)
	}
	var refused *omnisave.MigrationRefused
	if reason := heldFor(t, report); !errors.As(reason, &refused) || refused.Reason != omnisave.MigrationRefusedForkFamily {
		t.Fatalf("expected the server's refusal reason, got %v", reason)
	}
	if len(fixture.State.HeldProofs) != 0 {
		t.Fatalf("server refusals must not be remembered as proof verdicts: %+v", fixture.State.HeldProofs)
	}
}

// A failed proof is a function of the save's files and the lineage's
// history. While neither changes, later passes hold the lineage from the
// recorded verdict instead of re-reading and re-hashing the save to reach
// the same answer — proven here by making the save unreadable: the second
// pass still reports the recorded reason because it never opens the files.
func TestAFailedProofIsRememberedAcrossPasses(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	foreign := foreignRevision()
	historyFetches := 0
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{mirrorLineage(foreign)})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			historyFetches++
			writeJSON(t, response, []omnisave.Revision{foreign})
		default:
			http.NotFound(response, r)
		}
	})
	if err := fixture.State.Bind(fixture.Local(), "omnisave-1"); err != nil {
		t.Fatal(err)
	}

	_, report := reconcile(t, server, &fixture, savesync.Options{})
	if reason := heldFor(t, report); !errors.Is(reason, savesync.HoldNoMappingEvidence) {
		t.Fatalf("expected the lineage held for want of evidence, got %v", reason)
	}
	if len(fixture.State.HeldProofs) != 1 {
		t.Fatalf("the failed proof was not recorded: %+v", fixture.State.HeldProofs)
	}

	// An unreadable save file changes neither its stat summary nor the
	// history, so the memo must answer without opening it; a pass that
	// re-read the save would hold with a read error instead of the recorded
	// reason.
	if err := os.Chmod(fixture.LocalPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(fixture.LocalPath, 0o600) })
	_, report = reconcile(t, server, &fixture, savesync.Options{})
	if reason := heldFor(t, report); !errors.Is(reason, savesync.HoldNoMappingEvidence) {
		t.Fatalf("expected the second pass to hold from the recorded verdict, got %v", reason)
	}
	if historyFetches != 2 {
		t.Fatalf("history fetched %d times; each pass verifies the history the verdict is pinned to", historyFetches)
	}
}

// A game whose only lineage is held must never silently seed a second one:
// a new lineage can never rejoin the held history once it migrates, so the
// split is asked, and an unanswered or declined question just waits.
func TestAHeldOnlyGameNeverSeedsSilently(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	foreign := foreignRevision()
	seedAsked := false
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Omnisave{mirrorLineage(foreign)})
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodPost:
			seedAsked = true
			http.Error(response, "seeding is not part of this story", http.StatusInternalServerError)
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Revision{foreign})
		default:
			http.NotFound(response, r)
		}
	})
	// Unbound: the pass has to decide what this local save belongs to.
	answered := false
	prompts := strictPrompts(t)
	prompts.HeldSeed = func(string) (bool, error) {
		answered = true
		return false, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

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
	prompts.HeldSeed = func(string) (bool, error) { return true, nil }
	reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})
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
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	// The save answers to the retired spelling too, so the layout check
	// alone would accept the held lineage's paths. Exclusion has to come
	// from the hold itself.
	fixture.Save.LocationAliases = []string{omnisave.MirrorLocation}
	fixture.Scans[0].Games[0].Saves[0].LocationAliases = []string{omnisave.MirrorLocation}

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
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{held, native})
		case "/api/v1/omnisaves/omnisave-held/revisions":
			writeJSON(t, response, []omnisave.Revision{heldHistory})
		case "/api/v1/omnisaves/omnisave-native/revisions":
			writeJSON(t, response, []omnisave.Revision{nativeHistory})
		default:
			http.NotFound(response, r)
		}
	})
	var offered []savesync.AmbiguousOption
	// The question itself is the assertion, so the answer ends the pass
	// rather than sending it down an adoption this story does not serve. The
	// error coming back unchanged is the prompt contract a caller relies on
	// to tell a person calling off the run from a failure.
	errOffered := errors.New("options captured")
	prompts := strictPrompts(t)
	prompts.Ambiguous = func(_ string, options []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
		offered = options
		return savesync.AmbiguousChoice{}, errOffered
	}

	outcome, _, err := tryReconcile(server, nil, &fixture, savesync.Options{Prompts: prompts})

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
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	// The name proves the mapping, but the content does not agree, so an
	// unbound save cannot claim the lineage.
	mirror := omnisave.Revision{
		ID: "revision-1", OmnisaveID: "omnisave-1",
		Files: []omnisave.RevisionFile{{
			Path:     omnisave.MirrorLocation + "/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "aa", Size: 2},
		}},
	}
	var mutex sync.Mutex
	migrations := 0
	server := stubServer(t, func(response http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case r.URL.Path == "/api/v1/omnisaves" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Omnisave{mirrorLineage(mirror)})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/migrate-locations":
			migrations++
			writeJSON(t, response, omnisave.MigrationResult{
				PathFormatVersion: omnisave.PathFormatNative, Revisions: 1, Files: 1,
			})
		case r.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions" && r.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Revision{mirror})
		default:
			http.NotFound(response, r)
		}
	})
	prompts := savesync.Prompts{HeldSeed: func(string) (bool, error) { return false, nil }}

	// Unbound: held, and the verdict is remembered.
	_, report := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})
	if migrations != 0 {
		t.Fatal("an unbound save that matches no revision must not migrate the lineage")
	}
	if reason := heldFor(t, report); !errors.Is(reason, savesync.HoldNoMatchingRevision) {
		t.Fatalf("expected the unbound save held for its content, got %v", reason)
	}
	recorded, remembered := fixture.State.HeldProofFor(fixture.Local(), "omnisave-1")
	if !remembered || recorded.Bound {
		t.Fatalf("held proof = %+v", recorded)
	}

	// Bound to that same lineage, nothing else changed: the recorded hold
	// no longer describes the question being asked, so it is proven again.
	if err := fixture.State.Bind(fixture.Local(), "omnisave-1"); err != nil {
		t.Fatal(err)
	}
	reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})
	if migrations != 1 {
		t.Fatalf("binding should have retried the proof; migrations = %d", migrations)
	}
	if _, stillHeld := fixture.State.HeldProofFor(fixture.Local(), "omnisave-1"); stillHeld {
		t.Fatal("a migrated lineage must not keep a recorded hold")
	}
}
