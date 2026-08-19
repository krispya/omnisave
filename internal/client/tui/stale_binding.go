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

// StaleQuestion is one stale save put to the user: the game, the Omnisave it
// matches an older revision of, and the name forking would create. The pass
// that found the match fills it in, so the answer names the save it makes.
type StaleQuestion struct {
	GameTitle    string
	OmnisaveName string
	// ForkName is the deconflict name the fork would carry ("Save 1 (Steam
	// Deck)"); empty when the Device is unnamed and the server's default
	// applies.
	ForkName string
}

// PromptStaleBinding asks whether a local snapshot on a non-current revision
// should jump to the Current Revision or continue independently as a fork.
func PromptStaleBinding(question StaleQuestion) (StaleBindingChoice, error) {
	choice := StaleBindingJump
	form := staleBindingForm(question, &choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return choice, nil
}

func staleBindingForm(question StaleQuestion, choice *StaleBindingChoice) *huh.Form {
	prompt := huh.NewSelect[StaleBindingChoice]().
		Title(fmt.Sprintf("Save matches a revision of %s that is not its current one", question.OmnisaveName)).
		Options(
			huh.NewOption("Jump to current", StaleBindingJump),
			huh.NewOption(ForkLabel(question.ForkName), StaleBindingFork),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(question.GameTitle))
	return form.WithTheme(trackingTheme())
}
