package tui

import (
	"errors"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// PromptStaleBinding asks whether a local snapshot on a non-current revision
// should jump to the Current Revision or continue independently as a fork.
func PromptStaleBinding(question savesync.StaleQuestion) (savesync.StaleChoice, error) {
	choice := savesync.StaleJump
	form := staleBindingForm(question, &choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return choice, nil
}

func staleBindingForm(question savesync.StaleQuestion, choice *savesync.StaleChoice) *huh.Form {
	prompt := huh.NewSelect[savesync.StaleChoice]().
		Title(DivergenceTitle(question.OmnisaveName)).
		Options(
			huh.NewOption("Jump to current", savesync.StaleJump),
			huh.NewOption(ForkLabel(question.ForkName), savesync.StaleFork),
		).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(question.GameTitle))
	return form.WithTheme(trackingTheme())
}
