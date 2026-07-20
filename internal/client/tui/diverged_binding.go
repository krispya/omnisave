package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// DivergedBindingChoice resolves a save with new progress both locally and
// on the server (FDR-005). Neither choice destroys content.
type DivergedBindingChoice string

const (
	// DivergedBindingFork continues this device's progress as a new lineage.
	DivergedBindingFork DivergedBindingChoice = "fork"
	// DivergedBindingJump preserves this device's progress as a fork, then
	// takes the lineage head.
	DivergedBindingJump DivergedBindingChoice = "jump"
)

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
	prompt := huh.NewSelect[DivergedBindingChoice]().
		Title(fmt.Sprintf("Save has new progress here and on %s", omnisaveName)).
		Options(
			huh.NewOption("Fork here · continue this device's progress as a new playthrough", DivergedBindingFork),
			huh.NewOption("Jump to latest · keep this progress as a fork and take the latest revision", DivergedBindingJump),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
