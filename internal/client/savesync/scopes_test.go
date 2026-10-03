package savesync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/gamesave"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// slotFixture is one STS2 Steam account, whose save slots are the game's
// profiles: profile 1 is occupied, profiles 2 and 3 are empty, and the shared
// selector and settings sit beside them.
type slotFixture struct {
	root        string
	game        target.InstalledGame
	server      *remote.Client
	state       tracking.State
	saveProfile saveprofile.Profile
}

func newSlotFixture(t *testing.T) *slotFixture {
	t.Helper()
	server := savesynctest.NewServer(t)
	game := target.InstalledGame{ID: "local-sts2", TargetID: "steam:fixture", Identity: target.GameIdentity{Title: "Slay the Spire 2", Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "2868840"}}}}
	resolved, err := server.ResolveGame(context.Background(), catalog.Evidence{Identifiers: game.Identity.Identifiers, TitleHint: game.Identity.Title})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "SlayTheSpire2", "steam", "100")
	f := &slotFixture{root: root, game: game, server: server, state: tracking.NewState(), saveProfile: saveprofile.Profile{Provider: "test", ProviderID: "sts2", Rules: []saveprofile.Rule{{ID: "account", Path: filepath.Join(filepath.Dir(root), "<storeUserId>")}}}}
	f.state.Games[game.ID] = tracking.Game{ID: game.ID, Adapter: "steam", TargetID: game.TargetID, Title: game.Identity.Title, ServerGameID: resolved.Game.ID}
	f.write(t, "profile1/saves/progress.save", "progress-before")
	f.write(t, "profile1/saves/current_run.save", "run-before")
	f.write(t, "profile1/saves/current_run.save.backup", "backup-before")
	f.write(t, "profile.save", "selector")
	f.write(t, "settings.save", "settings")
	return f
}

func (f *slotFixture) write(t *testing.T, name, content string) {
	t.Helper()
	p := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *slotFixture) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// scan discovers the whole save and, through the shipped extension, its slots.
func (f *slotFixture) scan(t *testing.T) []client.TargetScan {
	t.Helper()
	saves, err := saveprofile.Resolve(f.game, f.saveProfile)
	if err != nil {
		t.Fatal(err)
	}
	destinations, err := saveprofile.ResolveDestinations(f.game, f.saveProfile)
	if err != nil {
		t.Fatal(err)
	}
	adapters, err := gamesave.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range adapters {
		if adapter.Supports(f.game.Identity) {
			slots, err := adapter.Discover(context.Background(), f.game, destinations)
			if err != nil {
				t.Fatal(err)
			}
			return []client.TargetScan{{Target: target.Target{ID: f.game.TargetID, Adapter: "steam"}, Games: []client.GameScan{{Game: f.game, Saves: saves, Destinations: destinations, Slots: gamesave.Discovery{Adapter: adapter.ID(), Found: slots}}}}}
		}
	}
	t.Fatal("fixture game has no extension")
	return nil
}

func (f *slotFixture) slots(t *testing.T) []gamesave.Slot {
	t.Helper()
	return f.scan(t)[0].Games[0].Slots.Found
}

