package tui

import "fmt"

// Upgrade results read as one plain title naming the client that is installed,
// with dim sentences under it saying what happened.

func upgradeReport(installed string, sentences ...string) {
	fmt.Println(plainTitle("Omnisave " + installed))
	for _, sentence := range sentences {
		fmt.Println("  " + mutedStyle.Render(sentence))
	}
}

// UpgradeUpToDate reports that the newest release is already installed.
func UpgradeUpToDate(installed string) {
	upgradeReport(installed, "Up to date")
}

// UpgradeAvailable reports a newer release without installing it.
func UpgradeAvailable(installed, latest string) {
	upgradeReport(installed,
		latest+" is available",
		"Run omnisave upgrade to install it")
}

// UpgradeApplied reports a completed upgrade. It names the path because a
// device can hold more than one client, and the one that was replaced is the
// only one this says anything about. A replaced binary is not a running one:
// the last sentence is what still has to happen, and it is nothing at all
// when the service already restarted onto the new client.
func UpgradeApplied(previous, installed, path string, restarted bool) {
	running := "Restart omnisave watch to run it"
	if restarted {
		running = "The Omnisave service restarted onto it"
	}
	upgradeReport(installed,
		"Upgraded from "+previous,
		"Installed to "+path,
		running)
}
