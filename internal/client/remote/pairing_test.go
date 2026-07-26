package remote_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
)

func TestPairingAsksAndPollsWithoutACredential(t *testing.T) {
	ctx := context.Background()
	var authorization string
	var requested access.RequestPairing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pairing/requests":
			json.NewDecoder(r.Body).Decode(&requested)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(access.PairingTicket{Code: "4F2KQP", Handle: "long-handle"})
		case "/api/v1/pairing/collect":
			var body struct {
				Handle string `json:"handle"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Handle != "long-handle" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(access.Collection{
				Status: access.CollectionApproved, Token: "issued-credential",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	ticket, err := remote.RequestPairing(ctx, server.URL, access.RequestPairing{
		DeviceID: "deck-1", DeviceName: "steamdeck", Platform: "linux",
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Code != "4F2KQP" || ticket.Handle != "long-handle" {
		t.Fatalf("unexpected ticket: %+v", ticket)
	}
	if requested.DeviceID != "deck-1" || requested.DeviceName != "steamdeck" {
		t.Fatalf("the request did not carry this Device's identity: %+v", requested)
	}
	// A client with no credential is exactly who calls these, so nothing
	// should be sending an Authorization header it does not have.
	if authorization != "" {
		t.Fatalf("pairing sent %q", authorization)
	}

	collection, err := remote.CollectPairing(ctx, server.URL, ticket.Handle, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if collection.Status != access.CollectionApproved || collection.Token != "issued-credential" {
		t.Fatalf("unexpected collection: %+v", collection)
	}
}

func TestCollectingWithAnUnknownHandleReportsTheResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, err := remote.CollectPairing(context.Background(), server.URL, "spent", server.Client())
	var response *remote.ResponseError
	if !errors.As(err, &response) || response.StatusCode != http.StatusNotFound {
		t.Fatalf("a spent handle reported %v", err)
	}
}
