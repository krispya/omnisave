// Package service runs the client unattended, as a background service the
// player owns. Watch is a complete loop with nobody in front of it, but
// something has to start it and keep it started: a device whose normal state
// is nobody's there — a Steam Deck in Gaming Mode — has no terminal to run it
// from and no session that outlives switching away.
//
// The service grants the client nothing. It runs as the same user, from the
// same binary, with the same access a terminal would give it (ADR-017). What
// it changes is that no one has to be there to start it.
package service

import (
	"context"
	"errors"
)

// ErrUnsupported reports a platform this build has no service manager for.
// It is an ordinary answer rather than a failure: the client runs perfectly
// well from a terminal, and every command that consults a manager treats an
// unsupported platform as "there is no service here" rather than as an error
// worth stopping for.
var ErrUnsupported = errors.New("omnisave cannot manage a background service on this platform")

// ErrNotInstalled reports an operation that needs a service the device does
// not have.
var ErrNotInstalled = errors.New("the Omnisave service is not installed")

// Config is what the service is being asked to run.
type Config struct {
	// Executable is the client binary the service runs. The caller resolves
	// it — the same resolution update uses to find what it replaces — so
	// that a device holding more than one client services the one that was
	// asked, not whichever the PATH would have found.
	Executable string
}

// StartMode says when an enabled service comes back without being started by
// the player. Login is the portable baseline; Linux can additionally start at
// boot when the user's systemd manager is allowed to linger.
type StartMode uint8

const (
	StartManually StartMode = iota
	StartAtLogin
	StartAtBoot
)

// Status is what a device can say about its own background service without
// reading a log. Every field is false on a platform with no manager, so a
// caller that only wants to know whether to suggest installing one needs no
// platform check of its own.
type Status struct {
	// Installed reports that the service is defined on this device.
	Installed bool
	// Enabled reports that it starts on its own rather than only when
	// something starts it.
	Enabled bool
	// Running reports a live process right now.
	Running bool
	// Start reports when an enabled service comes back on its own.
	Start StartMode
	// Definition is where the service is written, for output that has to name
	// a file the player can read.
	Definition string
}

// Manager installs, removes, and reports on the client's background service
// on one platform.
type Manager interface {
	// Install defines the service, starts it, and arranges for it to start
	// again on its own. It is safe to re-run: the definition is rewritten
	// from the config given, so an update of the definition is an install.
	Install(ctx context.Context, config Config) (Status, error)
	// Uninstall stops the service and removes its definition. Uninstalling
	// what was never installed is not an error — the end state is the one
	// asked for either way.
	Uninstall(ctx context.Context) error
	// Status reports what the device can see about the service now.
	Status(ctx context.Context) (Status, error)
	// Restart restarts a running service and reports whether it restarted
	// one. A service that is installed but stopped is left stopped: a client
	// replacing its own binary has no business starting something the player
	// stopped on purpose.
	Restart(ctx context.Context) (bool, error)
}

// Supported reports whether this platform can run the client as a service.
// It is what separates "this device has no service" from "no device on this
// platform can have one", which are the same Status and different sentences.
func Supported() bool {
	_, none := PlatformManager().(unsupported)
	return !none
}

// unsupported is the manager for a platform this build cannot service. It
// answers every question rather than refusing to be constructed, so callers
// branch on the answer instead of on the platform.
type unsupported struct{}

func (unsupported) Install(context.Context, Config) (Status, error) {
	return Status{}, ErrUnsupported
}

func (unsupported) Uninstall(context.Context) error { return ErrUnsupported }

// Status answers "no service here" rather than failing, so a caller deciding
// whether to mention the service at all needs no special case.
func (unsupported) Status(context.Context) (Status, error) { return Status{}, nil }

func (unsupported) Restart(context.Context) (bool, error) { return false, nil }
