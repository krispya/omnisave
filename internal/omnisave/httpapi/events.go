package httpapi

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	libraryChangedEvent = "library.changed"
	eventHeartbeat      = 15 * time.Second
)

type serverEvent struct {
	ID   uint64
	Type string
}

type eventBroker struct {
	mu          sync.Mutex
	latestID    uint64
	nextID      uint64
	subscribers map[uint64]chan serverEvent
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: make(map[uint64]chan serverEvent)}
}

func (b *eventBroker) publish(eventType string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latestID++
	event := serverEvent{ID: b.latestID, Type: eventType}
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- event:
			default:
			}
		}
	}
}

func (b *eventBroker) subscribe() (<-chan serverEvent, serverEvent, func()) {
	b.mu.Lock()
	b.nextID++
	subscriberID := b.nextID
	subscriber := make(chan serverEvent, 1)
	b.subscribers[subscriberID] = subscriber
	checkpoint := serverEvent{ID: b.latestID, Type: libraryChangedEvent}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, subscriberID)
			close(subscriber)
			b.mu.Unlock()
		})
	}
	return subscriber, checkpoint, unsubscribe
}

func (a *API) publishLibraryChanged() {
	a.events.publish(libraryChangedEvent)
}

func (a *API) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")

	events, checkpoint, unsubscribe := a.events.subscribe()
	defer unsubscribe()
	if err := writeServerEvent(w, checkpoint); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open || writeServerEvent(w, event) != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeServerEvent(w http.ResponseWriter, event serverEvent) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: {}\n\n", event.ID, event.Type)
	return err
}
