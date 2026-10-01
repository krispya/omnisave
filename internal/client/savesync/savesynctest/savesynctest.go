// Package savesynctest stands up what Save Sync tests run against: the real
// server stack over httptest, a Device with one tracked game and one save on
// disk, another Device committing to the same lineage, and a Reporter that
// records what a pass decided. It is shared by the savesync domain tests and
// by the client's watch-loop tests, which drive the domain through real
// passes.
package savesynctest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	deviceservice "github.com/krisbaumgartner/omnisave/internal/device/service"
	"github.com/krisbaumgartner/omnisave/internal/httpapi"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

// Token is the owner token every test server accepts.
const Token = "secret"

// NewServer is a client for a fresh real server: sqlite, the services, and
// the HTTP API, torn down with the test.
func NewServer(t testing.TB) *remote.Client {
	return NewInterceptedServer(t, nil)
}

// NewObservedServer is NewServer with a hook that sees every request, so a
// test can count what a pass asked for.
func NewObservedServer(t testing.TB, observe func(*http.Request)) *remote.Client {
	t.Helper()
	return NewInterceptedServer(t, func(_ http.ResponseWriter, request *http.Request) bool {
		observe(request)
		return false
	})
}

// NewInterceptedServer is the real server behind a hook that may answer a
// request itself, so a test can break one endpoint mid-run and repair it
// again. A hook returning true has answered; false passes the request
// through to the real handler.
func NewInterceptedServer(t testing.TB, intercept func(http.ResponseWriter, *http.Request) bool) *remote.Client {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlitestorage.Open(
		filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	var handler http.Handler = httpapi.New(accessservice.New(repository, Token), httpapi.Config{
		Saves:   omnisaveservice.New(repository),
		Catalog: catalogservice.New(repository, repository),
		Devices: deviceservice.New(repository),
	})
	if intercept != nil {
		wrapped := handler
		handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if intercept(response, request) {
				return
			}
			wrapped.ServeHTTP(response, request)
		})
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	remoteClient, err := remote.New(server.URL, Token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return remoteClient
}

// Fixture is one Device tracking one game, "Chrono Trigger", with one battery
// save of one file on disk.
type Fixture struct {
	// LocalPath is the save's file; Content is what it held at creation.
	LocalPath string
	Content   []byte
	Save      target.Save
	// Scans is what a scan of this Device finds.
	Scans []client.TargetScan
	State tracking.State
}

// NewFixture is a Device whose game already resolved to the Library identity
// "server-game-1", for tests that answer the server themselves.
func NewFixture(t testing.TB, content string) Fixture {
	t.Helper()
	directory := t.TempDir()
	localPath := filepath.Join(directory, "Chrono Trigger.srm")
	payload := []byte(content)
	if err := os.WriteFile(localPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	save := target.Save{
		ID:       "local-save-1",
		TargetID: "target-1",
		GameID:   "local-game-1",
		Kind:     "battery",
		Files: []target.File{{
			Path:         localPath,
			LocationID:   "battery",
			RelativePath: "Chrono Trigger.srm",
			Size:         int64(len(payload)),
		}},
	}
	scans := []client.TargetScan{{
		Target: target.Target{ID: "target-1", Adapter: "retroarch"},
		Games: []client.GameScan{{
			Game: target.InstalledGame{
				ID:       "local-game-1",
				TargetID: "target-1",
				Identity: target.GameIdentity{Title: "Chrono Trigger"},
			},
			Saves: []target.Save{save},
		}},
	}}
	state := tracking.NewState()
	state.Games["local-game-1"] = tracking.Game{
		ID:           "local-game-1",
		Adapter:      "retroarch",
		TargetID:     "target-1",
		Title:        "Chrono Trigger",
		ServerGameID: "server-game-1",
	}
	return Fixture{LocalPath: localPath, Content: payload, Save: save, Scans: scans, State: state}
}

// NewSyncFixture is a Device whose game resolves through the real server on
// its first pass instead of carrying a preset Library identity.
func NewSyncFixture(t testing.TB, content string) Fixture {
	t.Helper()
	fixture := NewFixture(t, content)
	fixture.Scans[0].Games[0].Game.Identity = target.GameIdentity{
		Title:       "Chrono Trigger",
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "424242"}},
	}
	game := fixture.State.Games["local-game-1"]
	game.ServerGameID = ""
	fixture.State.Games["local-game-1"] = game
	return fixture
}

// Local is the fixture's save as tracking identifies it.
func (f *Fixture) Local() tracking.LocalSave {
	return savesync.LocalSaveFrom(f.Scans[0], f.Scans[0].Games[0], f.Save)
}

