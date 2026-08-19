package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func newTestWatchModel(requests chan WatchRequest) watchModel {
	return newWatchModel(WatchConfig{ServerURL: "http://localhost:8080"}, requests)
}

// A run that reconciled before opening the view hands over what it found, so
// the first frame is the finished table with a footer that already proves
// the run reached the server — nothing waits for the next pass.
func TestAHandOffOpensOnTheTableAndClockItWasGiven(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Main", SyncedAt: time.Now().Add(-time.Hour)},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
		Synced: time.Now().Add(-11 * time.Minute),
	}, make(chan WatchRequest, 1))

	watching, _ := model.Update(watchWatchingMsg{files: 231})
	clocked, _ := watching.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	for _, text := range []string{
		"✓ Slay the Spire 2  Main · synced 1h ago",
		"· Project Zomboid   No save available",
		"watching 231 save paths · synced 11m ago · http://localhost:8081",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the opening frame to contain %q, got:\n%s", text, view)
		}
	}
}

func TestWatchViewShowsTheTableTallyAndFooter(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	updated, _ := model.Update(watchWatchingMsg{files: 3})
	updated, _ = updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now().Add(-2 * time.Minute)},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
		Summary: "  2 tracked · up to date",
		At:      time.Now(),
	}})
	clocked, _ := updated.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	for _, text := range []string{
		"▲ Omnisave · watching",
		"✓ Slay the Spire 2  Save 1 · synced 2m ago",
		"· Project Zomboid   No save available",
		"watching 3 save paths · synced just now",
		"http://localhost:8080",
		"s sync now · q quit",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the watch view to contain %q, got:\n%s", text, view)
		}
	}
}

func TestEventScrollbackKeepsSeparatePassesAdjacent(t *testing.T) {
	at := time.Date(2026, 8, 9, 0, 23, 0, 0, time.Local)
	waiting := Event{Glyph: "○", Title: "Slay the Spire 2", Sentence: "waiting for game to close", At: at}
	synced := Event{Glyph: "✓", Title: "Slay the Spire 2", Sentence: "synced with Main", At: at}
	scrollback := eventScrollback([]Event{waiting, synced})

	want := EventLine(waiting) + "\n" + EventLine(synced)
	if scrollback != want {
		t.Fatalf("expected adjacent events with no committed separator, got %q", scrollback)
	}
	if idle := eventScrollback(nil); idle != "" {
		t.Fatalf("expected an idle pass not to add a separator, got %q", idle)
	}
}

func TestWatchViewOwnsTheBlankLineBeforeItsTitle(t *testing.T) {
	view := ansi.Strip(newTestWatchModel(make(chan WatchRequest, 1)).View())
	if !strings.HasPrefix(view, "\n▲ Omnisave · watching\n") {
		t.Fatalf("expected one live separator before the title, got %q", view)
	}
}

func TestWatchSpinnerFinishesAFullRotationOnFastPasses(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	started := time.Now()
	updated, _ := model.Update(watchPassStartedMsg{at: started})
	settled, command := updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: started.Add(10 * time.Millisecond)}})

	fast := settled.(watchModel)
	if !fast.syncing || command == nil {
		t.Fatalf("expected a fast pass to keep spinning until the minimum, syncing=%v", fast.syncing)
	}
	// A newer pass makes the pending settle stale; its arrival must not
	// stop the fresh spinner.
	restarted, _ := fast.Update(watchPassStartedMsg{at: started.Add(20 * time.Millisecond)})
	ignored, _ := restarted.(watchModel).Update(watchSpinDoneMsg{generation: fast.spinGeneration})
	if !ignored.(watchModel).syncing {
		t.Fatal("expected a stale settle timer to be ignored")
	}
	current, _ := ignored.(watchModel).Update(watchSpinDoneMsg{generation: ignored.(watchModel).spinGeneration})
	if current.(watchModel).syncing {
		t.Fatal("expected the current settle timer to stop the spinner")
	}

	slow, _ := newTestWatchModel(make(chan WatchRequest, 1)).Update(watchPassStartedMsg{at: started.Add(-2 * time.Second)})
	slowSettled, _ := slow.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: started}})
	if slowSettled.(watchModel).syncing {
		t.Fatal("expected a pass longer than the minimum to settle immediately")
	}
}

