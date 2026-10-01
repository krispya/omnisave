package savesync

import "errors"

// Prompts put the questions only a person can answer. Each is optional: a
// nil prompt leaves its question for a later interactive run, which is what
// a headless pass wants, and a pass can answer some questions and not others
// — the watch view replays one divergence answer and nothing else. The zero
// value answers nothing.
//
// An error a prompt returns ends the pass unchanged, so a caller can tell a
// person calling off the run from a failure.
type Prompts struct {
	// SyncToDevice picks which server save to place on a Device that has no
	// local save for the game (FDR-003). An empty choice places nothing.
	SyncToDevice func(gameTitle string, options []SyncToDeviceOption) (SyncToDeviceChoice, error)
	// Stale resolves a Local Save matching exactly one Omnisave at a
	// revision that is not its Current Revision.
	// It is answered like a divergence, with a DivergedChoice.
	Stale func(question StaleQuestion) (DivergedChoice, error)
	// Ambiguous resolves a Local Save matching zero or several Omnisaves.
	Ambiguous func(gameTitle string, options []AmbiguousOption) (AmbiguousChoice, error)
	// Diverged resolves a bound save with progress on both sides (FDR-005,
	// decision 4). It may return ErrUnanswered to leave this one save
	// waiting, exactly as a nil prompt would.
	Diverged func(question DivergedQuestion) (DivergedChoice, error)
	// HeldSeed asks before seeding a new lineage for a game whose existing
	// lineages are held for migration; false, or no prompt, waits.
	HeldSeed func(gameTitle string) (bool, error)
}

// ErrUnanswered is how a prompt says it holds no answer for the question it
// was given. The pass leaves that save waiting, which is what a pass with no
// prompts at all does with every question it meets.
var ErrUnanswered = errors.New("no answer for this save")

// SyncToDeviceOption is one server save that can be placed on a Device with
// no local save.
type SyncToDeviceOption struct {
	OmnisaveID string
	Name       string
}

// SyncToDeviceChoice is empty when the person decides not to place a save.
type SyncToDeviceChoice struct {
	OmnisaveID string
}

// StaleQuestion is one stale save put to a person: the game, the Omnisave it
// matches a non-current revision of, and the name forking would create. The
// save may sit behind the Current Revision, or ahead of it after a restore.
type StaleQuestion struct {
	GameTitle    string
	OmnisaveName string
	// ForkName is the deconflict name the fork would carry ("Save 1 (Steam
	// Deck)"); empty when the Device is unnamed and the server's default
	// applies.
	ForkName string
}

// AmbiguousOption is one existing save a Local Save could sync with.
// MatchedRevisionID names the revision holding the Local Save's exact
// content, and is empty when adopting the save would replace that content.
type AmbiguousOption struct {
	OmnisaveID        string
	Name              string
	MatchedRevisionID string
}

// AmbiguousChoice resolves a Local Save that matches zero or several saves.
// Create makes the Local Save a new save instead of choosing one. UseLocal,
// for a chosen save the content did not match, makes the local content its
// Current Revision instead of applying that save's current here; a matched
// save already holds the content and ignores it.
type AmbiguousChoice struct {
	OmnisaveID string
	Create     bool
	UseLocal   bool
}

// DivergedQuestion is one diverged save put to a person: the game, the
// Omnisave that diverged, and the name forking would create, so the answer
// names the save it actually produces.
type DivergedQuestion struct {
	GameTitle    string
	OmnisaveName string
	// ForkName is the deconflict name a fork or seed would carry ("Save 1
	// (Steam Deck)"); empty when the Device is unnamed and the server's
	// default applies.
	ForkName string
}

// DivergedChoice answers a diverged save, and a stale one, which is asked
// the same way. Every answer keeps both sides recoverable: forking continues
// the local progress as its own lineage; jumping and using the local content
// both stay on the Omnisave and differ in which side's content becomes
// current. Whichever side loses stays in the Omnisave's history (FDR-005,
// decision 4; FDR-003, decision 5).
type DivergedChoice string

const (
	// DivergedFork continues this Device's progress as a new lineage.
	DivergedFork DivergedChoice = "fork"
	// DivergedJump takes the Current Revision, keeping any unsynced local
	// progress as a branch first.
	DivergedJump DivergedChoice = "jump"
	// DivergedUseLocal commits this Device's content on top of the Current
	// Revision and makes it current, leaving local files untouched.
	DivergedUseLocal DivergedChoice = "use-local"
)