// Write replaces the save file's content, as the game saving would.
func (f *Fixture) Write(t testing.TB, content string) {
	t.Helper()
	if err := os.WriteFile(f.LocalPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Read is the save file's content now.
func (f *Fixture) Read(t testing.TB) string {
	t.Helper()
	content, err := os.ReadFile(f.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// SyncOnce runs one headless-by-default pass over the fixture's scan without
// adapters — tracking, then reconciliation — and returns its outcome.
func SyncOnce(t testing.TB, server savesync.Server, fixture *Fixture, options savesync.Options) savesync.Outcome {
	t.Helper()
	ctx := context.Background()
	report := &Recorder{}
	outcome, confirmed := savesync.SyncTracking(ctx, server, &fixture.State, fixture.Scans, nil, report)
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	ports := savesync.Ports{Server: server, Report: report}
	if err := savesync.Reconcile(ctx, ports, &fixture.State, fixture.Scans, confirmed, &outcome, options); err != nil {
		t.Fatal(err)
	}
	return outcome
}

// OtherDeviceCommit pushes content onto the Omnisave's current revision as if
// another Device had played, and returns the new current revision.
func OtherDeviceCommit(t testing.TB, server *remote.Client, omnisaveID, content string) omnisave.Revision {
	t.Helper()
	ctx := context.Background()
	otherPath := filepath.Join(t.TempDir(), "Chrono Trigger.srm")
	if err := os.WriteFile(otherPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	otherSave := target.Save{
		ID: "other-save", TargetID: "other-target", GameID: "other-game", Kind: "battery",
		Files: []target.File{{Path: otherPath, LocationID: "battery", RelativePath: "Chrono Trigger.srm"}},
	}
	saves, err := server.ListOmnisaves(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var currentID string
	for _, save := range saves {
		if save.ID == omnisaveID && save.CurrentRevisionID != nil {
			currentID = *save.CurrentRevisionID
		}
	}
	var current omnisave.Revision
	for _, revision := range Revisions(t, server, omnisaveID) {
		if revision.ID == currentID {
			current = revision
		}
	}
	if current.ID == "" {
		t.Fatal("no current revision to commit on top of")
	}
	revision, err := binding.Push(ctx, server, omnisaveID, otherSave, current.ID, current.Files)
	if err != nil {
		t.Fatal(err)
	}
	return *revision
}

// PushSecondRevision seeds the fixture and pushes content over the seed, so
// the history has an ancestor to rewind to. It returns the seed revision and
// the binding after the push.
func PushSecondRevision(t testing.TB, server *remote.Client, fixture *Fixture, content string) (omnisave.Revision, tracking.Binding) {
	t.Helper()
	if outcome := SyncOnce(t, server, fixture, savesync.Options{}); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	fixture.Write(t, content)
	if outcome := SyncOnce(t, server, fixture, savesync.Options{}); outcome.Pushed != 1 {
		t.Fatalf("expected the second pass to push, got %+v", outcome)
	}
	bound, ok := fixture.State.BindingFor(fixture.Local())
	if !ok || bound.LastSyncedRevisionID == nil {
		t.Fatalf("expected a binding with a baseline after pushing, got %+v", bound)
	}
	history := Revisions(t, server, bound.OmnisaveID)
	if len(history) != 2 {
		t.Fatalf("expected a seed and one push, got %d revisions", len(history))
	}
	for _, revision := range history {
		if revision.ID != *bound.LastSyncedRevisionID {
			return revision, bound
		}
	}
	t.Fatal("expected an ancestor revision below the baseline")
	return omnisave.Revision{}, tracking.Binding{}
}

// Rewind restores the Omnisave's current revision to revision, as the Dash
// does, expecting it currently at the binding's baseline.
func Rewind(t testing.TB, server *remote.Client, bound tracking.Binding, revision omnisave.Revision) {
	t.Helper()
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                revision.ID,
	}); err != nil {
		t.Fatal(err)
	}
}

// Revisions is one Omnisave's history on the server.
func Revisions(t testing.TB, server *remote.Client, omnisaveID string) []omnisave.Revision {
	t.Helper()
	history, err := server.ListRevisions(context.Background(), omnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

// Saves is every Omnisave on the server.
func Saves(t testing.TB, server *remote.Client) []omnisave.Omnisave {
	t.Helper()
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return saves
}

// Adapter serves a fixture's target, game, and save through a real
// client.Scanner, statting the save's files on every scan so a pull's
// changes are seen the way a real adapter would see them.
type Adapter struct {
	Fixture *Fixture
}

// Name is the fixture target's adapter.
func (a Adapter) Name() string { return a.Fixture.Scans[0].Target.Adapter }

func (a Adapter) DiscoverTargets(context.Context) ([]target.Target, error) {
	return []target.Target{a.Fixture.Scans[0].Target}, nil
}

func (a Adapter) DiscoverGames(context.Context, target.Target) ([]target.InstalledGame, error) {
	return []target.InstalledGame{a.Fixture.Scans[0].Games[0].Game}, nil
}

func (a Adapter) DiscoverSaves(context.Context, target.Target, target.InstalledGame) ([]target.Save, error) {
	save := a.Fixture.Save
	files := make([]target.File, len(save.Files))
	copy(files, save.Files)
	for index, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			return nil, err
		}
		files[index].Size = info.Size()
		files[index].Modified = info.ModTime()
	}
	save.Files = files
	return []target.Save{save}, nil
}

func (a Adapter) DiscoverSaveDestinations(context.Context, target.Target, target.InstalledGame) ([]target.SaveDestination, error) {
	return nil, nil
}
