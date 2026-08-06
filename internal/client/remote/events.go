package remote

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// LibraryChangedEvent announces server-side movement worth a reconcile pass:
// a commit, a restore, a fork, a deletion — anything that changes what a
// device would sync against.
const LibraryChangedEvent = "library.changed"

const (
	eventsRetryFloor   = time.Second
	eventsRetryCeiling = time.Minute
)

// ServerEvents subscribes to the server's change feed and delivers event
// types until ctx ends, when the channel closes. A dropped stream reconnects
// with backoff. Every connection opens with the server's checkpoint event, so
// a consumer that reconciles on each delivery also reconciles after an
// outage, covering whatever the outage swallowed.
func (c *Client) ServerEvents(ctx context.Context) <-chan string {
	events := make(chan string)
	// The stream stays open indefinitely, so it cannot ride a client whose
	// Timeout bounds whole requests; ctx is what ends it.
	stream := &http.Client{Transport: c.httpClient.Transport}
	go func() {
		defer close(events)
		delay := eventsRetryFloor
		for {
			connected := c.streamServerEvents(ctx, stream, events)
			if ctx.Err() != nil {
				return
			}
			if connected {
				delay = eventsRetryFloor
			} else {
				delay = min(delay*2, eventsRetryCeiling)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
	return events
}

// streamServerEvents runs one connection until it fails or ctx ends, and
// reports whether the server accepted it — what decides if the retry delay
// resets or keeps growing.
func (c *Client) streamServerEvents(ctx context.Context, stream *http.Client, events chan<- string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/events", nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/event-stream")
	response, err := stream.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return false
	}

	scanner := bufio.NewScanner(response.Body)
	eventType := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case line == "":
			// A blank line ends one server-sent event; keepalive comments
			// carry no event: line and dispatch nothing.
			if eventType == "" {
				continue
			}
			select {
			case events <- eventType:
			case <-ctx.Done():
				return true
			}
			eventType = ""
		}
	}
	return true
}
