package remote

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// LibraryChangedEvent announces server-side movement worth a reconcile pass:
// a commit, a restore, a fork, a deletion — anything that changes what a
// device would sync against.
const LibraryChangedEvent = omnisave.LibraryChangedEvent

const (
	eventsRetryFloor   = time.Second
	eventsRetryCeiling = time.Minute
	// eventsSteadyAfter is how long a connection must live before the retry
	// delay resets: a server that accepts and then drops right away keeps
	// backing off instead of being redialed at the floor.
	eventsSteadyAfter = 30 * time.Second
	// eventsMaxLine caps one SSE line. Real lines are tiny; the cap keeps a
	// corrupt stream from growing the scanner without bound.
	eventsMaxLine = 1 << 20
)

// streamOutcome is how one connection ended — what the retry loop needs to
// pick the next delay, or to stop.
type streamOutcome int

const (
	// streamFailed never reached a healthy stream; the retry delay grows.
	streamFailed streamOutcome = iota
	// streamServed reached a stream that later ended; the delay resets
	// once the connection also lived long enough to count as steady.
	streamServed
	// streamRefused is an authoritative no — a bad token — that retrying
	// cannot fix.
	streamRefused
)

// ServerEvents delivers change types, reconnecting dropped streams with backoff.
// Checkpoints cover missed changes and are suppressed when already delivered.
func (c *Client) ServerEvents(ctx context.Context) <-chan string {
	events := make(chan string)
	// The event stream uses context cancellation instead of the API request timeout.
	stream := *c.httpClient
	stream.Timeout = 0
	go func() {
		defer close(events)
		delay := eventsRetryFloor
		var lastEventID uint64
		for {
			started := time.Now()
			outcome := c.streamServerEvents(ctx, &stream, events, &lastEventID)
			if ctx.Err() != nil || outcome == streamRefused {
				return
			}
			if outcome == streamServed && time.Since(started) >= eventsSteadyAfter {
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

// streamServerEvents runs one connection until it fails or ctx ends.
// lastEventID persists across connections: it is what recognizes a
// reconnect's checkpoint as already delivered instead of as movement.
func (c *Client) streamServerEvents(ctx context.Context, stream *http.Client, events chan<- string, lastEventID *uint64) streamOutcome {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/events", nil)
	if err != nil {
		return streamFailed
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/event-stream")
	response, err := stream.Do(request)
	if err != nil {
		return streamFailed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return streamRefused
		}
		return streamFailed
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 4096), eventsMaxLine)
	eventType := ""
	eventID, hasEventID := uint64(0), false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id:"):
			if id, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "id:")), 10, 64); err == nil {
				eventID, hasEventID = id, true
			}
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case line == "":
			// A blank line ends one server-sent event; keepalive comments
			// carry no event: line and dispatch nothing.
			deliver := eventType != ""
			if deliver && hasEventID {
				// An unchanged ID is a reconnect checkpoint that missed
				// nothing. Any moved ID is movement — even backward,
				// which is a server whose IDs started over.
				deliver = eventID != *lastEventID
				*lastEventID = eventID
			}
			delivered := eventType
			eventType, eventID, hasEventID = "", 0, false
			if !deliver {
				continue
			}
			select {
			case events <- delivered:
			case <-ctx.Done():
				return streamServed
			}
		}
	}
	return streamServed
}
