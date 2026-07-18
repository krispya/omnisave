package httpapi

import "testing"

func TestEventBrokerCoalescesForSlowSubscribers(t *testing.T) {
	broker := newEventBroker()
	events, _, unsubscribe := broker.subscribe()
	defer unsubscribe()

	broker.publish(libraryChangedEvent)
	broker.publish(libraryChangedEvent)
	if event := <-events; event.ID != 2 {
		t.Fatalf("slow subscriber received stale event: %+v", event)
	}
}
