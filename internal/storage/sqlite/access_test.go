package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/settings"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func openRepository(t *testing.T) *sqlite.Repository {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"),
		filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	return repository
}

func TestIssuedCredentialsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	artifactDir := filepath.Join(directory, "artifacts")

	repository, err := sqlite.Open(databasePath, artifactDir)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := accessservice.New(repository, "owner").ExchangeOwnerToken(ctx, "Dash")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(databasePath, artifactDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	principal, err := accessservice.New(reopened, "owner").Authenticate(ctx, issued.Token)
	if err != nil {
		t.Fatalf("a credential stopped working across a restart: %v", err)
	}
	if principal.CredentialID != issued.Credential.ID {
		t.Fatalf("the credential came back as %+v", principal)
	}
}

func TestAPairingTokenIsTakenOnlyOnce(t *testing.T) {
	ctx := context.Background()
	repository := openRepository(t)
	now := time.Now()
	record := storage.PairingRecord{
		PairingRequest: access.PairingRequest{
			ID:         "request-1",
			Code:       "4F2KQP",
			DeviceID:   "deck-1",
			DeviceName: "steamdeck",
			Status:     access.PairingPending,
			CreatedAt:  now,
			ExpiresAt:  now.Add(5 * time.Minute),
		},
		HandleHash: "handle-hash",
	}
	if err := repository.InsertPairingRequest(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := repository.ResolvePairingRequest(ctx, record.ID,
		access.PairingApproved, "credential-1", "minted-token"); err != nil {
		t.Fatal(err)
	}

	first, err := repository.TakePairingToken(ctx, "handle-hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.MintedToken != "minted-token" || first.Status != access.PairingApproved {
		t.Fatalf("the first collection got %+v", first)
	}
	second, err := repository.TakePairingToken(ctx, "handle-hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if second.MintedToken != "" {
		t.Fatalf("the token was handed out twice: %q", second.MintedToken)
	}

	// A request that has been answered stops being offered, and resolving it
	// again finds nothing pending to resolve.
	pending, err := repository.ListPendingPairingRequests(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("an approved request is still pending: %+v", pending)
	}
	if err := repository.ResolvePairingRequest(ctx, record.ID, access.PairingDenied, "", ""); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("an approved request was resolvable again: %v", err)
	}
}

func TestExpiredRequestsAreCountedUntilTheyAreSweptAway(t *testing.T) {
	ctx := context.Background()
	repository := openRepository(t)
	now := time.Now()
	for index, id := range []string{"a", "b"} {
		if err := repository.InsertPairingRequest(ctx, storage.PairingRecord{
			PairingRequest: access.PairingRequest{
				ID:            id,
				Code:          "CODE" + id,
				DeviceID:      "deck",
				DeviceName:    "steamdeck",
				SourceAddress: "10.0.0.9",
				Status:        access.PairingPending,
				CreatedAt:     now.Add(-time.Duration(index) * time.Minute),
				ExpiresAt:     now.Add(-time.Minute),
			},
			HandleHash: "handle-" + id,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Expired but not yet swept: the rate limit still counts them, so being
	// refused is not a way to reset the limit.
	count, err := repository.CountRecentPairingRequests(ctx, "10.0.0.9", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expired attempts counted as %d", count)
	}
	pending, err := repository.ListPendingPairingRequests(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expired requests are still offered: %+v", pending)
	}

	if err := repository.DeleteExpiredPairingRequests(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	count, err = repository.CountRecentPairingRequests(ctx, "10.0.0.9", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("the sweep left %d rows", count)
	}
}

func TestOwnerSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	repository := openRepository(t)

	if _, err := repository.GetOwnerSetting(ctx, settings.AnnounceDiscovery); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("an unset setting answered %v", err)
	}
	if err := repository.SetOwnerSetting(ctx, settings.AnnounceDiscovery, "false", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetOwnerSetting(ctx, settings.AnnounceDiscovery, "true", time.Now()); err != nil {
		t.Fatal(err)
	}
	value, err := repository.GetOwnerSetting(ctx, settings.AnnounceDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	if value != "true" {
		t.Fatalf("the stored setting is %q", value)
	}
}

func TestRevokingIsRecordedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repository := openRepository(t)
	credentials := accessservice.New(repository, "owner")
	issued, err := credentials.ExchangeOwnerToken(ctx, "Dash")
	if err != nil {
		t.Fatal(err)
	}

	if err := credentials.Revoke(ctx, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Revoke(ctx, issued.Credential.ID); err != nil {
		t.Fatalf("revoking twice failed: %v", err)
	}
	if err := credentials.Revoke(ctx, "no-such-credential"); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("revoking nothing answered %v", err)
	}

	listed, err := credentials.ListCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Revoked() {
		t.Fatalf("the revoked credential reads as %+v", listed)
	}
}