func (f *slotFixture) sync(t *testing.T, scans []client.TargetScan, prompts savesync.Prompts) (savesync.Outcome, *savesynctest.Recorder) {
	t.Helper()
	outcome := savesync.Outcome{}
	report := &savesynctest.Recorder{}
	if err := savesync.Reconcile(context.Background(), savesync.Ports{Server: f.server, Report: report}, &f.state, scans, map[string]bool{f.game.ID: true}, &outcome, savesync.Options{Prompts: prompts}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 0 {
		t.Fatalf("slot sync failed: %+v", report.Events)
	}
	return outcome, report
}

// track runs the first pass after tracking, which needs no answers.
func (f *slotFixture) track(t *testing.T) {
	t.Helper()
	f.sync(t, f.scan(t), savesync.Prompts{})
}

func (f *slotFixture) bindingFor(t *testing.T, label string) tracking.Binding {
	t.Helper()
	for _, bound := range f.state.Bindings {
		if bound.Slot == label {
			return bound
		}
	}
	t.Fatalf("%s is not bound", label)
	return tracking.Binding{}
}

func (f *slotFixture) histories(t *testing.T) []omnisave.Omnisave {
	t.Helper()
	listed, err := f.server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return listed
}

func TestEverySlotIsTrackedByDefault(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	bound := f.bindingFor(t, "Profile 1")
	if bound.Scope != (omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: "sts2.vanilla"}) {
		t.Fatal("the occupied slot was not bound as a save slot")
	}
	histories := f.histories(t)
	if len(histories) != 1 {
		t.Fatal("empty slots or the whole save created histories")
	}
	if histories[0].DisplayName != "Profile 1" {
		t.Fatalf("expected the history named after its slot, got %q", histories[0].DisplayName)
	}
	if f.state.SaveSelections[f.game.ID] != (tracking.SaveSelection{Adapter: "sts2.vanilla"}) {
		t.Fatal("the slot default was not recorded")
	}
}

func TestSlotHistoriesNeverMatchWholeSaveHistories(t *testing.T) {
	f := newSlotFixture(t)
	scans := f.scan(t)
	aggregate, _, err := binding.Seed(context.Background(), f.server, f.state.Games[f.game.ID].ServerGameID, scans[0].Games[0].Saves[0], "Whole backup")
	if err != nil {
		t.Fatal(err)
	}
	f.track(t)
	if f.bindingFor(t, "Profile 1").OmnisaveID == aggregate.ID {
		t.Fatal("a slot joined the whole-save history")
	}
}

func TestANewSlotGetsItsOwnHistoryWithoutTouchingOthers(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	store := tracking.NewStore(filepath.Join(t.TempDir(), "client.json"))
	if err := store.Save(f.state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.state = loaded
	f.write(t, "settings.save", "changed settings")
	f.write(t, "profile3/saves/progress.save", "new slot")
	f.sync(t, f.scan(t), savesync.Prompts{})
	history, err := f.server.ListRevisions(context.Background(), f.bindingFor(t, "Profile 1").OmnisaveID)
	if err != nil || len(history) != 1 {
		t.Fatal("shared or sibling changes advanced the slot history")
	}
	if f.bindingFor(t, "Profile 3").OmnisaveID == f.bindingFor(t, "Profile 1").OmnisaveID || len(f.histories(t)) != 2 {
		t.Fatal("the new slot did not start its own history")
	}
}

func TestSlotRewindRestoresRunAndBackupWithoutTouchingSiblings(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	bound := f.bindingFor(t, "Profile 1")
	before := *bound.LastSyncedRevisionID
	f.write(t, "profile1/saves/current_run.save", "run-after")
	f.write(t, "profile1/saves/current_run.save.backup", "backup-after")
	f.write(t, "profile1/saves/history/completed.run", "terminal history")
	f.sync(t, f.scan(t), savesync.Prompts{})
	f.write(t, "profile2/saves/progress.save", "sibling")
	current := *f.bindingFor(t, "Profile 1").LastSyncedRevisionID
	if _, err := f.server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{ExpectedCurrentRevisionID: &current, RevisionID: before}); err != nil {
		t.Fatal(err)
	}
	outcome, _ := f.sync(t, f.scan(t), savesync.Prompts{})
	if outcome.Pulled != 1 || f.read(t, "profile1/saves/current_run.save") != "run-before" || f.read(t, "profile1/saves/current_run.save.backup") != "backup-before" {
		t.Fatal("rewind did not restore coherent run state")
	}
	if _, err := os.Stat(filepath.Join(f.root, "profile1/saves/history/completed.run")); !os.IsNotExist(err) {
		t.Fatal("newer terminal history survived rewind")
	}
	for name, content := range map[string]string{"profile2/saves/progress.save": "sibling", "profile.save": "selector", "settings.save": "settings"} {
		if f.read(t, name) != content {
			t.Fatal("rewind touched unrelated content")
		}
	}
}

