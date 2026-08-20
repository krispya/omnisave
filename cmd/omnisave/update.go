package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/client/update"
)

// installedVersion is what this binary reports about itself, build number
// included, so an update names exactly what it replaced.
func installedVersion() string {
	if buildNumber == "" {
		return version
	}
	return version + "-" + buildNumber
}

// runUpdate replaces this binary with a published release. It reads the same
// release layout the install scripts do, so a client installed by either one
// updates the same way (ADR-015).
func runUpdate(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	check := flags.Bool("check", false, "report whether a newer release exists without installing it")
	wanted := flags.String("version", "", "install this version instead of the newest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	path, err := update.Path()
	if err != nil {
		return fmt.Errorf("could not find the installed client: %w", err)
	}

	updater := &update.Updater{
		Repo: environmentOr("OMNISAVE_REPO", update.DefaultRepo),
		// The same override the install script takes, so a client can be
		// updated from wherever it was installed from.
		DownloadBase: os.Getenv("OMNISAVE_BASE_URL"),
	}

	target := *wanted
	if target == "" {
		if updater.DownloadBase != "" {
			return errors.New("pass --version when OMNISAVE_BASE_URL is set: only a release can name its own newest version")
		}
		latest, err := updater.Latest(ctx)
		if err != nil {
			return fmt.Errorf("could not check for a newer release: %w", err)
		}
		target = latest
		// An explicit --version reinstalls or downgrades on purpose; without
		// one, an update only ever moves forward.
		if !update.Newer(version, target) {
			tui.UpdateUpToDate(installedVersion())
			return nil
		}
	}

	if *check {
		tui.UpdateAvailable(installedVersion(), target)
		return nil
	}

	binary, err := updater.Download(ctx, target)
	if err != nil {
		if errors.Is(err, update.ErrNotFound) {
			return fmt.Errorf("release %s does not publish a client for this platform", target)
		}
		return err
	}
	previous := installedVersion()
	if err := update.Replace(path, binary); err != nil {
		return err
	}
	// Replacing the binary renames the new one over the path, which leaves a
	// running client executing the file it already opened. That is deliberate
	// — nothing is interrupted mid-pass — but it means a background service
	// keeps running the old client until something restarts it, and on the
	// devices that most need a service there is nobody to notice. A service
	// that is stopped stays stopped: the update has no business starting
	// something the player stopped.
	restarted, err := service.PlatformManager().Restart(ctx)
	if err != nil {
		// The update landed. A service that could not be restarted is worth
		// saying out loud, but it is not a failed update.
		tui.UpdateApplied(previous, target, path, false)
		tui.ServiceFailed(err)
		return nil
	}
	tui.UpdateApplied(previous, target, path, restarted)
	return nil
}
