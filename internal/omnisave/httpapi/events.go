package httpapi

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

const (
	libraryChangedEvent = omnisave.LibraryChangedEvent
	// A pending request is worth watching for: it is visible only while it
	// lives, and an owner staring at the Dash should not have to reload to
	// see the Device in their hands ask, or to watch it expire.
	accessChangedEvent = omnisave.AccessChangedEvent
	eventHeartbeat     = 15 * time.Second
)

type serverEvent struct {
	ID   uint64
	Type string
}

// subscription holds what one reader has not caught up on yet.
//
// Pending events are kept per scope rather than in a queue, which is what makes
// "at least once per scope" true for a reader that is behind: repeated
// invalidations of one scope coalesce into the latest, and a second scope
// arriving never displaces the first. A one-slot signal wakes the reader
// without the broker ever blocking on it.
type subscription struct {
	mu      sync.Mutex
	pending map[string]serverEvent
	signal  chan struct{}
	closed  bool
}

func (s *subscription) offer(event serverEvent) {
	s.mu.Lock()
	if !s.closed {
		s.pending[event.Type] = event
	}
	s.mu.Unlock()

	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// take returns everything owed to this reader, oldest scope first.
func (s *subscription) take() []serverEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := make([]serverEvent, 0, len(s.pending))
	for _, event := range s.pending {
		events = append(events, event)
	}
	clear(s.pending)
	slices.SortFunc(events, func(left, right serverEvent) int {
		return cmp.Compare(left.ID, right.ID)
	})
	return events
}

type eventBroker struct {
	mu          sync.Mutex
	latestID    uint64
	nextID      uint64
	subscribers map[uint64]*subscription
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: make(map[uint64]*subscription)}
}

func (b *eventBroker) publish(eventType string) {
	b.mu.Lock()
	b.latestID++
	event := serverEvent{ID: b.latestID, Type: eventType}
	subscribers := make([]*subscription, 0, len(b.subscribers))
	for _, subscriber := range b.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	b.mu.Unlock()

	for _, subscriber := range subscribers {
		subscriber.offer(event)
	}
}

func (b *eventBroker) subscribe() (*subscription, serverEvent, func()) {
	b.mu.Lock()
	b.nextID++
	subscriberID := b.nextID
	subscriber := &subscription{
		pending: make(map[string]serverEvent),
		signal:  make(chan struct{}, 1),
	}
	b.subscribers[subscriberID] = subscriber
	checkpoint := serverEvent{ID: b.latestID, Type: libraryChangedEvent}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, subscriberID)
			b.mu.Unlock()

			subscriber.mu.Lock()
			subscriber.closed = true
			subscriber.mu.Unlock()
			close(subscriber.signal)
		})
	}
	return subscriber, checkpoint, unsubscribe
}

func (a *API) publishLibraryChanged() {
	a.events.publish(libraryChangedEvent)
}

func (a *API) publishAccessChanged() {
	a.events.publish(accessChangedEvent)
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

	subscriber, checkpoint, unsubscribe := a.events.subscribe()
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
		case _, open := <-subscriber.signal:
			if !open {
				return
			}
			for _, event := range subscriber.take() {
				if writeServerEvent(w, event) != nil {
					return
				}
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
