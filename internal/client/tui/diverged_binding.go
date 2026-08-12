package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// DivergedBindingChoice preserves both sides by forking or preserving-then-jumping.
type DivergedBindingChoice string

const (
	// DivergedBindingFork continues this device's progress as a new lineage.
	DivergedBindingFork DivergedBindingChoice = "fork"
	// DivergedBindingJump preserves this device's progress as a fork, then
	// takes the Current Revision.
	DivergedBindingJump DivergedBindingChoice = "jump"
)

// DivergedOption is one answer as the user reads it. The label is shared by
// the track run's form and the watch view's modal, so the two surfaces
// cannot drift into naming the same choice differently. "Take current"
// rather than "jump": the glossary already spends that word on a restore
// between sibling branches, and neither surface has room to disambiguate.
type DivergedOption struct {
	Label       string
	Description string
	Choice      DivergedBindingChoice
}

// DivergedOptions is the answer set, in the order both surfaces show it.
func DivergedOptions() []DivergedOption {
	return []DivergedOption{
		{
			Label:       "Fork here",
			Description: "continue this device's progress as a new playthrough",
			Choice:      DivergedBindingFork,
		},
		{
			Label:       "Take current",
			Description: "keep this progress as a fork and take the current revision",
			Choice:      DivergedBindingJump,
		},
	}
}

// PromptDivergedBinding asks how a diverged save should continue.
func PromptDivergedBinding(gameTitle, omnisaveName string) (DivergedBindingChoice, error) {
	choice := DivergedBindingFork
	form := divergedBindingForm(gameTitle, omnisaveName, &choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return choice, nil
}

func divergedBindingForm(gameTitle, omnisaveName string, choice *DivergedBindingChoice) *huh.Form {
	options := make([]huh.Option[DivergedBindingChoice], 0, len(DivergedOptions()))
	for _, option := range DivergedOptions() {
		options = append(options, huh.NewOption(option.Label+" · "+option.Description, option.Choice))
	}
	prompt := huh.NewSelect[DivergedBindingChoice]().
		Title(fmt.Sprintf("Save changed here and %s moved on the server", omnisaveName)).
		Options(options...).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