func TestAnOutageKeepsTheLastTableAndClockAndShowsTheCause(t *testing.T) {
	model := newTestWatchModel(make(chan WatchRequest, 1))
	synced := time.Now().Add(-2 * time.Minute)
	settled, _ := model.Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: synced},
		}},
		At: synced,
	}})

	// The server goes away: the pass reaches no one and reports no games.
	offline, _ := settled.(watchModel).Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{General: []string{"Library sync failed — connection refused"}},
		At:       time.Now(),
	}})
	clocked, _ := offline.(watchModel).Update(watchClockMsg(time.Now()))

	view := ansi.Strip(clocked.(watchModel).View())
	if !strings.Contains(view, "✓ Slay the Spire 2  Save 1 · synced 2m ago") {
		t.Fatalf("expected the outage to leave the last known table, got:\n%s", view)
	}
	if !strings.Contains(view, "✗ Library sync failed — connection refused") {
		t.Fatalf("expected the outage shown as the current condition, got:\n%s", view)
	}
	if strings.Contains(view, "synced just now") {
		t.Fatalf("expected a failed pass not to claim a fresh sync, got:\n%s", view)
	}
}

func TestWatchViewSpinsWhileSyncingAndKeysWork(t *testing.T) {
	requests := make(chan WatchRequest, 1)
	model := newTestWatchModel(requests)
	updated, _ := model.Update(watchPassStartedMsg{})
	syncingView := ansi.Strip(updated.(watchModel).View())
	header, _, _ := strings.Cut(strings.TrimPrefix(syncingView, "\n"), "\n")
	if header != "▲ Omnisave · watching ⠋" {
		t.Fatalf("expected the active header to end at the spinner, got %q", header)
	}
	if !strings.Contains(syncingView, "watching 0 save paths") {
		t.Fatalf("expected the footer to keep its usual text while syncing, got:\n%s", syncingView)
	}

	settled, _ := updated.(watchModel).Update(watchPassFinishedMsg{result: PassResult{At: time.Now()}})
	settled.(watchModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	select {
	case request := <-requests:
		if request.Kind != WatchSyncNow {
			t.Fatalf("expected a sync-now request, got %v", request)
		}
	default:
		t.Fatal("expected pressing s to request a sync")
	}

	_, command := settled.(watchModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command == nil {
		t.Fatal("expected q to quit the view")
	}
}

// Presence rides the standing table without waiting for a pass: a live
// session claims the game's state glyph, and the glyph returns when the
// report empties.
func TestWatchMarksPlayingGamesWithTheirGlyph(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Main", SyncedAt: time.Now()},
			{Glyph: "·", Title: "Project Zomboid", Events: []string{"No save available"}},
		}},
	}, make(chan WatchRequest, 1))

	playing, _ := model.Update(watchPlayingMsg{"Slay the Spire 2"})
	view := ansi.Strip(playing.(watchModel).View())
	if !strings.Contains(view, "▶ Slay the Spire 2") {
		t.Fatalf("expected the playing glyph to claim the row, got:\n%s", view)
	}
	if strings.Contains(view, "✓ Slay the Spire 2") {
		t.Fatalf("expected the state glyph replaced while playing, got:\n%s", view)
	}

	cleared, _ := playing.(watchModel).Update(watchPlayingMsg(nil))
	view = ansi.Strip(cleared.(watchModel).View())
	if strings.Contains(view, "▶") || !strings.Contains(view, "✓ Slay the Spire 2") {
		t.Fatalf("expected the state glyph back once play stops, got:\n%s", view)
	}
}

// A rewind on another device reaches this one as a pass, and the whole point
// of watching is seeing which game it is working on: the row spins while the
// table underneath it still reads as what the last pass settled.
func TestWatchSpinsTheRowAPassHasInHand(t *testing.T) {
	synced := time.Now().Add(-4 * time.Minute)
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: synced},
			{Glyph: "✓", Title: "Project Zomboid", SyncedWith: "Save 2", SyncedAt: synced},
		}},
		Synced: synced,
	}, make(chan WatchRequest, 1))

	started, _ := model.Update(watchPassStartedMsg{at: time.Now()})
	// The pass is playing the game it is syncing, which changes nothing:
	// work outranks presence, or the row would look idle while it syncs.
	present, _ := started.(watchModel).Update(watchPlayingMsg{"Slay the Spire 2"})
	clocked, _ := present.(watchModel).Update(watchClockMsg(time.Now()))
	working, _ := clocked.(watchModel).Update(watchWorkingMsg("Slay the Spire 2"))

	view := ansi.Strip(working.(watchModel).View())
	if !strings.Contains(view, "⠋ Slay the Spire 2") {
		t.Fatalf("expected the worked row to wear the spinner, got:\n%s", view)
	}
	if !strings.Contains(view, "Save 1 · synced 4m ago") {
		t.Fatalf("expected the settled status to stay while the row spins, got:\n%s", view)
	}
	if !strings.Contains(view, "✓ Project Zomboid") {
		t.Fatalf("expected only the worked row marked, got:\n%s", view)
	}

	// Nothing is in hand between games, so no row keeps the spinner.
	between, _ := working.(watchModel).Update(watchWorkingMsg(""))
	if view := ansi.Strip(between.(watchModel).View()); strings.Contains(view, "⠋ ") {
		t.Fatalf("expected an idle pass to leave no row spinning, got:\n%s", view)
	}
}

