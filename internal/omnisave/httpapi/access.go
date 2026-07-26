package httpapi

import (
	"net/http"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/settings"
)

// requestPairing records a client with no credential asking for one. It is one
// of the two endpoints reachable without authentication, because a Device with
// no credential is exactly who calls it.
func (a *API) requestPairing(w http.ResponseWriter, r *http.Request) {
	var input access.RequestPairing
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	input.SourceAddress = sourceAddress(r)
	ticket, err := a.access.RequestPairing(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	writeJSON(w, http.StatusCreated, ticket)
}

// collectPairing answers one poll from a waiting client. The handle arrives in
// the body rather than the path so the secret that collects a credential stays
// out of request logs and browser history.
func (a *API) collectPairing(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Handle string `json:"handle"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	collection, err := a.access.Collect(r.Context(), input.Handle)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (a *API) listPairingRequests(w http.ResponseWriter, r *http.Request) {
	requests, err := a.access.ListPendingRequests(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if requests == nil {
		requests = []access.PairingRequest{}
	}
	writeJSON(w, http.StatusOK, requests)
}

func (a *API) approvePairingRequest(w http.ResponseWriter, r *http.Request) {
	if err := a.access.Approve(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) denyPairingRequest(w http.ResponseWriter, r *http.Request) {
	if err := a.access.Deny(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	w.WriteHeader(http.StatusNoContent)
}

// exchangeOwnerToken trades the owner token for a credential of the caller's
// own, which is how the Dash stops storing the owner's secret.
func (a *API) exchangeOwnerToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	issued, err := a.access.ExchangeOwnerToken(r.Context(), input.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	writeJSON(w, http.StatusCreated, issued)
}

// claimStatus tells a browser whether this server is still unclaimed, so the
// Dash knows to offer claiming instead of asking for a token. It is readable
// without a credential because the browser asking has none yet, and it says
// nothing a stranger on the network could not learn by trying to claim.
func (a *API) claimStatus(w http.ResponseWriter, r *http.Request) {
	claimable, err := a.access.Claimable(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	hasPIN, err := a.access.HasPIN(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"claimable": claimable && fromLocalNetwork(r),
		"pin_set":   hasPIN,
	})
}

// claim hands the first credential to the first browser to reach an unclaimed
// server from the local network (ADR-010).
func (a *API) claim(w http.ResponseWriter, r *http.Request) {
	var input access.ClaimServer
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	if !fromLocalNetwork(r) {
		// Nothing about this server is claimable from off the network, and
		// saying "already claimed" would be a lie.
		writeForbidden(w)
		return
	}
	issued, err := a.access.Claim(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	writeJSON(w, http.StatusCreated, issued)
}

// signIn exchanges the owner's PIN for a credential of this browser's own. It
// is open for the same reason claiming is — whoever calls it has no credential
// — and what protects it is that the server refuses to be hurried (ADR-010).
func (a *API) signIn(w http.ResponseWriter, r *http.Request) {
	var input access.SignIn
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	input.SourceAddress = sourceAddress(r)
	issued, err := a.access.SignIn(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	writeJSON(w, http.StatusCreated, issued)
}

// setPIN changes the owner PIN. Holding a credential is what entitles a caller
// to it, which is how an owner who forgot theirs sets a new one from a browser
// that is already signed in.
func (a *API) setPIN(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PIN string `json:"pin"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	if err := a.access.SetPIN(r.Context(), input.PIN); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := a.access.ListCredentials(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if credentials == nil {
		credentials = []access.Credential{}
	}
	writeJSON(w, http.StatusOK, credentials)
}

func (a *API) revokeCredential(w http.ResponseWriter, r *http.Request) {
	if err := a.access.Revoke(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	a.publishAccessChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listSettings(w http.ResponseWriter, r *http.Request) {
	values, err := a.settings.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if values == nil {
		values = []settings.Setting{}
	}
	writeJSON(w, http.StatusOK, values)
}

// updateSetting stores one owner setting. The value arrives as text whatever
// its kind, because that is what a switch, a client ID, and a secret have in
// common on the wire — and what comes back never carries a secret (ADR-011).
func (a *API) updateSetting(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	setting, err := a.settings.Set(r.Context(), r.PathValue("key"), input.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, setting)
}
