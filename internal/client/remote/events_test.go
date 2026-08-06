package remote_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/remote"
)

// newEventServer serves one scripted SSE connection per call and then ends
// it, counting connections so reconnect behavior is observable. hold keeps a
// connection open until the client hangs up.
func newEventServer(t *testing.T, serve func(connection int64, w http.ResponseWriter, flush, hold func())) (*remote.Client, *atomic.Int64) {
	t.Helper()
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("expected the configured API token on the event stream")
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		serve(connections.Add(1), w, flusher.Flush, func() { <-r.Context().Done() })
	}))
	t.Cleanup(server.Close)
	client, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &connections
}

func receiveEvent(t *testing.T, events <-chan string) string {
	t.Helper()
	select {
	case event, open := <-events:
		if !open {
			t.Fatal("expected an event before the stream closed")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a server event")
		return ""
	}
}

func TestServerEventsDeliversTypesAndIgnoresKeepalives(t *testing.T) {
	client, _ := newEventServer(t, func(connection int64, w http.ResponseWriter, flush, hold func()) {
		fmt.Fprint(w, "id: 1\nevent: library.changed\ndata: {}\n\n")
		fmt.Fprint(w, ": keepalive\n\n")
		fmt.Fprint(w, "id: 2\nevent: access.changed\ndata: {}\n\n")
		flush()
		// Hold the connection so the test never exercises reconnect.
		hold()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := client.ServerEvents(ctx)
	if event := receiveEvent(t, events); event != remote.LibraryChangedEvent {
		t.Fatalf("expected the checkpoint library event, got %q", event)
	}
	if event := receiveEvent(t, events); event != "access.changed" {
		t.Fatalf("expected the access event after the keepalive, got %q", event)
	}
}

func TestServerEventsReconnectsAfterTheStreamDrops(t *testing.T) {
	client, connections := newEventServer(t, func(connection int64, w http.ResponseWriter, flush, hold func()) {
		fmt.Fprintf(w, "id: %d\nevent: library.changed\ndata: {}\n\n", connection)
		flush()
		// Returning drops the connection right after the checkpoint.
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := client.ServerEvents(ctx)
	receiveEvent(t, events)
	receiveEvent(t, events)
	if connections.Load() < 2 {
		t.Fatalf("expected a dropped stream to reconnect, got %d connections", connections.Load())
	}
}

func TestServerEventsClosesWhenTheContextEnds(t *testing.T) {
	client, _ := newEventServer(t, func(connection int64, w http.ResponseWriter, flush, hold func()) {
		fmt.Fprint(w, "id: 1\nevent: library.changed\ndata: {}\n\n")
		flush()
		hold()
	})
	ctx, cancel := context.WithCancel(context.Background())

	events := client.ServerEvents(ctx)
	receiveEvent(t, events)
	cancel()

	select {
	case _, open := <-events:
		if open {
			// A late event already in flight is fine; the close must follow.
			if _, stillOpen := <-events; stillOpen {
				t.Fatal("expected the event channel to close after cancel")
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the event channel to close")
	}
}
