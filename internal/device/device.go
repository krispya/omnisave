// Package device defines the Devices a server knows — self-identified client
// installations — and their live playing presence (see FDR-002 and ADR-013).
// Registration is durable; presence is a short-lived report the server alone
// ages out.
package device

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("device: not found")
	ErrInvalid  = errors.New("device: invalid input")
)

// Device is one self-identified client installation known to the server.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// Registration is a Device's self-reported identity.
type Registration struct {
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
}

// Presence is one Device's credible playing report: the Games it said it is
// running and when it last said so.
type Presence struct {
	DeviceID       string    `json:"device_id"`
	PlayingGameIDs []string  `json:"playing_game_ids"`
	ReportedAt     time.Time `json:"reported_at"`
}

// Service registers Devices and keeps their playing presence. Presence lives
// only in memory: a report stays credible for a short window unless the
// Device re-affirms it, so a crashed or disconnected Device stops reading as
// playing without anyone clearing it.
type Service interface {
	// Register records a Device's identity, creating it on first contact and
	// refreshing its name, platform, and last-seen time afterwards.
	Register(ctx context.Context, id string, input Registration) (*Device, error)
	Get(ctx context.Context, id string) (*Device, error)

	// ReportPlaying replaces a registered Device's playing set (ErrNotFound for
	// an unknown Device) and reports whether that changed what readers see.
	ReportPlaying(ctx context.Context, id string, gameIDs []string) (changed bool, err error)
	// Presence returns every Device with a credible non-empty playing report,
	// ordered by Device ID.
	Presence() []Presence
	// Playing reports whether deviceID credibly reports gameID as running, and
	// when it last said so.
	Playing(deviceID, gameID string) (reportedAt time.Time, playing bool)
	// OnPresenceExpired registers fn to run whenever aging alone changes what
	// readers see, so watchers hear about it instead of keeping clocks of their
	// own (ADR-013). fn runs on the expiry timer's goroutine.
	OnPresenceExpired(fn func())
}

// Repository persists Device registrations. GetDevice answers ErrNotFound for
// an unknown Device.
type Repository interface {
	UpsertDevice(ctx context.Context, device Device) error
	GetDevice(ctx context.Context, id string) (*Device, error)
}
