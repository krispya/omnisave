package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/client/upgrade"
)

// runService manages the background service that runs watch unattended. It
// sits beside upgrade rather than beside track: both act on this client's
// installation rather than on anyone's saves.
func runService(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("service", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	// The standard flag package stops at the first positional argument, so
	// the action is read before parsing and the flags after it are parsed
	// from the remainder — the same two-pass shape connect uses.
	action := "status"
	if len(arguments) > 0 && arguments[0] != "" && arguments[0][0] != '-' {
		action = arguments[0]
		arguments = arguments[1:]
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	switch action {
	case "install":
		return installService(ctx, *statePath)
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
// nothing, which is the one outcome worth spending a check to prevent.
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
	executable, err := upgrade.Path()
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
	if !service.Supported() {
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
		return false
	}
	tui.ServiceTookOver()
	return true
}

// suggestService mentions the service to a run that is about to watch in the
// foreground on a device that has none. It never asks: this run is already
// doing the work, and the question was put once, when tracking was set up.
func suggestService(ctx context.Context) {
	if !service.Supported() {
		return
	}
	status, err := service.PlatformManager().Status(ctx)
	if err != nil || status.Installed {
		return
	}
	tui.ServiceSuggested()
}