// A history follows one slot. Offering it to an empty sibling would let
// play in either slot overwrite the other.
func TestASlotHistoryIsNeverOfferedToASiblingSlot(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	_, report := f.sync(t, f.scan(t), savesync.Prompts{SyncToDevice: func(string, []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		t.Fatal("slot 1's history was offered to an empty slot")
		return savesync.SyncToDeviceChoice{}, nil
	}})
	for _, kind := range []string{"NoSave", "SaveAvailable", "SaveLocationUnavailable"} {
		if len(report.Of(kind)) != 0 {
			t.Fatalf("empty slots beside an occupied one reported %s", kind)
		}
	}
}

func TestACopiedSlotStartsItsOwnHistory(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	f.write(t, "profile2/saves/progress.save", "progress-before")
	f.write(t, "profile2/saves/current_run.save", "run-before")
	f.write(t, "profile2/saves/current_run.save.backup", "backup-before")
	f.sync(t, f.scan(t), savesync.Prompts{})
	if f.bindingFor(t, "Profile 1").OmnisaveID == f.bindingFor(t, "Profile 2").OmnisaveID {
		t.Fatal("equal bytes made two slots share one history")
	}
}

func TestAnUnownedHistoryIsPlacedAtTheChosenSlot(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	profile1 := f.bindingFor(t, "Profile 1")
	fork, err := f.server.ForkOmnisave(context.Background(), profile1.OmnisaveID, omnisave.ForkOmnisave{RevisionID: *profile1.LastSyncedRevisionID, DisplayName: "Other device"})
	if err != nil {
		t.Fatal(err)
	}
	slots := f.slots(t)
	f.sync(t, f.scan(t), savesync.Prompts{SyncToDevice: func(_ string, options []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		if len(options) != 2 || options[0].OmnisaveID != fork.Omnisave.ID || options[1].OmnisaveID != fork.Omnisave.ID {
			t.Fatalf("expected the unowned history for both empty slots, got %+v", options)
		}
		return savesync.SyncToDeviceChoice{OmnisaveID: fork.Omnisave.ID, DestinationID: slots[1].Save.ID}, nil
	}})
	if f.read(t, "profile2/saves/progress.save") != "progress-before" {
		t.Fatal("slot placement failed")
	}
	if _, err := os.Stat(filepath.Join(f.root, "profile3")); !os.IsNotExist(err) {
		t.Fatal("an unchosen destination was populated")
	}
	// Each slot now advances only its own history.
	f.write(t, "profile2/saves/progress.save", "slot 2 progress")
	f.sync(t, f.scan(t), savesync.Prompts{})
	if f.read(t, "profile1/saves/progress.save") != "progress-before" {
		t.Fatal("playing slot 2 overwrote slot 1")
	}
}

func TestPendingSlotPlacementWaitsForItsOwnDestination(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	profile1 := f.bindingFor(t, "Profile 1")
	fork, err := f.server.ForkOmnisave(context.Background(), profile1.OmnisaveID, omnisave.ForkOmnisave{RevisionID: *profile1.LastSyncedRevisionID})
	if err != nil {
		t.Fatal(err)
	}
	history, err := f.server.ListRevisions(context.Background(), fork.Omnisave.ID)
	if err != nil {
		t.Fatal(err)
	}
	scans := f.scan(t)
	slots := scans[0].Games[0].Slots.Found
	planned, err := binding.PlannedMaterialization(slots[1].Destination, history[0])
	if err != nil {
		t.Fatal(err)
	}
	local := savesync.LocalSaveFrom(scans[0], scans[0].Games[0], planned)
	f.state.RecordPlacement(local, tracking.PendingPlacement{OmnisaveID: fork.Omnisave.ID, Save: planned, Current: history[0], Destination: &slots[1].Destination})
	// The extension still offers another empty slot. That availability must
	// not authorize recreating a journaled destination it no longer offers.
	scans[0].Games[0].Slots.Found = []gamesave.Slot{slots[0], slots[2]}
	f.sync(t, scans, savesync.Prompts{SyncToDevice: func(string, []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		t.Fatal("a history with a journaled restore was offered elsewhere")
		return savesync.SyncToDeviceChoice{}, nil
	}})
	if _, err := os.Stat(filepath.Join(f.root, "profile2")); !os.IsNotExist(err) {
		t.Fatal("unavailable pending destination was recreated")
	}
	if _, pending := f.state.PlacementFor(local); !pending {
		t.Fatal("unavailable destination lost its restore journal")
	}
}

