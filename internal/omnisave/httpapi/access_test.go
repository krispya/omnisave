package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/access"
	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	ownersettings "github.com/krisbaumgartner/omnisave/internal/settings"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

// The owner token the test server is configured with. It matches the one
// openEventStream authenticates with, so the SSE test can share the helper.
const owner = "secret"

func newConnectedServer(t *testing.T, pins map[string]string) http.Handler {
	t.Helper()
	repository := storagetest.NewMemoryRepository()
	return httpapi.New(accessservice.New(repository, owner), httpapi.Config{
		Saves:    omnisaveservice.New(repository),
		Catalog:  catalogservice.New(repository, repository, catalogProviderStub{}),
		Settings: ownersettings.New(repository, pins),
	})
}

func send(t *testing.T, handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Buffer
	if body == "" {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, path, reader)
	// httptest defaults to 192.0.2.1, which is a public documentation address.
	// These stand for the owner's own browser, which is on the network.
	request.RemoteAddr = "127.0.0.1:54321"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestOnlyThePairingEndpointsAnswerWithoutACredential(t *testing.T) {
	handler := newConnectedServer(t, nil)

	open := send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"deck-1","device_name":"steamdeck"}`)
	if open.Code != http.StatusCreated {
		t.Fatalf("asking to pair without a credential returned %d: %s", open.Code, open.Body.String())
	}

	// Everything else, including reading the pending requests it just made,
	// is behind a credential.
	for _, path := range []string{
		"/api/v1/pairing/requests",
		"/api/v1/credentials",
		"/api/v1/settings",
		"/api/v1/omnisaves",
		"/api/v1/games",
	} {
		response := send(t, handler, http.MethodGet, path, "", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s answered %d without a credential", path, response.Code)
		}
	}
}

func TestADeviceConnectsByBeingApprovedInTheDash(t *testing.T) {
	handler := newConnectedServer(t, nil)

	var ticket access.PairingTicket
	created := send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"deck-1","device_name":"steamdeck","platform":"linux"}`)
	decodeResponse(t, created, &ticket)
	if ticket.Code == "" || ticket.Handle == "" || ticket.Code == ticket.Handle {
		t.Fatalf("the ticket does not carry two distinct secrets: %+v", ticket)
	}

	var pending []access.PairingRequest
	listed := send(t, handler, http.MethodGet, "/api/v1/pairing/requests", owner, "")
	decodeResponse(t, listed, &pending)
	if len(pending) != 1 || pending[0].Code != ticket.Code {
		t.Fatalf("the Dash cannot match the code on the Device: %+v", pending)
	}
	if pending[0].DeviceName != "steamdeck" || pending[0].SourceAddress == "" {
		t.Fatalf("a pending request is missing what the owner reads: %+v", pending[0])
	}

	waiting := send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`)
	var collection access.Collection
	decodeResponse(t, waiting, &collection)
	if collection.Status != access.CollectionPending || collection.Token != "" {
		t.Fatalf("an unapproved poll answered %+v", collection)
	}

	approved := send(t, handler, http.MethodPost,
		"/api/v1/pairing/requests/"+pending[0].ID+"/approve", owner, "")
	if approved.Code != http.StatusNoContent {
		t.Fatalf("approve returned %d: %s", approved.Code, approved.Body.String())
	}

	collected := send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`)
	decodeResponse(t, collected, &collection)
	if collection.Status != access.CollectionApproved || collection.Token == "" {
		t.Fatalf("approval did not hand over a credential: %+v", collection)
	}

	// The Device now reaches the API on its own credential, and the owner
	// token is not part of anything it does.
	library := send(t, handler, http.MethodGet, "/api/v1/omnisaves", collection.Token, "")
	if library.Code != http.StatusOK {
		t.Fatalf("the connected Device was refused: %d", library.Code)
	}

	var credentials []access.Credential
	listedCredentials := send(t, handler, http.MethodGet, "/api/v1/credentials", owner, "")
	decodeResponse(t, listedCredentials, &credentials)
	if len(credentials) != 1 || credentials[0].DeviceName != "steamdeck" {
		t.Fatalf("the issued credential is not listed against its Device: %+v", credentials)
	}

	revoked := send(t, handler, http.MethodDelete, "/api/v1/credentials/"+credentials[0].ID, owner, "")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke returned %d: %s", revoked.Code, revoked.Body.String())
	}
	after := send(t, handler, http.MethodGet, "/api/v1/omnisaves", collection.Token, "")
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked Device still reached the API: %d", after.Code)
	}
}

