package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/access"
)

type principalContextKey struct{}

// Authenticate resolves the bearer token on a request to the principal behind
// it. Authentication is a lookup against credentials this server issued rather
// than a comparison with one configured string (ADR-007); the owner token
// still works, as the bootstrap and as the way back in.
func Authenticate(service access.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeUnauthorized(w)
			return
		}
		principal, err := service.Authenticate(r.Context(), token)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
	})
}

// RequireOwner guards operations that Device credentials may not perform.
func RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok || !principal.OwnerPresent() {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireOwnerToken prevents issued credentials from minting another credential.
func RequireOwnerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok || !principal.Owner {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(authorization, "Bearer "), true
}

func withPrincipal(ctx context.Context, principal *access.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFrom returns who a request authenticated as, if anything did.
func PrincipalFrom(ctx context.Context) (*access.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(*access.Principal)
	return principal, ok
}

// sourceAddress returns the direct peer and does not trust forwarded headers.
func sourceAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// fromLocalNetwork reports whether the direct peer is loopback or private.
func fromLocalNetwork(r *http.Request) bool {
	address := net.ParseIP(sourceAddress(r))
	if address == nil {
		return false
	}
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="Omnisave"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func writeForbidden(w http.ResponseWriter) {
	http.Error(w, "forbidden", http.StatusForbidden)
}
