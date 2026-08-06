package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// StaleBindingChoice is the user's resolution when local content matches a
// revision of exactly one Omnisave that is not its Current Revision — the
// save may sit behind the current, or ahead of it after a restore.
type StaleBindingChoice string

const (
	// StaleBindingJump replaces the local save with the Current Revision.
	StaleBindingJump StaleBindingChoice = "jump"
	// StaleBindingFork creates a new lineage from the matching revision.
	StaleBindingFork StaleBindingChoice = "fork"
)

// PromptStaleBinding asks whether a local snapshot on a non-current revision
// should jump to the Current Revision or continue independently as a fork.
func PromptStaleBinding(gameTitle, omnisaveName string) (StaleBindingChoice, error) {
	choice := StaleBindingJump
	form := staleBindingForm(gameTitle, omnisaveName, &choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return choice, nil
}

func staleBindingForm(gameTitle, omnisaveName string, choice *StaleBindingChoice) *huh.Form {
	prompt := huh.NewSelect[StaleBindingChoice]().
		Title(fmt.Sprintf("Save matches a revision of %s that is not its current one", omnisaveName)).
		Options(
			huh.NewOption("Jump to current · replace local files with the current revision", StaleBindingJump),
			huh.NewOption("Fork here · keep these files as a new independent playthrough", StaleBindingFork),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