func TestDenyingEndsTheRequestAndMintsNothing(t *testing.T) {
	handler := newConnectedServer(t, nil)
	var ticket access.PairingTicket
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"unknown-1","device_name":"unknown laptop"}`), &ticket)
	var pending []access.PairingRequest
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/pairing/requests", owner, ""), &pending)

	denied := send(t, handler, http.MethodPost,
		"/api/v1/pairing/requests/"+pending[0].ID+"/deny", owner, "")
	if denied.Code != http.StatusNoContent {
		t.Fatalf("deny returned %d: %s", denied.Code, denied.Body.String())
	}

	var collection access.Collection
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`), &collection)
	if collection.Status != access.CollectionDenied || collection.Token != "" {
		t.Fatalf("the refused client was told %+v", collection)
	}
	var credentials []access.Credential
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/credentials", owner, ""), &credentials)
	if len(credentials) != 0 {
		t.Fatalf("denial issued %d credentials", len(credentials))
	}
}

func TestACollectedCredentialCannotBeCollectedAgain(t *testing.T) {
	handler := newConnectedServer(t, nil)
	var ticket access.PairingTicket
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"deck-1","device_name":"steamdeck"}`), &ticket)
	var pending []access.PairingRequest
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/pairing/requests", owner, ""), &pending)
	send(t, handler, http.MethodPost, "/api/v1/pairing/requests/"+pending[0].ID+"/approve", owner, "")

	first := send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("the first collection returned %d", first.Code)
	}
	second := send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`)
	if second.Code != http.StatusNotFound {
		t.Fatalf("a spent handle returned %d: %s", second.Code, second.Body.String())
	}
}

func TestOnlyTheOwnerTokenTradesForACredential(t *testing.T) {
	handler := newConnectedServer(t, nil)

	var issued access.IssuedCredential
	exchanged := send(t, handler, http.MethodPost, "/api/v1/credentials/exchange", owner, `{"name":"Dash"}`)
	if exchanged.Code != http.StatusCreated {
		t.Fatalf("the owner token could not trade for a credential: %d %s",
			exchanged.Code, exchanged.Body.String())
	}
	decodeResponse(t, exchanged, &issued)
	if issued.Token == "" || issued.Token == owner {
		t.Fatalf("the Dash was handed %q", issued.Token)
	}
	if library := send(t, handler, http.MethodGet, "/api/v1/omnisaves", issued.Token, ""); library.Code != http.StatusOK {
		t.Fatalf("the Dash credential was refused: %d", library.Code)
	}

	// An issued credential must not be able to mint another one: a stolen
	// Device credential would otherwise grow a spare that survives revoking.
	again := send(t, handler, http.MethodPost, "/api/v1/credentials/exchange", issued.Token, `{"name":"Second"}`)
	if again.Code != http.StatusForbidden {
		t.Fatalf("an issued credential minted another: %d %s", again.Code, again.Body.String())
	}
}

