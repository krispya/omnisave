package remote

import (
	"context"
	"net/http"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
)

// Pairing is how a client with no credential asks for one. These two calls are
// the only ones that reach a server unauthenticated, because a Device with no
// credential is exactly who makes them (ADR-007). Everything after them
// carries the credential this flow collects, through a Client.

// RequestPairing asks a server to pair, naming the Device identity this
// installation already self-identifies with. The answer carries a code to
// display and a handle to poll with.
func RequestPairing(
	ctx context.Context, serverURL string, input access.RequestPairing, httpClient *http.Client,
) (*access.PairingTicket, error) {
	var ticket access.PairingTicket
	if err := pair(ctx, serverURL, "/api/v1/pairing/requests", input, httpClient, &ticket); err != nil {
		return nil, err
	}
	return &ticket, nil
}

// CollectPairing asks whether the owner has answered yet. An approved request
// gives up its credential here, once.
func CollectPairing(
	ctx context.Context, serverURL, handle string, httpClient *http.Client,
) (*access.Collection, error) {
	var collection access.Collection
	body := struct {
		Handle string `json:"handle"`
	}{Handle: handle}
	if err := pair(ctx, serverURL, "/api/v1/pairing/collect", body, httpClient, &collection); err != nil {
		return nil, err
	}
	return &collection, nil
}

// pair sends one unauthenticated call to a server this client has no Client
// for yet: the address is still a string, and there is no token to send.
func pair(
	ctx context.Context, serverURL, path string, input any, httpClient *http.Client, result any,
) error {
	baseURL, err := NormalizeServerURL(serverURL)
	if err != nil {
		return err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return postJSON(ctx, httpClient, baseURL+path, "", input, result)
}
