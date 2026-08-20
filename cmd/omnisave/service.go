package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/client/update"
)

// runService manages the background service that runs watch unattended. It
// sits beside update rather than beside track: both act on this client's
// installation rather than on anyone's saves.
func runService(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("service", flag.ContinueOnError)
	// The standard flag package stops at the first positional argument, so
	// the action is read before parsing and any flags a later action grows
	// are parsed from the remainder — the same two-pass shape connect uses.
	action := "status"
	if len(arguments) > 0 && arguments[0] != "" && arguments[0][0] != '-' {
		action = arguments[0]
		arguments = arguments[1:]
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	// Anything left over was about to be dropped, and a dropped word is how
	// a misplaced flag would run one action while the player asked for
	// another — reporting status, exit zero, while an install walks away
	// believing the service is on.
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q; the service takes one action: install, uninstall, or status", flags.Arg(0))
	}

	switch action {
	case "install":
		return installService(ctx, "")
	case "uninstall":
		return uninstallService(ctx)
	case "status":
		return reportService(ctx)
	default:
		return fmt.Errorf("unknown service action %q; use install, uninstall, or status", action)
	}
}

// installService defines the service and starts it. It refuses a device that
// is not connected: the unit runs watch with no flags and no environment, so
// the only credential it can ever use is the one connect persisted. Installing
// before that produces a service that starts, fails to find a connection,
// and retries forever into a log nobody reads — a device confidently doing
// nothing, which is the one outcome worth spending a check to prevent. For
// the same reason the state checked is the default one: a check against any
// other path would be approving a credential the service can never load.
func installService(ctx context.Context, statePath string) error {
	if !service.Supported() {
		tui.ServiceUnsupported()
		return nil
	}
	store, err := trackingStore(statePath)
	if err != nil {
		return err
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	if state.Server.Token == "" {
		return errors.New("this device is not connected; run omnisave connect first")
	}
	executable, err := update.Path()
	if err != nil {
		return fmt.Errorf("could not find the installed client: %w", err)
	}
	status, err := service.PlatformManager().Install(ctx, service.Config{Executable: executable})
	if err != nil {
		tui.ServiceFailed(err)
		return errReported
	}
	tui.ServiceInstalled(status)
	return nil
}

func uninstallService(ctx context.Context) error {
	if !service.Supported() {
		tui.ServiceUnsupported()
		return nil
	}
	if err := service.PlatformManager().Uninstall(ctx); err != nil {
		tui.ServiceFailed(err)
		return errReported
	}
	tui.ServiceUninstalled()
	return nil
}

func reportService(ctx context.Context) error {
	if !service.Supported() {
		tui.ServiceUnsupported()
		return nil
	}
	status, err := service.PlatformManager().Status(ctx)
	if err != nil {
		tui.ServiceFailed(err)
		return errReported
	}
	tui.ServiceStatus(status)
	return nil
}

// offerService asks the run that just set tracking up whether this device
// should keep syncing without it. Accepting ends the run rather than watching:
// the service is watching now, and two watchers on one device would be two
// passes writing the same tracking state. It reports whether it took over.
func offerService(ctx context.Context, statePath string) bool {
	// A session on a custom state has nothing to offer: the unit runs watch
	// with no flags (ADR-017), so the only state the service could ever read
	// is the default one connect persisted.
	if statePath != "" || !service.Supported() {
		return false
	}
	status, err := service.PlatformManager().Status(ctx)
	if err != nil || status.Installed {
		// A device that already has a service is already answered, and a
		// device that cannot say is not worth stopping the run to ask.
		return false
	}
	install, err := tui.PromptInstallService()
	if err != nil || !install {
		return false
	}
	if err := installService(ctx, statePath); err != nil {
		// The offer was a convenience; a device that could not take it still
		// has this run's watch loop, which is what it would have had anyway.
		// But the player said yes to something, so the reason it did not
		// happen cannot leave with the error.
		if !errors.Is(err, errReported) {
			tui.ServiceFailed(err)
		}
		return false
	}
	tui.ServiceTookOver()
	return true
}

// suggestService mentions the service to a run that is about to watch in the
// foreground on a device that has none. It never asks: this run is already
// doing the work, and the question was put once, when tracking was set up. A
// custom-state session gets no mention either, because the service only ever
// runs the default state — for that session the command is not an alternative.
func suggestService(ctx context.Context, statePath string) {
	if statePath != "" || !service.Supported() {
		return
	}
	status, err := service.PlatformManager().Status(ctx)
	if err != nil || status.Installed {
		return
	}
	tui.ServiceSuggested()
}