func TestPairingIsRefusedAfterTooManyAttempts(t *testing.T) {
	handler := newConnectedServer(t, nil)
	for attempt := 0; attempt < 10; attempt++ {
		response := send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
			`{"device_id":"deck-1","device_name":"steamdeck"}`)
		if response.Code != http.StatusCreated {
			t.Fatalf("attempt %d returned %d", attempt, response.Code)
		}
	}
	refused := send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"deck-1","device_name":"steamdeck"}`)
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh attempt returned %d", refused.Code)
	}
}

func TestOwnerSettingsAreReadableAndEditableUnlessPinned(t *testing.T) {
	handler := newConnectedServer(t, nil)

	var listed []ownersettings.Setting
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/settings", owner, ""), &listed)
	announce := findSetting(t, listed, ownersettings.AnnounceDiscovery)
	if !announce.Value || !announce.Editable || announce.Kind != ownersettings.KindToggle {
		t.Fatalf("the announcement setting reads as %+v", announce)
	}

	var updated ownersettings.Setting
	response := send(t, handler, http.MethodPatch,
		"/api/v1/settings/"+ownersettings.AnnounceDiscovery, owner, `{"value":"false"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("turning the announcement off returned %d: %s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &updated)
	if updated.Value || updated.Source != ownersettings.SourceOwner {
		t.Fatalf("the stored setting reads as %+v", updated)
	}

	pinned := newConnectedServer(t, map[string]string{ownersettings.AnnounceDiscovery: "true"})
	decodeResponse(t, send(t, pinned, http.MethodGet, "/api/v1/settings", owner, ""), &listed)
	announce = findSetting(t, listed, ownersettings.AnnounceDiscovery)
	if announce.Editable || announce.Source != ownersettings.SourceDeployment {
		t.Fatalf("a pinned setting is offered as %+v", announce)
	}
	refused := send(t, pinned, http.MethodPatch,
		"/api/v1/settings/"+ownersettings.AnnounceDiscovery, owner, `{"value":"false"}`)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("editing a pinned setting returned %d", refused.Code)
	}
}

