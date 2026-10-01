package contract_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/httpapi/contract"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// A client that reads a refusal gets back the same domain error the server
// wrote, fields and all, so both sides can match on types instead of strings.
func TestEveryCodedRefusalSurvivesTheTripToTheClient(t *testing.T) {
	expected, actual := "revision-1", "revision-2"
	refusals := []error{
		&catalog.IdentityConflict{GameIDs: []string{"game-1", "game-2"}},
		&omnisave.CurrentRevisionConflict{ExpectedCurrentRevisionID: &expected, ActualCurrentRevisionID: &actual},
		&omnisave.CurrentRevisionConflict{ActualCurrentRevisionID: &actual},
		&omnisave.RevisionInUse{Reason: omnisave.RevisionInUseChildren},
		&omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedForkFamily},
		omnisave.ErrPathFormatMigrationRequired,
		&access.LockedOut{RetryAfter: 30 * time.Second},
		&omnisave.MissingArtifacts{SHA256: []string{"abc"}},
	}
	for _, refusal := range refusals {
		_, body := contract.EncodeError(refusal)
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		decoded := contract.DecodeError(encoded)
		if lockedOut, ok := refusal.(*access.LockedOut); ok {
			// The wire speaks whole seconds, rounded up so waiting exactly
			// that long is enough.
			var got *access.LockedOut
			if !errors.As(decoded, &got) || got.RetryAfter < lockedOut.RetryAfter {
				t.Fatalf("locked out: got %#v from %s", decoded, encoded)
			}
			continue
		}
		if !reflect.DeepEqual(decoded, refusal) {
			t.Fatalf("round trip changed %#v into %#v via %s", refusal, decoded, encoded)
		}
	}
}

func TestAnUncodedFailureIsDescribedByItsStatusAlone(t *testing.T) {
	status, body := contract.EncodeError(omnisave.ErrNotFound)
	if status != http.StatusNotFound || body["error"] != http.StatusText(http.StatusNotFound) {
		t.Fatalf("unexpected encoding: %d %v", status, body)
	}
	encoded, _ := json.Marshal(body)
	if decoded := contract.DecodeError(encoded); decoded != nil {
		t.Fatalf("expected no coded refusal, got %v", decoded)
	}
}
