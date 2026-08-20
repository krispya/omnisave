package tui

import "fmt"

// Update results read as one plain title naming the client that is installed,
// with dim sentences under it saying what happened.

func updateReport(installed string, sentences ...string) {
	fmt.Println(plainTitle("Omnisave " + installed))
	for _, sentence := range sentences {
		fmt.Println("  " + mutedStyle.Render(sentence))
	}
}

// UpdateUpToDate reports that the newest release is already installed.
func UpdateUpToDate(installed string) {
	updateReport(installed, "Up to date")
}

// UpdateAvailable reports a newer release without installing it.
func UpdateAvailable(installed, latest string) {
	updateReport(installed,
		latest+" is available",
		"Run omnisave update to install it")
}

// UpdateApplied reports a completed update. It names the path because a
// device can hold more than one client, and the one that was replaced is the
// only one this says anything about. A replaced binary is not a running one:
// the last sentence is what still has to happen, and it is nothing at all
// when the service already restarted onto the new client.
func UpdateApplied(previous, installed, path string, restarted bool) {
	running := "Restart omnisave watch to run it"
	if restarted {
		running = "The Omnisave service restarted onto it"
	}
	updateReport(installed,
		"Updated from "+previous,
		"Installed to "+path,
		running)
}
