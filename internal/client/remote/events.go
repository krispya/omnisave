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
	// eventsSilenceLimit is how long a stream may say nothing before it is
	// treated as dead. The server sends a keepalive every 15 seconds, so a
	// live stream is never quiet for long and this is several missed ones.
	//
	// It exists for the case a handheld makes ordinary: a device suspends,
	// its peer goes away without ever saying so, and it wakes holding a
	// socket that will never deliver another byte and will never report an
	// error either. A read on that socket blocks for as long as the kernel
	// keeps it, which is far longer than anyone waits for their save. Silence
	// is the only evidence such a stream leaves, so silence is what ends it.
	eventsSilenceLimit = 45 * time.Second
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

// silenceLimit is how long this client waits on a quiet stream before
// redialing it.
func (c *Client) silenceLimit() time.Duration {
	if c.eventSilence > 0 {
		return c.eventSilence
	}
	return eventsSilenceLimit
}

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
	// Cancelling this request is the only way to end a read that will never
	// return on its own, so the silence watchdog gets a context of its own to
	// cancel. It is armed before the request rather than after it, because a
	// dial that hangs is the same problem arriving one step earlier.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.baseURL+"/api/v1/events", nil)
	if err != nil {
		return streamFailed
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/event-stream")
	silence := time.AfterFunc(c.silenceLimit(), cancel)
	defer silence.Stop()
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
		// Every line proves the stream is alive, keepalive comments included —
		// that is what the server sends them for.
		silence.Reset(c.silenceLimit())
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
