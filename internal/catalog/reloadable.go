package catalog

import (
	"context"
	"io"
	"sync"
)

// Reloadable is a provider whose credentials the owner can change while the
// server runs (ADR-011).
//
// The catalog is built once, so a provider configured later has to be the same
// object that was registered at startup. This is that object: it holds a
// delegate that can be replaced, and reports itself unavailable while it has
// none. Resolution and search already skip an unavailable provider — a
// provider that cannot answer today is a case they have always tolerated — so
// an unconfigured one costs nothing and needs no special handling anywhere.
type Reloadable struct {
	name string

	mu       sync.RWMutex
	delegate Provider
}

// NewReloadable creates an unconfigured provider under a fixed name. The name
// is fixed because it is stamped onto stored media and selection tokens, which
// outlive any particular set of credentials.
func NewReloadable(name string) *Reloadable {
	return &Reloadable{name: name}
}

// Set replaces the credentials in use. A nil delegate turns the provider off,
// which is what removing credentials does.
func (r *Reloadable) Set(delegate Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delegate = delegate
}

// Configured reports whether this provider can currently answer.
func (r *Reloadable) Configured() bool { return r.current() != nil }

func (r *Reloadable) current() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.delegate
}

func (r *Reloadable) Name() string { return r.name }

func (r *Reloadable) Resolve(ctx context.Context, evidence ResolveGame) (*ProviderMatch, error) {
	delegate := r.current()
	if delegate == nil {
		return nil, ErrUnavailable
	}
	return delegate.Resolve(ctx, evidence)
}

func (r *Reloadable) Search(ctx context.Context, input SearchGames) ([]GameCandidate, error) {
	delegate := r.current()
	if delegate == nil {
		return nil, ErrUnavailable
	}
	return delegate.Search(ctx, input)
}

func (r *Reloadable) Match(ctx context.Context, selectionToken string) (*ProviderMatch, error) {
	delegate := r.current()
	if delegate == nil {
		return nil, ErrUnavailable
	}
	return delegate.Match(ctx, selectionToken)
}

func (r *Reloadable) OpenMedia(ctx context.Context, reference MediaReference) (string, io.ReadCloser, error) {
	delegate := r.current()
	if delegate == nil {
		return "", nil, ErrUnavailable
	}
	return delegate.OpenMedia(ctx, reference)
}

var _ Provider = (*Reloadable)(nil)