func TestPendingRequestsReachAnOpenDashAsAnEvent(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	handler := httpapi.New(accessservice.New(repository, owner), httpapi.Config{
		Saves:    omnisaveservice.New(repository),
		Settings: ownersettings.New(repository, nil),
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	stream := openEventStream(t, server)
	readEvent(t, stream)

	body := bytes.NewBufferString(`{"device_id":"deck-1","device_name":"steamdeck"}`)
	response, err := server.Client().Post(server.URL+"/api/v1/pairing/requests", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	event := readEvent(t, stream)
	if event.Type != "access.changed" {
		t.Fatalf("a pending request published %q", event.Type)
	}
}

func TestAnUnclaimedServerIsClaimedByTheFirstBrowserOnTheNetwork(t *testing.T) {
	handler := newConnectedServer(t, nil)

	var status struct {
		Claimable bool `json:"claimable"`
	}
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/claim", "", ""), &status)
	if !status.Claimable {
		t.Fatal("a server that has issued nothing is not offering to be claimed")
	}

	var issued access.IssuedCredential
	claimed := send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Dash on the laptop","pin":"4071"}`)
	if claimed.Code != http.StatusCreated {
		t.Fatalf("claiming returned %d: %s", claimed.Code, claimed.Body.String())
	}
	decodeResponse(t, claimed, &issued)
	if issued.Token == "" || issued.Credential.DeviceName != "Dash on the laptop" {
		t.Fatalf("claiming issued %+v", issued.Credential)
	}
	if library := send(t, handler, http.MethodGet, "/api/v1/omnisaves", issued.Token, ""); library.Code != http.StatusOK {
		t.Fatalf("the claiming browser was refused: %d", library.Code)
	}

	// Claiming is a door that closes once and stays closed.
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/claim", "", ""), &status)
	if status.Claimable {
		t.Fatal("a claimed server still offers to be claimed")
	}
	again := send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Someone else","pin":"1111"}`)
	if again.Code != http.StatusConflict {
		t.Fatalf("a second claim returned %d: %s", again.Code, again.Body.String())
	}
}

func TestExchangingTheOwnerTokenAlsoClaimsTheServer(t *testing.T) {
	handler := newConnectedServer(t, nil)
	if exchanged := send(t, handler, http.MethodPost, "/api/v1/credentials/exchange",
		owner, `{"name":"Dash"}`); exchanged.Code != http.StatusCreated {
		t.Fatalf("exchange returned %d", exchanged.Code)
	}

	// The owner got there first, so the network cannot claim it afterward.
	claimed := send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Stranger","pin":"1111"}`)
	if claimed.Code != http.StatusConflict {
		t.Fatalf("a server with an owner was claimable: %d", claimed.Code)
	}
}

func TestClaimingIsRefusedFromOffTheLocalNetwork(t *testing.T) {
	handler := newConnectedServer(t, nil)

	fromInternet := func(method, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1/claim", bytes.NewBufferString(body))
		request.RemoteAddr = "203.0.113.7:54321"
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	var status struct {
		Claimable bool `json:"claimable"`
	}
	decodeResponse(t, fromInternet(http.MethodGet, ""), &status)
	if status.Claimable {
		t.Fatal("an unclaimed server offered itself to a public address")
	}
	if refused := fromInternet(http.MethodPost, `{"name":"Stranger","pin":"1111"}`); refused.Code != http.StatusForbidden {
		t.Fatalf("a public address claiming returned %d", refused.Code)
	}

	// And it is still there for whoever is actually on the network.
	if claimed := send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Dash","pin":"4071"}`); claimed.Code != http.StatusCreated {
		t.Fatalf("the refusal consumed the claim: %d", claimed.Code)
	}
}

func TestASecondBrowserSignsInWithThePIN(t *testing.T) {
	handler := newConnectedServer(t, nil)
	send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Mac","pin":"4071"}`)

	// The second browser is offered signing in, not claiming.
	var status struct {
		Claimable bool `json:"claimable"`
		PINSet    bool `json:"pin_set"`
	}
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/claim", "", ""), &status)
	if status.Claimable || !status.PINSet {
		t.Fatalf("the second browser was offered %+v", status)
	}

	var issued access.IssuedCredential
	response := send(t, handler, http.MethodPost, "/api/v1/session", "", `{"pin":"4071","name":"Laptop"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("signing in returned %d: %s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &issued)
	if library := send(t, handler, http.MethodGet, "/api/v1/omnisaves", issued.Token, ""); library.Code != http.StatusOK {
		t.Fatalf("the signed-in browser was refused: %d", library.Code)
	}

	// Both browsers hold their own credential, and either can be revoked
	// without disturbing the other.
	var credentials []access.Credential
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/credentials", owner, ""), &credentials)
	if len(credentials) != 2 {
		t.Fatalf("two browsers produced %d credentials", len(credentials))
	}
}

func TestGuessingThePINIsLockedOutLongBeforeTenThousandTries(t *testing.T) {
	handler := newConnectedServer(t, nil)
	send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Mac","pin":"4071"}`)

	// Four digits is ten thousand guesses. The server has to stop being
	// willing to answer long before that (ADR-010).
	for attempt := 1; attempt <= 5; attempt++ {
		refused := send(t, handler, http.MethodPost, "/api/v1/session", "", `{"pin":"0000"}`)
		if attempt < 5 && refused.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d returned %d", attempt, refused.Code)
		}
		if attempt == 5 && refused.Code != http.StatusTooManyRequests {
			t.Fatalf("the fifth wrong guess returned %d, wanted a lockout", refused.Code)
		}
	}

	// Locked out means locked out even for the right PIN: an attacker who
	// stops guessing must not be able to resume by getting lucky.
	locked := send(t, handler, http.MethodPost, "/api/v1/session", "", `{"pin":"4071"}`)
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("the correct PIN during a lockout returned %d", locked.Code)
	}
	if locked.Header().Get("Retry-After") == "" {
		t.Fatal("a lockout does not say how long to wait")
	}

	// The owner token is never throttled, so a stranger holding down the
	// lockout cannot lock the owner out of their own server.
	if library := send(t, handler, http.MethodGet, "/api/v1/omnisaves", owner, ""); library.Code != http.StatusOK {
		t.Fatalf("the owner token was caught by the PIN lockout: %d", library.Code)
	}
}

func TestThePINMustBeFourDigits(t *testing.T) {
	for _, pin := range []string{"", "123", "12345", "abcd", "12 4"} {
		handler := newConnectedServer(t, nil)
		response := send(t, handler, http.MethodPost, "/api/v1/claim", "",
			`{"name":"Mac","pin":"`+pin+`"}`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("claiming with PIN %q returned %d", pin, response.Code)
		}
		// A rejected claim must leave the server claimable, not owned and
		// unreachable.
		var status struct {
			Claimable bool `json:"claimable"`
		}
		decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/claim", "", ""), &status)
		if !status.Claimable {
			t.Fatalf("a claim rejected for PIN %q still took the server", pin)
		}
	}
}

func TestAnOwnerWhoForgotThePINSetsANewOneFromASignedInBrowser(t *testing.T) {
	handler := newConnectedServer(t, nil)
	var issued access.IssuedCredential
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/claim", "",
		`{"name":"Mac","pin":"4071"}`), &issued)

	if changed := send(t, handler, http.MethodPut, "/api/v1/pin", issued.Token,
		`{"pin":"9182"}`); changed.Code != http.StatusNoContent {
		t.Fatalf("changing the PIN returned %d: %s", changed.Code, changed.Body.String())
	}
	if response := send(t, handler, http.MethodPost, "/api/v1/session", "",
		`{"pin":"9182"}`); response.Code != http.StatusCreated {
		t.Fatalf("the new PIN returned %d", response.Code)
	}
	if response := send(t, handler, http.MethodPost, "/api/v1/session", "",
		`{"pin":"4071"}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("the old PIN still works: %d", response.Code)
	}
	// Changing it is not something a stranger can do.
	if refused := send(t, handler, http.MethodPut, "/api/v1/pin", "", `{"pin":"0000"}`); refused.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated caller changed the PIN: %d", refused.Code)
	}
}

func findSetting(t *testing.T, settings []ownersettings.Setting, key string) ownersettings.Setting {
	t.Helper()
	for _, setting := range settings {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("no setting named %q in %+v", key, settings)
	return ownersettings.Setting{}
}

func TestAProviderSecretIsWrittenAndNeverReadBack(t *testing.T) {
	handler := newConnectedServer(t, nil)

	for key, value := range map[string]string{
		ownersettings.IGDBClientID:     "client-id-from-twitch",
		ownersettings.IGDBClientSecret: "the-secret-itself",
	} {
		response := send(t, handler, http.MethodPatch, "/api/v1/settings/"+key, owner,
			`{"value":"`+value+`"}`)
		if response.Code != http.StatusOK {
			t.Fatalf("setting %s returned %d: %s", key, response.Code, response.Body.String())
		}
		// Not even the response to writing it echoes a secret back.
		if key == ownersettings.IGDBClientSecret && strings.Contains(response.Body.String(), value) {
			t.Fatalf("writing the secret echoed it: %s", response.Body.String())
		}
	}

	listed := send(t, handler, http.MethodGet, "/api/v1/settings", owner, "")
	if strings.Contains(listed.Body.String(), "the-secret-itself") {
		t.Fatalf("the settings list carries the secret: %s", listed.Body.String())
	}
	var settings []ownersettings.Setting
	decodeResponse(t, listed, &settings)

	// The client ID is shown, because it is not a secret.
	clientID := findSetting(t, settings, ownersettings.IGDBClientID)
	if clientID.Text != "client-id-from-twitch" || !clientID.Configured {
		t.Fatalf("the client ID reads as %+v", clientID)
	}
	// The secret says only that it exists.
	secret := findSetting(t, settings, ownersettings.IGDBClientSecret)
	if !secret.Configured || secret.Text != "" || secret.Kind != ownersettings.KindSecret {
		t.Fatalf("the secret reads as %+v", secret)
	}
}

func TestProviderCredentialsPinnedByTheDeploymentAreNotEditable(t *testing.T) {
	handler := newConnectedServer(t, map[string]string{
		ownersettings.IGDBClientID:     "pinned-id",
		ownersettings.IGDBClientSecret: "pinned-secret",
	})

	var settings []ownersettings.Setting
	listed := send(t, handler, http.MethodGet, "/api/v1/settings", owner, "")
	if strings.Contains(listed.Body.String(), "pinned-secret") {
		t.Fatalf("a pinned secret was served: %s", listed.Body.String())
	}
	decodeResponse(t, listed, &settings)
	secret := findSetting(t, settings, ownersettings.IGDBClientSecret)
	if secret.Editable || secret.Source != ownersettings.SourceDeployment || !secret.Configured {
		t.Fatalf("a pinned secret reads as %+v", secret)
	}
	refused := send(t, handler, http.MethodPatch,
		"/api/v1/settings/"+ownersettings.IGDBClientSecret, owner, `{"value":"mine"}`)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("editing a pinned secret returned %d", refused.Code)
	}
}

func TestADeviceCannotApproveAnotherDeviceOrChangeThePIN(t *testing.T) {
	handler := newConnectedServer(t, nil)
	send(t, handler, http.MethodPost, "/api/v1/claim", "", `{"name":"Mac","pin":"4071"}`)

	// A Device pairs and is approved, the ordinary way.
	var ticket access.PairingTicket
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"deck","device_name":"steamdeck"}`), &ticket)
	var pending []access.PairingRequest
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/pairing/requests", owner, ""), &pending)
	send(t, handler, http.MethodPost, "/api/v1/pairing/requests/"+pending[0].ID+"/approve", owner, "")
	var collection access.Collection
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/collect", "",
		`{"handle":"`+ticket.Handle+`"}`), &collection)
	device := collection.Token

	// That Device now holds a real credential. ADR-007: approval happens in
	// the Dash and nowhere else, so one compromised Device must not be able to
	// admit others or take the PIN.
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/pairing/requests", "",
		`{"device_id":"intruder","device_name":"intruder"}`), &ticket)
	decodeResponse(t, send(t, handler, http.MethodGet, "/api/v1/pairing/requests", owner, ""), &pending)

	if approved := send(t, handler, http.MethodPost,
		"/api/v1/pairing/requests/"+pending[0].ID+"/approve", device, ""); approved.Code != http.StatusForbidden {
		t.Fatalf("a Device approved another Device: %d", approved.Code)
	}
	if denied := send(t, handler, http.MethodPost,
		"/api/v1/pairing/requests/"+pending[0].ID+"/deny", device, ""); denied.Code != http.StatusForbidden {
		t.Fatalf("a Device denied a request: %d", denied.Code)
	}
	if changed := send(t, handler, http.MethodPut, "/api/v1/pin", device,
		`{"pin":"9999"}`); changed.Code != http.StatusForbidden {
		t.Fatalf("a Device changed the owner PIN: %d", changed.Code)
	}

	// The owner's own browser credential still can.
	var dash access.IssuedCredential
	decodeResponse(t, send(t, handler, http.MethodPost, "/api/v1/session", "", `{"pin":"4071"}`), &dash)
	if approved := send(t, handler, http.MethodPost,
		"/api/v1/pairing/requests/"+pending[0].ID+"/approve", dash.Token, ""); approved.Code != http.StatusNoContent {
		t.Fatalf("the owner's browser could not approve: %d", approved.Code)
	}
}
