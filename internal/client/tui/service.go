package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
)

// The service reads as one plain title with dim sentences under it, the shape
// update uses: both answer a question about this installation rather than
// about anyone's saves, and they should look like the same kind of answer.

func serviceReport(sentences ...string) {
	fmt.Println(plainTitle("Omnisave service"))
	for _, sentence := range sentences {
		fmt.Println("  " + mutedStyle.Render(sentence))
	}
}

// ServiceInstalled confirms a service that is now running. It says when the
// service comes back on its own, because that is the whole reason to install
// one.
func ServiceInstalled(status service.Status) {
	serviceReport("Running", startsWhen(status), "Defined in "+status.Definition)
}

// ServiceUninstalled confirms a device with no service left on it.
func ServiceUninstalled() {
	serviceReport("Removed", "Omnisave syncs when you run it")
}

// ServiceStatus reports what the device can see about its own service. It is
// the only way to answer "is this actually running?" on a device with no
// terminal to have watched it from.
func ServiceStatus(status service.Status) {
	if !status.Installed {
		serviceReport("Not installed",
			"Run omnisave service install to keep syncing in the background")
		return
	}
	state := "Stopped"
	if status.Running {
		state = "Running"
	}
	serviceReport(state, startsWhen(status), "Defined in "+status.Definition)
}

// ServiceUnsupported reports a platform with no service manager. Running the
// client by hand is not a degraded mode — it is what every device did before
// this existed — so this says what to do rather than what is missing.
func ServiceUnsupported() {
	serviceReport("Not available on this platform",
		"Run omnisave to sync and keep watching")
}

// startsWhen says what brings the service back without anyone asking, which
// is the difference between a service and a command someone remembered to run.
func startsWhen(status service.Status) string {
	switch status.Start {
	case service.StartAtBoot:
		return "Starts at boot"
	case service.StartAtLogin:
		return "Starts when you log in"
	default:
		return "Does not start on its own"
	}
}

// ServiceSuggested is the non-blocking mention on a run that could have been
// a service. It is one dim line rather than a question because the run it
// interrupts is already doing the work, and a device that syncs when you ask
// it to is a choice rather than a mistake.
func ServiceSuggested() {
	fmt.Println("  " + mutedStyle.Render("Run omnisave service install to keep syncing in the background"))
}

// PromptInstallService asks whether this device should keep syncing after the
// terminal closes. It is asked once, at the end of the run that set tracking
// up, because that run already has the player's attention and is the last
// moment before a Steam Deck goes back to Gaming Mode and stops having one.
func PromptInstallService() (bool, error) {
	choice := installServiceYes
	form := installServiceForm(&choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, ErrAborted
		}
		return false, err
	}
	return choice == installServiceYes, nil
}

const (
	installServiceYes = "yes"
	installServiceNo  = "no"
)

func installServiceForm(choice *string) *huh.Form {
	prompt := huh.NewSelect[string]().
		Title("Keep syncing after you close this terminal?").
		Options(
			huh.NewOption("Yes", installServiceYes),
			huh.NewOption("No", installServiceNo),
		).
		Value(choice)
	return huh.NewForm(huh.NewGroup(prompt).Title("Background syncing")).WithTheme(trackingTheme())
}

// ServiceFailed reports an install or removal that changed nothing, in the
// shape every other client-level failure prints.
func ServiceFailed(err error) {
	fmt.Println(FailureLine(Cause(err)))
}

// ServiceTookOver ends the run that installed the service. The pass this run
// made is done and the service is watching now, so staying would mean two
// watchers on one device writing the same state.
func ServiceTookOver() {
	fmt.Println("  " + mutedStyle.Render("Omnisave is watching in the background. This run is done."))
}
