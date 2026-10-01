package tui

import (
	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// DivergedStep is one screen of the divergence question: a prompt and the
// options it offers, in the order both surfaces show them.
type DivergedStep struct {
	Title   string
	Options []DivergedOption
}

// DivergedOption is one option as the user reads it. The label is shared by
// the track run's form and the watch view's modal, so the two surfaces cannot
// drift into naming the same choice differently. An option either answers
// the question with Choice or, with Next set, opens the step that does.
type DivergedOption struct {
	Label  string
	Choice savesync.DivergedChoice
	Next   *DivergedStep
}

// DivergenceTitle says what a save that is not the same on both sides has
// done. The stale and diverged prompts and the watch modal all open on it,
// so they share the sentence rather than each phrasing it their own way.
func DivergenceTitle(omnisaveName string) string {
	return omnisaveName + " diverges between this device and the server"
}

// ForkLabel names a fork answer for the save it would create. The stale and
// diverged prompts both offer one, so they share the wording rather than
// drift apart. An unnamed Device has no deconflict name to show, and the
// answer says only that a save appears.
func ForkLabel(forkName string) string {
	if forkName == "" {
		return "Fork as a new save"
	}
	return "Fork as " + forkName
}

// currentStep asks which save becomes an omnisave's Current Revision when
// this Device's save and the omnisave's disagree: keep the current save, or
// use this Device's. The diverged, stale, and unmatched prompts all end on
// it, so the same decision reads the same way wherever it is asked. Either
// answer leaves one omnisave, with the other side kept in its history.
func currentStep(omnisaveName string) *DivergedStep {
	return &DivergedStep{
		Title: "Which save becomes current on " + omnisaveName + "?",
		Options: []DivergedOption{
			{Label: "Keep the current save", Choice: savesync.DivergedJump},
			{Label: "Use this device's save", Choice: savesync.DivergedUseLocal},
		},
	}
}

// DivergedSteps is the question in two steps. The first asks whether this
// save keeps syncing with its omnisave or starts its own; syncing opens
// currentStep.
//
// Each step opens on its first option, so confirming without reading syncs
// and keeps the current save: that ends the divergence without moving what
// other Devices follow, where using this Device's save moves current for
// all of them and forking splits the playthrough. Every answer keeps the
// local progress (FDR-005, decision 4).
func DivergedSteps(question savesync.DivergedQuestion) DivergedStep {
	return DivergedStep{
		Title: DivergenceTitle(question.OmnisaveName),
		Options: []DivergedOption{
			{Label: "Sync with " + question.OmnisaveName, Next: currentStep(question.OmnisaveName)},
			{Label: ForkLabel(question.ForkName), Choice: savesync.DivergedFork},
		},
	}
}

// PromptDivergedBinding asks how a diverged save should continue.
func PromptDivergedBinding(question savesync.DivergedQuestion) (savesync.DivergedChoice, error) {
	return askSteps(question.GameTitle, DivergedSteps(question))
}

// askSteps runs a step at a time until an option answers.
func askSteps(gameTitle string, step DivergedStep) (savesync.DivergedChoice, error) {
	for {
		picked := step.Options[0]
		if err := divergedStepForm(gameTitle, step, &picked).Run(); err != nil {
			return "", bindingPromptError(err)
		}
		if picked.Next == nil {
			return picked.Choice, nil
		}
		step = *picked.Next
	}
}

func divergedStepForm(gameTitle string, step DivergedStep, picked *DivergedOption) *huh.Form {
	options := make([]huh.Option[DivergedOption], 0, len(step.Options))
	for _, option := range step.Options {
		options = append(options, huh.NewOption(option.Label, option))
	}
	prompt := huh.NewSelect[DivergedOption]().
		Title(step.Title).
		Options(options...).
		Value(picked)
	return huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme())
}
