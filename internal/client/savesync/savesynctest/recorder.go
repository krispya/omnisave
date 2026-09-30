package savesynctest

import (
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// Event is one thing a pass reported, as data.
type Event struct {
	// Kind names the Reporter method that heard it, e.g. "SyncedWith".
	Kind     string
	Title    string
	Omnisave string
	// Detail is a method's other text: a canonical title, a fork name, or a
	// skipped registration's reason.
	Detail string
	Count  int
	Names  []string
	Err    error
}

// Recorder is a savesync.Reporter that keeps everything a pass reported, in
// order. It is not safe for concurrent use; a pass reports from one goroutine.
type Recorder struct {
	Events []Event
	// Worked is every title a pass took in hand, with "" for each time it
	// let go of one.
	Worked []string
}

var _ savesync.Reporter = (*Recorder)(nil)

// Of lists the events of one kind, in order.
func (r *Recorder) Of(kind string) []Event {
	var events []Event
	for _, event := range r.Events {
		if event.Kind == kind {
			events = append(events, event)
		}
	}
	return events
}

func (r *Recorder) add(event Event) { r.Events = append(r.Events, event) }

func (r *Recorder) Working(title string) { r.Worked = append(r.Worked, title) }
func (r *Recorder) Idle()                { r.Worked = append(r.Worked, "") }

func (r *Recorder) Added(title, canonical string) {
	r.add(Event{Kind: "Added", Title: title, Detail: canonical})
}

func (r *Recorder) Linked(title, canonical string) {
	r.add(Event{Kind: "Linked", Title: title, Detail: canonical})
}

func (r *Recorder) Pending(title string)         { r.add(Event{Kind: "Pending", Title: title}) }
func (r *Recorder) Removed(title string)         { r.add(Event{Kind: "Removed", Title: title}) }
func (r *Recorder) DeletedOnServer(title string) { r.add(Event{Kind: "DeletedOnServer", Title: title}) }

func (r *Recorder) Failed(title string, err error) {
	r.add(Event{Kind: "Failed", Title: title, Err: err})
}

func (r *Recorder) SyncFailed(err error)    { r.add(Event{Kind: "SyncFailed", Err: err}) }
func (r *Recorder) BindingFailed(err error) { r.add(Event{Kind: "BindingFailed", Err: err}) }
func (r *Recorder) SaveDeleted(title string) {
	r.add(Event{Kind: "SaveDeleted", Title: title})
}
func (r *Recorder) NoSave(title string)        { r.add(Event{Kind: "NoSave", Title: title}) }
func (r *Recorder) SaveAvailable(title string) { r.add(Event{Kind: "SaveAvailable", Title: title}) }
func (r *Recorder) SaveLocationUnavailable(title string) {
	r.add(Event{Kind: "SaveLocationUnavailable", Title: title})
}

func (r *Recorder) PreservedAs(title, omnisaveName string) {
	r.add(Event{Kind: "PreservedAs", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) Stale(title, omnisaveName string) {
	r.add(Event{Kind: "Stale", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) Unbound(title string) { r.add(Event{Kind: "Unbound", Title: title}) }

func (r *Recorder) SyncedWith(title, omnisaveName string, _ time.Time) {
	r.add(Event{Kind: "SyncedWith", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) CurrentMoved(title, omnisaveName string) {
	r.add(Event{Kind: "CurrentMoved", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) PullDeferred(title, omnisaveName string) {
	r.add(Event{Kind: "PullDeferred", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) Branched(title, omnisaveName string) {
	r.add(Event{Kind: "Branched", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) BranchKept(title, omnisaveName string) {
	r.add(Event{Kind: "BranchKept", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) Forked(title, omnisaveName string) {
	r.add(Event{Kind: "Forked", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) Diverged(title, omnisaveName, forkName string) {
	r.add(Event{Kind: "Diverged", Title: title, Omnisave: omnisaveName, Detail: forkName})
}

func (r *Recorder) SaveFailed(title string, err error) {
	r.add(Event{Kind: "SaveFailed", Title: title, Err: err})
}

func (r *Recorder) Migrated(title, omnisaveName string) {
	r.add(Event{Kind: "Migrated", Title: title, Omnisave: omnisaveName})
}

func (r *Recorder) MigrationHeld(title, omnisaveName string, reason error) {
	r.add(Event{Kind: "MigrationHeld", Title: title, Omnisave: omnisaveName, Err: reason})
}

func (r *Recorder) StoreRegistered(title string, count int) {
	r.add(Event{Kind: "StoreRegistered", Title: title, Count: count})
}

func (r *Recorder) StoreRegistrationSkipped(title, reason string) {
	r.add(Event{Kind: "StoreRegistrationSkipped", Title: title, Detail: reason})
}

func (r *Recorder) StoreRegistrationFailed(title string, err error) {
	r.add(Event{Kind: "StoreRegistrationFailed", Title: title, Err: err})
}

func (r *Recorder) StoreRegistrationIncomplete(title string, count int) {
	r.add(Event{Kind: "StoreRegistrationIncomplete", Title: title, Count: count})
}

func (r *Recorder) StoreDeleted(title string, deleted int) {
	r.add(Event{Kind: "StoreDeleted", Title: title, Count: deleted})
}

func (r *Recorder) StoreExtras(title string, extras int) {
	r.add(Event{Kind: "StoreExtras", Title: title, Count: extras})
}

func (r *Recorder) Unlocked(title string, names []string) {
	r.add(Event{Kind: "Unlocked", Title: title, Names: names})
}