// A spinner says a row is busy; the phase says what it is busy with, which
// is the difference between a sync working and a sync stuck. The header stays
// compact because it has no particular save to describe.
func TestTheWorkedRowSaysWhatThePassIsDoing(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire II", SyncedWith: "Save 1", SyncedAt: time.Now().Add(-4 * time.Minute)},
			{Glyph: "✓", Title: "Chrono Trigger", SyncedWith: "Save 2", SyncedAt: time.Now().Add(-4 * time.Minute)},
		}},
	}, make(chan WatchRequest, 1))

	// The pass has not reached a game yet, so only the spinner reaches the header.
	started, _ := model.Update(watchPassStartedMsg{at: time.Now()})
	scanning, _ := started.(watchModel).Update(watchPhaseMsg("scanning"))
	header, _, _ := strings.Cut(strings.TrimPrefix(ansi.Strip(scanning.(watchModel).View()), "\n"), "\n")
	if header != "▲ Omnisave · watching ⠋" {
		t.Fatalf("expected no text beside the header spinner, got %q", header)
	}

	working, _ := scanning.(watchModel).Update(watchWorkingMsg("Slay the Spire II"))
	if view := ansi.Strip(working.(watchModel).View()); strings.Contains(view, "scanning") {
		t.Fatalf("expected the header's phase spent once a game is in hand, got:\n%s", view)
	}

	// The work names the game it is checking; the row already does, so the
	// row says the verb alone.
	checking, _ := working.(watchModel).Update(watchPhaseMsg("checking Slay the Spire II"))
	view := ansi.Strip(checking.(watchModel).View())
	if !strings.Contains(view, "⠋ Slay the Spire II  Save 1 · checking") {
		t.Fatalf("expected the row to say what is happening to it, got:\n%s", view)
	}
	if !strings.Contains(view, "✓ Chrono Trigger     Save 2 · synced 4m ago") {
		t.Fatalf("expected every other row left settled, got:\n%s", view)
	}

	downloading, _ := checking.(watchModel).Update(watchPhaseMsg("downloading (1/3)"))
	if view := ansi.Strip(downloading.(watchModel).View()); !strings.Contains(view, "Save 1 · downloading (1/3)") {
		t.Fatalf("expected the transfer's own words on the row, got:\n%s", view)
	}

	// A phase belongs to the game it was reported under and never carries
	// over to the next one.
	next, _ := downloading.(watchModel).Update(watchWorkingMsg("Chrono Trigger"))
	view = ansi.Strip(next.(watchModel).View())
	if strings.Contains(view, "downloading") {
		t.Fatalf("expected the phase spent with its game, got:\n%s", view)
	}
	if !strings.Contains(view, "✓ Slay the Spire II  Save 1 · synced 4m ago") {
		t.Fatalf("expected the settled status back once the pass moved on, got:\n%s", view)
	}
}

// A pass that ends holds nothing, so the mark cannot outlive it — a lost
// clear would otherwise leave a row spinning until the next pass.
func TestAFinishedPassClearsTheWorkedRow(t *testing.T) {
	model := newWatchModel(WatchConfig{
		ServerURL: "http://localhost:8081",
		Initial: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now()},
		}},
	}, make(chan WatchRequest, 1))

	working, _ := model.Update(watchWorkingMsg("Slay the Spire 2"))
	settled, _ := working.(watchModel).Update(watchPassFinishedMsg{result: PassResult{
		Snapshot: ReportSnapshot{Games: []GameStatus{
			{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now()},
		}},
		At: time.Now(),
	}})

	if view := ansi.Strip(settled.(watchModel).View()); !strings.Contains(view, "✓ Slay the Spire 2") {
		t.Fatalf("expected the state glyph back once the pass finished, got:\n%s", view)
	}
}

func divergedSnapshot() ReportSnapshot {
	return ReportSnapshot{Games: []GameStatus{
		{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now()},
		{
			Glyph:   "○",
			Title:   "Project Zomboid",
			Events:  []string{"Save diverged from Save 2, run omnisave track to resolve"},
			Pending: &PendingDecision{
				Kind: PendingDiverged, OmnisaveName: "Save 2",
				ForkName: "Save 2 (Steam Deck)", Keep: KeepAsBranch,
			},
		},
	}}
}

