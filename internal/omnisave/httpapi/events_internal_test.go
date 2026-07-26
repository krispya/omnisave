package httpapi

import "testing"

func TestEventBrokerCoalescesForSlowSubscribers(t *testing.T) {
	broker := newEventBroker()
	subscriber, _, unsubscribe := broker.subscribe()
	defer unsubscribe()

	broker.publish(libraryChangedEvent)
	broker.publish(libraryChangedEvent)

	<-subscriber.signal
	events := subscriber.take()
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("repeated invalidations of one scope did not coalesce: %+v", events)
	}
}

// A reader that is behind must not lose one scope because another changed:
// ADR-002 promises at least once per scope, and the Library and access views
// share a single connection.
func TestEventBrokerKeepsOneEventPerScopeForSlowSubscribers(t *testing.T) {
	broker := newEventBroker()
	subscriber, _, unsubscribe := broker.subscribe()
	defer unsubscribe()

	broker.publish(libraryChangedEvent)
	broker.publish(accessChangedEvent)

	<-subscriber.signal
	events := subscriber.take()
	if len(events) != 2 || events[0].Type != libraryChangedEvent || events[1].Type != accessChangedEvent {
		t.Fatalf("a scope was dropped for a slow subscriber: %+v", events)
	}
}
