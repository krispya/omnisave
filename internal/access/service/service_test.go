package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

const ownerToken = "owner-token-with-enough-length-0123456789"

// clock is a hand-wound time source: pairing turns on expiry, and a test that
// waited out five real minutes would not be run.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
}

func newService(t *testing.T) (access.Service, *clock) {
	t.Helper()
	time := newClock()
	return accessservice.NewWithClock(storagetest.NewMemoryRepository(), ownerToken, time.Now), time
}

func requestPairing(t *testing.T, ctx context.Context, service access.Service, name string) *access.PairingTicket {
	t.Helper()
	ticket, err := service.RequestPairing(ctx, access.RequestPairing{
		DeviceID:      "device-" + name,
		DeviceName:    name,
		Platform:      "linux",
		SourceAddress: "192.168.1.44",
	})
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestApprovingAPairingRequestIsWhatMintsACredential(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	ticket := requestPairing(t, ctx, service, "steamdeck")

	waiting, err := service.Collect(ctx, ticket.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != access.CollectionPending || waiting.Token != "" {
		t.Fatalf("a request nobody approved handed out %+v", waiting)
	}
	credentials, err := service.ListCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 0 {
		t.Fatalf("waiting minted %d credentials", len(credentials))
	}

	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Code != ticket.Code {
		t.Fatalf("the Dash cannot show the code the Device displays: %+v", pending)
	}
	if err := service.Approve(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}

	collected, err := service.Collect(ctx, ticket.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if collected.Status != access.CollectionApproved || collected.Token == "" {
		t.Fatalf("approval did not hand over a credential: %+v", collected)
	}
	principal, err := service.Authenticate(ctx, collected.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Owner || principal.Name != "steamdeck" || principal.DeviceID != "device-steamdeck" {
		t.Fatalf("the minted credential authenticates as %+v", principal)
	}
}

func TestACollectedCredentialIsNotHandedOutTwice(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	ticket := requestPairing(t, ctx, service, "steamdeck")
	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Approve(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Collect(ctx, ticket.Handle); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Collect(ctx, ticket.Handle); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("a spent handle answered again: %v", err)
	}
}

func TestKnowingTheCodeCollectsNothing(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	ticket := requestPairing(t, ctx, service, "steamdeck")
	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Approve(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}

	// The code is what a person reads off a screen in a room with other
	// people in it. Only the handle collects.
	if _, err := service.Collect(ctx, ticket.Code); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("the displayed code collected a credential: %v", err)
	}
}

func TestDenyingARequestMintsNothingAndTellsTheClient(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	ticket := requestPairing(t, ctx, service, "unknown-laptop")
	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deny(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}

	collected, err := service.Collect(ctx, ticket.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if collected.Status != access.CollectionDenied || collected.Token != "" {
		t.Fatalf("denial answered %+v", collected)
	}
	credentials, err := service.ListCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 0 {
		t.Fatalf("denial minted %d credentials", len(credentials))
	}
}

func TestAnExpiredRequestLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	service, clock := newService(t)
	ticket := requestPairing(t, ctx, service, "steamdeck")
	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}

	clock.advance(6 * time.Minute)

	stale, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("the Dash still offers an expired request: %+v", stale)
	}
	if err := service.Approve(ctx, pending[0].ID); !errors.Is(err, access.ErrNotPending) {
		t.Fatalf("an expired request was approvable: %v", err)
	}
	collected, err := service.Collect(ctx, ticket.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if collected.Status != access.CollectionExpired {
		t.Fatalf("the client was told %+v", collected)
	}
}

func TestRevokingOneDeviceLeavesTheOthersWorking(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	deck := connect(t, ctx, service, "steamdeck")
	desktop := connect(t, ctx, service, "desktop")

	credentials, err := service.ListCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var deckID string
	for _, credential := range credentials {
		if credential.DeviceName == "steamdeck" {
			deckID = credential.ID
		}
	}
	if deckID == "" {
		t.Fatalf("the issued credentials do not name the Device: %+v", credentials)
	}
	if err := service.Revoke(ctx, deckID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Authenticate(ctx, deck); !errors.Is(err, access.ErrUnauthorized) {
		t.Fatalf("a revoked Device still authenticates: %v", err)
	}
	if _, err := service.Authenticate(ctx, desktop); err != nil {
		t.Fatalf("revoking one Device broke another: %v", err)
	}
}

func TestTheOwnerTokenAuthenticatesAndTradesForACredential(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)

	principal, err := service.Authenticate(ctx, ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	if !principal.Owner {
		t.Fatalf("the owner token authenticated as %+v", principal)
	}

	issued, err := service.ExchangeOwnerToken(ctx, "Dash")
	if err != nil {
		t.Fatal(err)
	}
	if issued.Token == ownerToken {
		t.Fatal("the Dash was handed the owner's own secret")
	}
	if issued.Credential.Kind != access.KindDash {
		t.Fatalf("the Dash credential is a %q", issued.Credential.Kind)
	}
	if _, err := service.Authenticate(ctx, issued.Token); err != nil {
		t.Fatal(err)
	}

	// Revoking the browser's credential must not lock the owner out: the
	// token is the way back in when nothing else works.
	if err := service.Revoke(ctx, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, ownerToken); err != nil {
		t.Fatalf("revoking the Dash disarmed the owner token: %v", err)
	}
}

func TestPairingIsRefusedAfterTooManyAttemptsFromOneAddress(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	for attempt := 0; attempt < 10; attempt++ {
		if _, err := service.RequestPairing(ctx, access.RequestPairing{
			DeviceID: "device", DeviceName: "device", SourceAddress: "10.0.0.9",
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, err := service.RequestPairing(ctx, access.RequestPairing{
		DeviceID: "device", DeviceName: "device", SourceAddress: "10.0.0.9",
	})
	if !errors.Is(err, access.ErrRateLimited) {
		t.Fatalf("the eleventh attempt answered %v", err)
	}
	if _, err := service.RequestPairing(ctx, access.RequestPairing{
		DeviceID: "device", DeviceName: "device", SourceAddress: "10.0.0.10",
	}); err != nil {
		t.Fatalf("one address exhausted another address's attempts: %v", err)
	}
}

func TestALiveRequestNeverSharesACodeWithAnother(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	seen := make(map[string]bool)
	for index := 0; index < 8; index++ {
		ticket, err := service.RequestPairing(ctx, access.RequestPairing{
			DeviceID: "device", DeviceName: "device", SourceAddress: "10.0.0.9",
		})
		if err != nil {
			t.Fatal(err)
		}
		if seen[ticket.Code] {
			t.Fatalf("two live requests both show %q", ticket.Code)
		}
		seen[ticket.Code] = true
	}
}

func TestPairingRequiresADeviceIdentity(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	if _, err := service.RequestPairing(ctx, access.RequestPairing{
		DeviceName: "nameless", SourceAddress: "10.0.0.9",
	}); !errors.Is(err, access.ErrInvalid) {
		t.Fatalf("a request with no Device id was accepted: %v", err)
	}
}

func TestAnUnknownTokenAuthenticatesAsNobody(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	for _, token := range []string{"", "   ", "not-a-credential"} {
		if _, err := service.Authenticate(ctx, token); !errors.Is(err, access.ErrUnauthorized) {
			t.Fatalf("token %q authenticated: %v", token, err)
		}
	}
}

// connect runs one Device all the way through pairing and returns the
// credential it ends up holding.
func connect(t *testing.T, ctx context.Context, service access.Service, name string) string {
	t.Helper()
	ticket := requestPairing(t, ctx, service, name)
	pending, err := service.ListPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range pending {
		if request.Code != ticket.Code {
			continue
		}
		if err := service.Approve(ctx, request.ID); err != nil {
			t.Fatal(err)
		}
	}
	collected, err := service.Collect(ctx, ticket.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if collected.Status != access.CollectionApproved {
		t.Fatalf("%s did not connect: %+v", name, collected)
	}
	return collected.Token
}
