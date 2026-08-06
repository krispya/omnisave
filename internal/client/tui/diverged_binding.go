package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// DivergedBindingChoice resolves a save whose local content and Omnisave
// Current Revision both moved away from the sync baseline (FDR-005) — a
// commit elsewhere or a restore, in any direction. Neither choice destroys
// content.
type DivergedBindingChoice string

const (
	// DivergedBindingFork continues this device's progress as a new lineage.
	DivergedBindingFork DivergedBindingChoice = "fork"
	// DivergedBindingJump preserves this device's progress as a fork, then
	// takes the Current Revision.
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
		Title(fmt.Sprintf("Save changed here and %s moved on the server", omnisaveName)).
		Options(
			huh.NewOption("Fork here · continue this device's progress as a new playthrough", DivergedBindingFork),
			huh.NewOption("Jump to current · keep this progress as a fork and take the current revision", DivergedBindingJump),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
