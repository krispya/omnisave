package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestLibraryEventsFollowSuccessfulSaveChanges(t *testing.T) {
	server := newEventServer(t, storagetest.NewMemoryRepository())
	stream := openEventStream(t, server)
	if checkpoint := readEvent(t, stream); checkpoint.ID != 0 {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}

	response := eventRequest(t, server, http.MethodPost, "/api/v1/omnisaves", `{}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid create returned %d", response.StatusCode)
	}
	response.Body.Close()

	response = eventRequest(t, server, http.MethodPost, "/api/v1/omnisaves", `{"game_id":"chrono-trigger"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create returned %d", response.StatusCode)
	}
	response.Body.Close()
	if event := readEvent(t, stream); event.ID != 1 || event.Type != "library.changed" {
		t.Fatalf("unexpected create event: %+v", event)
	}
}

func TestLibraryEventsIncludeProvenanceChanges(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	server := newEventServer(t, repository)
	stream := openEventStream(t, server)
	_ = readEvent(t, stream)

	response := eventRequest(t, server, http.MethodPost, "/api/v1/games/resolve", `{
		"fingerprints":[{"platform":"snes","algorithm":"sha1","value":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"}]
	}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resolve returned %d", response.StatusCode)
	}
	var resolved struct {
		Game struct {
			ID string `json:"id"`
		} `json:"game"`
	}
	if err := json.NewDecoder(response.Body).Decode(&resolved); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	assertEventID(t, stream, 1)

	response = eventRequest(t, server, http.MethodPut, "/api/v1/devices/steam-deck", `{"name":"Steam Deck","platform":"linux"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register returned %d", response.StatusCode)
	}
	response.Body.Close()
	assertEventID(t, stream, 2)

	trackingPath := "/api/v1/games/" + resolved.Game.ID + "/tracking/steam-deck"
	response = eventRequest(t, server, http.MethodPut, trackingPath, `{"adapter":"retroarch","installed":true}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("track returned %d", response.StatusCode)
	}
	response.Body.Close()
	assertEventID(t, stream, 3)

	response = eventRequest(t, server, http.MethodDelete, trackingPath, "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("untrack returned %d", response.StatusCode)
	}
	response.Body.Close()
	assertEventID(t, stream, 4)
}

type streamedEvent struct {
	ID   uint64
	Type string
}

func newEventServer(t *testing.T, repository *storagetest.MemoryRepository) *httptest.Server {
	t.Helper()
	handler := httpapi.BearerAuth("secret", httpapi.New(
		omnisaveservice.New(repository),
		catalogservice.New(repository, repository, catalogProviderStub{}),
	))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func openEventStream(t *testing.T, server *httptest.Server) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("event stream returned %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	t.Cleanup(func() {
		cancel()
		response.Body.Close()
	})
	return bufio.NewReader(response.Body)
}

func eventRequest(t *testing.T, server *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	var payload io.Reader
	if body != "" {
		payload = bytes.NewBufferString(body)
	}
	request, err := http.NewRequest(method, server.URL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertEventID(t *testing.T, stream *bufio.Reader, expected uint64) {
	t.Helper()
	event := readEvent(t, stream)
	if event.ID != expected || event.Type != "library.changed" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func readEvent(t *testing.T, stream *bufio.Reader) streamedEvent {
	t.Helper()
	result := make(chan struct {
		event streamedEvent
		err   error
	}, 1)
	go func() {
		event, err := readEventFrame(stream)
		result <- struct {
			event streamedEvent
			err   error
		}{event: event, err: err}
	}()
	select {
	case frame := <-result:
		if frame.err != nil {
			t.Fatal(frame.err)
		}
		return frame.event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server event")
		return streamedEvent{}
	}
}

func readEventFrame(stream *bufio.Reader) (streamedEvent, error) {
	event := streamedEvent{}
	for {
		line, err := stream.ReadString('\n')
		if err != nil {
			return streamedEvent{}, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return event, nil
		}
		if value, found := strings.CutPrefix(line, "id: "); found {
			event.ID, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				return streamedEvent{}, err
			}
		}
		if value, found := strings.CutPrefix(line, "event: "); found {
			event.Type = value
		}
	}
}
