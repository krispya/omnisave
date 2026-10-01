// Package contract is the /api/v1 wire contract shared by the server's HTTP
// adapter and the client that speaks to it: the change-feed event names and
// the mapping between domain errors and error responses. Both sides compile
// against this one definition, so a renamed event or a new refusal is a
// compile-time change rather than a string that silently stops matching. The
// Dash mirrors it by hand.
package contract

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/device"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/settings"
)

// Event types published on the server's change feed (ADR-002).
const (
	// LibraryChanged announces movement in the Library — a commit, a restore,
	// a fork, a deletion — anything that changes what a Device would sync
	// against.
	LibraryChanged = "library.changed"
	// AccessChanged announces a change in who may reach the server: a pending
	// request appearing, resolving, or expiring, or a credential issued or
	// revoked.
	AccessChanged = "access.changed"
	// DevicesChanged announces a change in Device playing presence.
	DevicesChanged = "devices.changed"
)

// Error codes name the refusals a caller can act on. Each carries the fields
// that let it act without another round trip; every other failure is
// described by its status alone.
const (
	CodeIdentityConflict            = "identity_conflict"
	CodeCurrentRevisionConflict     = "current_revision_conflict"
	CodeRevisionInUse               = "revision_in_use"
	CodeMigrationRefused            = "migration_refused"
	CodePathFormatMigrationRequired = "path_format_migration_required"
	CodeLockedOut                   = "locked_out"
	CodeArtifactMissing             = "artifact_missing"
)

// EncodeError maps a domain error to its HTTP status and JSON body. A coded
// refusal's body carries "error" (its code), "status", and its own fields; any
// other error's body carries the status text instead of a code.
func EncodeError(err error) (int, map[string]any) {
	coded := func(status int, code string, fields map[string]any) (int, map[string]any) {
		body := map[string]any{"error": code, "status": status}
		for key, value := range fields {
			body[key] = value
		}
		return status, body
	}

	var identityConflict *catalog.IdentityConflict
	var revisionConflict *omnisave.CurrentRevisionConflict
	var inUse *omnisave.RevisionInUse
	var refused *omnisave.MigrationRefused
	var lockedOut *access.LockedOut
	var missing *omnisave.MissingArtifacts
	switch {
	case errors.As(err, &identityConflict):
		return coded(http.StatusConflict, CodeIdentityConflict, map[string]any{
			"game_ids": identityConflict.GameIDs,
		})
	case errors.As(err, &revisionConflict):
		return coded(http.StatusConflict, CodeCurrentRevisionConflict, map[string]any{
			"expected_current_revision_id": revisionConflict.ExpectedCurrentRevisionID,
			"actual_current_revision_id":   revisionConflict.ActualCurrentRevisionID,
		})
	case errors.As(err, &inUse):
		return coded(http.StatusConflict, CodeRevisionInUse, map[string]any{"reason": inUse.Reason})
	case errors.As(err, &refused):
		return coded(http.StatusConflict, CodeMigrationRefused, map[string]any{"reason": refused.Reason})
	case errors.Is(err, omnisave.ErrPathFormatMigrationRequired):
		return coded(http.StatusConflict, CodePathFormatMigrationRequired, nil)
	case errors.As(err, &lockedOut):
		// Rounded up, so a client that waits exactly this long is let in.
		return coded(http.StatusTooManyRequests, CodeLockedOut, map[string]any{
			"retry_after": int(lockedOut.RetryAfter.Seconds()) + 1,
		})
	case errors.As(err, &missing):
		return coded(http.StatusUnprocessableEntity, CodeArtifactMissing, map[string]any{
			"missing_sha256": missing.SHA256,
		})
	}
	status := errorStatus(err)
	return status, map[string]any{"error": http.StatusText(status), "status": status}
}

// errorStatus is the status of an error that names no coded refusal.
func errorStatus(err error) int {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, omnisave.ErrInvalid), errors.Is(err, catalog.ErrInvalid),
		errors.Is(err, access.ErrInvalid), errors.Is(err, device.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, omnisave.ErrNotFound), errors.Is(err, catalog.ErrNotFound),
		errors.Is(err, access.ErrNotFound), errors.Is(err, settings.ErrNotFound),
		errors.Is(err, device.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, catalog.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, catalog.ErrConflict), errors.Is(err, access.ErrNotPending),
		errors.Is(err, access.ErrClaimed), errors.Is(err, access.ErrNoPIN):
		return http.StatusConflict
	case errors.Is(err, access.ErrUnauthorized), errors.Is(err, access.ErrPIN):
		return http.StatusUnauthorized
	// A setting the deployment pinned is not the owner's to change, and
	// saying so is the point: a Dash that silently ignored the edit would
	// be worse than one that never offered it (ADR-003).
	case errors.Is(err, settings.ErrPinned):
		return http.StatusForbidden
	case errors.Is(err, access.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusInternalServerError
}

// DecodeError turns an error response body back into the domain error
// EncodeError wrote it from. It returns nil for a body that names no coded
// refusal, which leaves the status to describe the failure.
func DecodeError(body []byte) error {
	var fields struct {
		Code                      string   `json:"error"`
		Reason                    string   `json:"reason"`
		GameIDs                   []string `json:"game_ids"`
		ExpectedCurrentRevisionID *string  `json:"expected_current_revision_id"`
		ActualCurrentRevisionID   *string  `json:"actual_current_revision_id"`
		RetryAfter                int      `json:"retry_after"`
		MissingSHA256             []string `json:"missing_sha256"`
	}
	if json.Unmarshal(body, &fields) != nil {
		return nil
	}
	switch fields.Code {
	case CodeIdentityConflict:
		return &catalog.IdentityConflict{GameIDs: fields.GameIDs}
	case CodeCurrentRevisionConflict:
		return &omnisave.CurrentRevisionConflict{
			ExpectedCurrentRevisionID: fields.ExpectedCurrentRevisionID,
			ActualCurrentRevisionID:   fields.ActualCurrentRevisionID,
		}
	case CodeRevisionInUse:
		return &omnisave.RevisionInUse{Reason: fields.Reason}
	case CodeMigrationRefused:
		return &omnisave.MigrationRefused{Reason: fields.Reason}
	case CodePathFormatMigrationRequired:
		return omnisave.ErrPathFormatMigrationRequired
	case CodeLockedOut:
		return &access.LockedOut{RetryAfter: time.Duration(fields.RetryAfter) * time.Second}
	case CodeArtifactMissing:
		return &omnisave.MissingArtifacts{SHA256: fields.MissingSHA256}
	}
	return nil
}
