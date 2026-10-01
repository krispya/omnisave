package savesync_test

import (
	"context"
	"errors"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// The client binary wires these into every pass.
var (
	_ savesync.Server   = (*remote.Client)(nil)
	_ savesync.Adapters = (*client.Scanner)(nil)
)

// A commit still expecting a moved Current Revision is refused in a way the
// domain recognizes without knowing the transport, and the typed conflict
// still carries where current actually is.
func TestACommitAgainstAMovedCurrentRevisionIsAConflict(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	bound, _ := fixture.State.BindingFor(fixture.Local())
	moved := savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	fixture.Write(t, "stale-progress")

	_, err := binding.Push(context.Background(), server, bound.OmnisaveID,
		fixture.Save, *bound.LastSyncedRevisionID, nil)

	if !errors.Is(err, omnisave.ErrConflict) {
		t.Fatalf("expected the refusal to match omnisave.ErrConflict, got %v", err)
	}
	var conflict *omnisave.CurrentRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a typed current revision conflict, got %v", err)
	}
	if conflict.ActualCurrentRevisionID == nil || *conflict.ActualCurrentRevisionID != moved.ID {
		t.Fatalf("expected the conflict to carry the moved revision, got %+v", conflict)
	}
}
