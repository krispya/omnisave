package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// StaleBindingChoice is the user's resolution when local content matches an
// older revision of exactly one Omnisave.
type StaleBindingChoice string

const (
	// StaleBindingFastForward replaces the local save with the lineage head.
	StaleBindingFastForward StaleBindingChoice = "fast-forward"
	// StaleBindingFork creates a new lineage from the matching revision.
	StaleBindingFork StaleBindingChoice = "fork"
)

// PromptStaleBinding asks whether an older local snapshot should catch up to
// its lineage or continue independently as a fork.
func PromptStaleBinding(gameTitle, omnisaveName string) (StaleBindingChoice, error) {
	choice := StaleBindingFastForward
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
		Title(fmt.Sprintf("Save matches an older revision of %s", omnisaveName)).
		Options(
			huh.NewOption("Jump to latest · replace local files with the latest revision", StaleBindingFastForward),
			huh.NewOption("Fork here · keep these files as a new independent playthrough", StaleBindingFork),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