func TestAMissingAdapterNeverBroadensRecordedSlots(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	scans := f.scan(t)
	scans[0].Games[0].Slots = gamesave.Discovery{}
	outcome := savesync.Outcome{}
	report := &savesynctest.Recorder{}
	if err := savesync.Reconcile(context.Background(), savesync.Ports{Server: f.server, Report: report}, &f.state, scans, map[string]bool{f.game.ID: true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 1 || len(report.Of("SaveFailed")) != 1 || outcome.Seeded != 0 || len(f.histories(t)) != 1 {
		t.Fatal("a missing adapter broadened the recorded scope")
	}
}

// An adapter with no slots here, such as on a launcher it does not support or
// before any profile exists, leaves the game's ordinary whole-save binding.
func TestAGameWithoutSlotsHereBindsItsWholeSave(t *testing.T) {
	f := newSlotFixture(t)
	scans := f.scan(t)
	scans[0].Games[0].Slots = gamesave.Discovery{Adapter: "sts2.vanilla"}
	outcome, _ := f.sync(t, scans, savesync.Prompts{})
	if outcome.Seeded != 1 || len(f.state.Bindings) != 1 || f.state.Bindings[0].Scope != (omnisave.SaveScope{}) {
		t.Fatal("the whole save was not bound")
	}
	if _, recorded := f.state.SaveSelections[f.game.ID]; recorded {
		t.Fatal("a scope was recorded without slots to offer")
	}
}

// A discovery that could not finish, such as a file vanishing while the game
// writes, must not read as "no slots": that would pick the whole save forever.
func TestAFailedDiscoveryHoldsTheGameRatherThanItsWholeSave(t *testing.T) {
	f := newSlotFixture(t)
	scans := f.scan(t)
	scans[0].Games[0].Slots.Err = errors.New("save directory cannot be enumerated completely")
	outcome := savesync.Outcome{}
	report := &savesynctest.Recorder{}
	if err := savesync.Reconcile(context.Background(), savesync.Ports{Server: f.server, Report: report}, &f.state, scans, map[string]bool{f.game.ID: true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Failed != 1 || len(f.state.Bindings) != 0 || len(f.histories(t)) != 0 {
		t.Fatal("a failed discovery broadened the game to its whole save")
	}
	f.track(t)
	if f.bindingFor(t, "Profile 1").Scope.Kind != omnisave.ScopeSlot {
		t.Fatal("slots did not apply once discovery recovered")
	}
}

func TestASlotBindingKeepsItsScopeBeforeTheDefaultIsRecorded(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	// As after a manual bind on a Device that never recorded the default.
	delete(f.state.SaveSelections, f.game.ID)
	scans := f.scan(t)
	scans[0].Games[0].Slots = gamesave.Discovery{Adapter: "sts2.vanilla"}
	outcome := savesync.Outcome{}
	report := &savesynctest.Recorder{}
	if err := savesync.Reconcile(context.Background(), savesync.Ports{Server: f.server, Report: report}, &f.state, scans, map[string]bool{f.game.ID: true}, &outcome, savesync.Options{}); err != nil {
		t.Fatal(err)
	}
	if outcome.Seeded != 0 || len(f.histories(t)) != 1 || len(report.Of("SaveFailed")) != 1 {
		t.Fatal("a slot binding was broadened to the whole save")
	}
}

// A profile deleted in game leaves its slot empty but still bound. Placing
// another history there must finish and rebind, not hold the slot forever.
func TestAnEmptiedSlotCanTakeAnotherHistory(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	profile1 := f.bindingFor(t, "Profile 1")
	fork, err := f.server.ForkOmnisave(context.Background(), profile1.OmnisaveID, omnisave.ForkOmnisave{RevisionID: *profile1.LastSyncedRevisionID, DisplayName: "Other device"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "profile1")); err != nil {
		t.Fatal(err)
	}
	slots := f.slots(t)
	f.sync(t, f.scan(t), savesync.Prompts{SyncToDevice: func(string, []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		return savesync.SyncToDeviceChoice{OmnisaveID: fork.Omnisave.ID, DestinationID: slots[0].Save.ID}, nil
	}})
	if f.bindingFor(t, "Profile 1").OmnisaveID != fork.Omnisave.ID || f.read(t, "profile1/saves/progress.save") != "progress-before" {
		t.Fatal("the emptied slot did not take the chosen history")
	}
	f.write(t, "profile1/saves/progress.save", "new progress")
	f.sync(t, f.scan(t), savesync.Prompts{})
	history, err := f.server.ListRevisions(context.Background(), fork.Omnisave.ID)
	if err != nil || len(history) != 2 {
		t.Fatal("new progress in the slot did not push")
	}
}

func TestExistingWholeSaveBindingsKeepTheirMeaning(t *testing.T) {
	f := newSlotFixture(t)
	scans := f.scan(t)
	whole := scans[0].Games[0].Saves[0]
	aggregate, revision, err := binding.Seed(context.Background(), f.server, f.state.Games[f.game.ID].ServerGameID, whole, "Whole backup")
	if err != nil {
		t.Fatal(err)
	}
	local := savesync.LocalSaveFrom(scans[0], scans[0].Games[0], whole)
	if err := f.state.Bind(local, aggregate.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.state.RecordSynced(local, aggregate.ID, revision.ID); err != nil {
		t.Fatal(err)
	}
	f.track(t)
	if len(f.state.Bindings) != 1 || f.state.Bindings[0].OmnisaveID != aggregate.ID || len(f.histories(t)) != 1 {
		t.Fatal("the slot default reinterpreted an existing whole-save binding")
	}
}

func TestTheWholeSaveIsAnExplicitChoice(t *testing.T) {
	f := newSlotFixture(t)
	f.track(t)
	asked, err := savesync.ChooseSaveScopes(&f.state, f.scan(t), func(question savesync.ScopeQuestion) (savesync.ScopeChoice, error) {
		if question.Current != savesync.ScopeSaveSlots || !slices.Equal(question.Slots, []string{"Profile 1", "Profile 2", "Profile 3"}) {
			t.Fatalf("unexpected scope question %+v", question)
		}
		return savesync.ScopeWholeSave, nil
	})
	if err != nil || !asked {
		t.Fatalf("scope was not revisited: %v", err)
	}
	if len(f.state.Bindings) != 0 {
		t.Fatal("slot mappings survived the whole-save choice")
	}
	outcome, _ := f.sync(t, f.scan(t), savesync.Prompts{})
	if outcome.Seeded != 1 || f.state.Bindings[0].Scope != (omnisave.SaveScope{}) || len(f.histories(t)) != 2 {
		t.Fatal("the whole save did not start its own history beside the slot history")
	}
}

func TestAPlayedGameMarksEachSlotRow(t *testing.T) {
	f := newSlotFixture(t)
	presence := savesync.TrackedPresence(nil, &f.state, f.scan(t))
	titles := presence.Titles(map[string]bool{f.game.ID: true})
	want := []string{"Slay the Spire 2", "Slay the Spire 2 · Profile 1", "Slay the Spire 2 · Profile 2", "Slay the Spire 2 · Profile 3"}
	if !slices.Equal(titles, want) {
		t.Fatalf("expected every row of the played game, got %v", titles)
	}
}
