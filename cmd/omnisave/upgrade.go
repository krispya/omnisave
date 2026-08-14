package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/client/upgrade"
)

// installedVersion is what this binary reports about itself, build number
// included, so an upgrade names exactly what it replaced.
func installedVersion() string {
	if buildNumber == "" {
		return version
	}
	return version + "-" + buildNumber
}

// runUpgrade replaces this binary with a published release. It reads the same
// release layout the install scripts do, so a client installed by either one
// upgrades the same way (ADR-015).
func runUpgrade(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	check := flags.Bool("check", false, "report whether a newer release exists without installing it")
	wanted := flags.String("version", "", "install this version instead of the newest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	path, err := upgrade.Path()
	if err != nil {
		return fmt.Errorf("could not find the installed client: %w", err)
	}

	upgrader := &upgrade.Upgrader{
		Repo: environmentOr("OMNISAVE_REPO", upgrade.DefaultRepo),
		// The same override the install script takes, so a client can be
		// upgraded from wherever it was installed from.
		DownloadBase: os.Getenv("OMNISAVE_BASE_URL"),
	}

	target := *wanted
	if target == "" {
		if upgrader.DownloadBase != "" {
			return errors.New("pass --version when OMNISAVE_BASE_URL is set: only a release can name its own newest version")
		}
		latest, err := upgrader.Latest(ctx)
		if err != nil {
			return fmt.Errorf("could not check for a newer release: %w", err)
		}
		target = latest
		// An explicit --version reinstalls or downgrades on purpose; without
		// one, an upgrade only ever moves forward.
		if !upgrade.Newer(version, target) {
			tui.UpgradeUpToDate(installedVersion())
			return nil
		}
	}

	if *check {
		tui.UpgradeAvailable(installedVersion(), target)
		return nil
	}

	binary, err := upgrader.Download(ctx, target)
	if err != nil {
		if errors.Is(err, upgrade.ErrNotFound) {
			return fmt.Errorf("release %s does not publish a client for this platform", target)
		}
		return err
	}
	previous := installedVersion()
	if err := upgrade.Replace(path, binary); err != nil {
		return err
	}
	tui.UpgradeApplied(previous, target, path)
	return nil
}
