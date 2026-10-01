package savesync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// stubServer is a client for a hand-written server, for stories whose server
// answers are easier to state than to produce.
func stubServer(t *testing.T, handler http.HandlerFunc) *remote.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	remoteClient, err := remote.New(server.URL, savesynctest.Token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return remoteClient
}

// reconcile runs one reconciliation of the fixture's game, as a pass does
// once the server has confirmed it, and fails the test on an error.
func reconcile(t *testing.T, server savesync.Server, fixture *savesynctest.Fixture, options savesync.Options) (savesync.Outcome, *savesynctest.Recorder) {
	t.Helper()
	return reconcileWith(t, server, nil, fixture, options)
}

// reconcileWith is reconcile with the adapters a story's target needs.
func reconcileWith(t *testing.T, server savesync.Server, adapters savesync.Adapters, fixture *savesynctest.Fixture, options savesync.Options) (savesync.Outcome, *savesynctest.Recorder) {
	t.Helper()
	outcome, report, err := tryReconcile(server, adapters, fixture, options)
	if err != nil {
		t.Fatal(err)
	}
	return outcome, report
}

// tryReconcile is reconcile for stories about the error a pass returns.
func tryReconcile(server savesync.Server, adapters savesync.Adapters, fixture *savesynctest.Fixture, options savesync.Options) (savesync.Outcome, *savesynctest.Recorder, error) {
	outcome := savesync.Outcome{Tracked: 1, Synced: true}
	report := &savesynctest.Recorder{}
	ports := savesync.Ports{Server: server, Adapters: adapters, Report: report}
	err := savesync.Reconcile(context.Background(), ports, &fixture.State, fixture.Scans,
		map[string]bool{"local-game-1": true}, &outcome, options)
	return outcome, report, err
}

// syncOnce is one headless pass over the fixture against a real server.
func syncOnce(t *testing.T, server savesync.Server, fixture *savesynctest.Fixture) savesync.Outcome {
	t.Helper()
	return savesynctest.SyncOnce(t, server, fixture, savesync.Options{})
}

// findRevision looks one revision up in a history.
func findRevision(history []omnisave.Revision, id string) (omnisave.Revision, bool) {
	for _, revision := range history {
		if revision.ID == id {
			return revision, true
		}
	}
	return omnisave.Revision{}, false
}

// strictPrompts answers every question by failing the test. A story replaces
// the one prompt it expects, so any other question is a regression.
func strictPrompts(t *testing.T) savesync.Prompts {
	return savesync.Prompts{
		SyncToDevice: func(gameTitle string, _ []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
			t.Fatalf("unexpected sync-to-device prompt for %q", gameTitle)
			return savesync.SyncToDeviceChoice{}, nil
		},
		Stale: func(question savesync.StaleQuestion) (savesync.DivergedChoice, error) {
			t.Fatalf("unexpected stale prompt for %q", question.GameTitle)
			return "", nil
		},
		Ambiguous: func(gameTitle string, _ []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
			t.Fatalf("unexpected ambiguity prompt for %q", gameTitle)
			return savesync.AmbiguousChoice{}, nil
		},
		Diverged: func(question savesync.DivergedQuestion) (savesync.DivergedChoice, error) {
			t.Fatalf("unexpected diverged prompt for %q", question.GameTitle)
			return "", nil
		},
		HeldSeed: func(gameTitle string) (bool, error) {
			t.Fatalf("unexpected held-seed prompt for %q", gameTitle)
			return false, nil
		},
	}
}

// answering is strictPrompts with the divergence question answered.
func answering(t *testing.T, choice savesync.DivergedChoice) savesync.Prompts {
	prompts := strictPrompts(t)
	prompts.Diverged = func(savesync.DivergedQuestion) (savesync.DivergedChoice, error) {
		return choice, nil
	}
	return prompts
}

// testRevision is a one-file battery revision holding content.
func testRevision(id, omnisaveID, content string) omnisave.Revision {
	return omnisave.Revision{
		ID: id, OmnisaveID: omnisaveID,
		Files: []omnisave.RevisionFile{{
			Path:     "battery/Chrono Trigger.srm",
			Artifact: artifactOf(content),
		}},
	}
}

func artifactOf(content string) omnisave.Artifact {
	digest := sha256.Sum256([]byte(content))
	return omnisave.Artifact{
		Format: "application/octet-stream", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content)),
	}
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Error(err)
	}
}

// assertBinding fails unless the fixture's save is bound to omnisaveID with
// revisionID as its sync baseline.
func assertBinding(t *testing.T, fixture *savesynctest.Fixture, omnisaveID, revisionID string) {
	t.Helper()
	bound, ok := fixture.State.BindingFor(fixture.Local())
	if !ok || bound.OmnisaveID != omnisaveID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != revisionID {
		t.Fatalf("expected binding to %s at %s, got %+v", omnisaveID, revisionID, bound)
	}
}
