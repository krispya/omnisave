package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A device that suspends wakes holding a stream whose peer went away without
// saying so: no bytes arrive, no error arrives, and the read blocks for as
// long as the kernel keeps the socket. Nothing else in the client notices,
// and the periodic pull becomes the only way server-side movement is ever
// seen again. Silence is the only evidence such a stream leaves, so the
// client has to be the one to end it.
func TestASilentStreamIsRedialed(t *testing.T) {
	var connections atomic.Int64
	redialed := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		// Every connection opens normally and then says nothing at all — no
		// events and, unlike a healthy server, no keepalives either.
		fmt.Fprint(w, "id: 1\nevent: library.changed\ndata: {}\n\n")
		flusher.Flush()
		if connections.Add(1) > 1 {
			select {
			case redialed <- struct{}{}:
			default:
			}
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.eventSilence = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drain(client.ServerEvents(ctx))

	select {
	case <-redialed:
	case <-time.After(10 * time.Second):
		t.Fatalf("a silent stream was never redialed; %d connection(s) made", connections.Load())
	}
}

// drain takes delivered events the way the watch loop does. The channel is
// unbuffered, so a test that ignores it stalls the stream on its first event
// and proves nothing about what the stream does next.
func drain(events <-chan string) {
	go func() {
		for range events {
		}
	}()
}

// Delivery waits on the watch loop, and the watch loop waits on its passes: a
// pass can hold the loop past the silence limit while the stream stands ready
// with an event. That is a slow reader, not a dead peer, and the stream must
// not be torn down for it — the keepalives it cannot scan meanwhile are
// sitting in the socket, not missing.
func TestABlockedDeliveryIsNotMistakenForSilence(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "id: 1\nevent: %s\ndata: {}\n\n", LibraryChangedEvent)
		flusher.Flush()
		heartbeat := time.NewTicker(20 * time.Millisecond)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.eventSilence = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := client.ServerEvents(ctx)

	// The consumer is mid-pass for several silence windows before it takes
	// the delivery the stream is holding out.
	time.Sleep(700 * time.Millisecond)
	select {
	case delivered := <-events:
		if delivered != LibraryChangedEvent {
			t.Fatalf("expected the held event delivered, got %q", delivered)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the held event")
	}
	drain(events)

	// Listening has resumed; the keepalives keep the stream alive from here.
	time.Sleep(400 * time.Millisecond)
	if made := connections.Load(); made != 1 {
		t.Errorf("a stream waiting on its reader was redialed: %d connections", made)
	}
}

// The watchdog measures silence, not connection age: a stream that keeps
// speaking is a stream that is working, however long it has been open. Only
// keepalives arrive here, which is exactly what a healthy idle server sends.
func TestAStreamKeptAliveByKeepalivesIsNotRedialed(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\nevent: library.changed\ndata: {}\n\n")
		flusher.Flush()
		connections.Add(1)
		heartbeat := time.NewTicker(20 * time.Millisecond)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.eventSilence = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drain(client.ServerEvents(ctx))

	// Several silence windows pass, each one crossed by keepalives alone.
	time.Sleep(time.Second)
	if made := connections.Load(); made != 1 {
		t.Errorf("a stream carrying keepalives was redialed: %d connections", made)
	}
}
