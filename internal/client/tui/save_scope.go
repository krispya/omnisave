package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// PromptSaveScope asks whether a game keeps a history per save slot; no keeps
// one whole save. It is only asked through omnisave bind --choose-scope, for
// games whose adapter finds slots on this Device, or fails to; the current
// scope starts selected.
func PromptSaveScope(question savesync.ScopeQuestion) (savesync.ScopeChoice, error) {
	choice := string(question.Current)
	prompt := huh.NewSelect[string]().
		Title("Sync individual save slots?").
		Options(
			huh.NewOption("Yes", string(savesync.ScopeSaveSlots)),
			huh.NewOption("No", string(savesync.ScopeWholeSave)),
		).
		Value(&choice)
	if len(question.Slots) > 0 {
		prompt.Description(strings.Join(question.Slots, ", "))
	}
	form := huh.NewForm(huh.NewGroup(prompt).Title(question.GameTitle)).WithTheme(trackingTheme())
	if err := runScopeForm(form); err != nil {
		return "", err
	}
	return savesync.ScopeChoice(choice), nil
}

func runScopeForm(form *huh.Form) error {
	err := form.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}
