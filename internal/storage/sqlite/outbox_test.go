package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// SQLite, not a process-local mutex, owns projection order. Once one
// repository selects the oldest action, a second repository cannot apply any
// action until the first durable write and queue removal finish.
func TestStoreOutboxProjectionOrderSpansRepositoryInstances(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	first, err := Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	tx, err := first.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []store.Omnisave{
		{ID: "save-1", DisplayName: "Older state"},
		{ID: "save-1", DisplayName: "Newer state"},
	} {
		if err := enqueueStoreAction(ctx, tx, storeActionRecordOmnisave, record.ID, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	firstApplied := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan flushResult, 1)
	go func() {
		flushed, err := first.flushNextStoreAction(ctx, func(
			ctx context.Context,
			kind, targetID, payload string,
		) error {
			if err := first.applyStoreAction(ctx, kind, targetID, payload); err != nil {
				return err
			}
			close(firstApplied)
			<-releaseFirst
			return nil
		})
		firstDone <- flushResult{flushed: flushed, err: err}
	}()

	select {
	case <-firstApplied:
	case <-time.After(2 * time.Second):
		t.Fatal("the first repository did not begin projecting the oldest action")
	}

	secondApplied := make(chan struct{}, 1)
	secondDone := make(chan flushResult, 1)
	go func() {
		flushed, err := second.flushNextStoreAction(ctx, func(
			ctx context.Context,
			kind, targetID, payload string,
		) error {
			if err := second.applyStoreAction(ctx, kind, targetID, payload); err != nil {
				return err
			}
			secondApplied <- struct{}{}
			return nil
		})
		secondDone <- flushResult{flushed: flushed, err: err}
	}()

	var overtook bool
	select {
	case <-secondApplied:
		overtook = true
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)

	for name, result := range map[string]flushResult{
		"first":  awaitFlush(t, firstDone),
		"second": awaitFlush(t, secondDone),
	} {
		if result.err != nil {
			t.Fatalf("%s projection failed: %v", name, result.err)
		}
		if !result.flushed {
			t.Fatalf("%s repository did not project an action", name)
		}
	}
	if overtook {
		t.Fatal("the second repository projected before the first released its database claim")
	}

	record, err := first.store.GetOmnisave("save-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.DisplayName != "Newer state" {
		t.Fatalf("expected the newer projection to remain in the store, got %q", record.DisplayName)
	}
	var queued int
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_outbox`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("expected both projected actions to leave the queue, got %d", queued)
	}
}

type flushResult struct {
	flushed bool
	err     error
}

func awaitFlush(t *testing.T, result <-chan flushResult) flushResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for outbox projection")
		return flushResult{}
	}
}