func settledWatchModel(requests chan WatchRequest, snapshot ReportSnapshot) watchModel {
	model := newTestWatchModel(requests)
	settled, _ := model.Update(watchPassFinishedMsg{result: PassResult{Snapshot: snapshot, At: time.Now()}})
	return settled.(watchModel)
}

// The resolve key is offered only when a save is actually waiting on it.
func TestTheResolveKeyAppearsOnlyWhileASaveIsWaiting(t *testing.T) {
	quiet := settledWatchModel(make(chan WatchRequest, 4), ReportSnapshot{Games: []GameStatus{
		{Glyph: "✓", Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: time.Now()},
	}})
	if view := ansi.Strip(quiet.View()); !strings.Contains(view, "s sync now · q quit") {
		t.Fatalf("expected no resolve key with nothing waiting, got:\n%s", view)
	}

	waiting := settledWatchModel(make(chan WatchRequest, 4), divergedSnapshot())
	if view := ansi.Strip(waiting.View()); !strings.Contains(view, "s sync now · r resolve · q quit") {
		t.Fatalf("expected the resolve key while a save waits, got:\n%s", view)
	}
}

// The question takes over the pinned block: the table, the footer clock, and
// the usual keys all step aside so the modal is the only thing asking.
func TestTheQuestionTakesOverTheBlock(t *testing.T) {
	requests := make(chan WatchRequest, 4)
	model := settledWatchModel(requests, divergedSnapshot())

	asked, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	view := ansi.Strip(asked.(watchModel).View())
	for _, text := range []string{
		"▲ Omnisave · watching",
		"Project Zomboid",
		"Save 2 diverged from the server",
		"› Fork here",
		"continue as Save 2 (Steam Deck)",
		"Take current",
		"↑↓ choose · enter confirm · esc dismiss",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the question to contain %q, got:\n%s", text, view)
		}
	}
	for _, gone := range []string{"Slay the Spire 2", "watching 0 save paths", "s sync now"} {
		if strings.Contains(view, gone) {
			t.Fatalf("expected the question to replace %q, got:\n%s", gone, view)
		}
	}
	if request := <-requests; request.Kind != WatchQuestionOpened {
		t.Fatalf("expected the loop to be told a question opened, got %v", request)
	}
}

// A pass owns the table it is rebuilding, so the question waits for it —
// the same rule that already holds the sync key.
func TestTheQuestionWaitsForAPassInFlight(t *testing.T) {
	requests := make(chan WatchRequest, 4)
	model := settledWatchModel(requests, divergedSnapshot())
	syncing, _ := model.Update(watchPassStartedMsg{at: time.Now()})

	asked, _ := syncing.(watchModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if asked.(watchModel).question != nil {
		t.Fatal("expected a pass in flight to hold the question back")
	}
	select {
	case request := <-requests:
		t.Fatalf("expected no request while a pass runs, got %v", request)
	default:
	}
}

// Enter records the answer against the pair the question showed; the loop
// replays it, so the view itself resolves nothing.
func TestAnsweringSendsTheChoiceAndTheSaveItBelongsTo(t *testing.T) {
	requests := make(chan WatchRequest, 4)
	model := settledWatchModel(requests, divergedSnapshot())
	asked, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	<-requests

	moved, _ := asked.(watchModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	answered, _ := moved.(watchModel).Update(tea.KeyMsg{Type: tea.KeyEnter})

	if answered.(watchModel).question != nil {
		t.Fatal("expected answering to close the question")
	}
	request := <-requests
	if request.Kind != WatchAnswered {
		t.Fatalf("expected an answer, got %v", request)
	}
	if request.Title != "Project Zomboid" || request.Omnisave != "Save 2" {
		t.Fatalf("expected the answer to name the save it belongs to, got %v", request)
	}
	if request.Diverged != DivergedBindingJump {
		t.Fatalf("expected the second option to answer take-current, got %q", request.Diverged)
	}
}

// Escape decides nothing: the row is left exactly as it was, and the loop
// is released to keep syncing.
func TestDismissingTheQuestionDecidesNothing(t *testing.T) {
	requests := make(chan WatchRequest, 4)
	model := settledWatchModel(requests, divergedSnapshot())
	asked, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	<-requests

	dismissed, _ := asked.(watchModel).Update(tea.KeyMsg{Type: tea.KeyEsc})

	if dismissed.(watchModel).question != nil {
		t.Fatal("expected escape to close the question")
	}
	if request := <-requests; request.Kind != WatchQuestionDismissed {
		t.Fatalf("expected a dismissal, got %v", request)
	}
	view := ansi.Strip(dismissed.(watchModel).View())
	if !strings.Contains(view, "Save 2 · diverged") {
		t.Fatalf("expected the waiting row to survive a dismissal, got:\n%s", view)
	}
}
