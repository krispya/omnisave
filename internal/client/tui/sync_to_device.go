package tui

import (
	"errors"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

const syncToDeviceLeaveWithoutSave = ":leave-without-save"

// PromptSyncToDevice asks which server save should be placed on a Device that
// has no local save for the game.
func PromptSyncToDevice(gameTitle string, options []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
	var selected string
	form := syncToDeviceForm(gameTitle, options, &selected)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return savesync.SyncToDeviceChoice{}, ErrAborted
		}
		return savesync.SyncToDeviceChoice{}, err
	}
	if selected == syncToDeviceLeaveWithoutSave {
		return savesync.SyncToDeviceChoice{}, nil
	}
	return savesync.SyncToDeviceChoice{OmnisaveID: selected}, nil
}

func syncToDeviceForm(gameTitle string, options []savesync.SyncToDeviceOption, selected *string) *huh.Form {
	if *selected == "" && len(options) > 0 {
		*selected = options[0].OmnisaveID
	}
	selections := make([]huh.Option[string], 0, len(options)+1)
	for _, option := range options {
		selections = append(selections, huh.NewOption(option.Name, option.OmnisaveID))
	}
	selections = append(selections,
		huh.NewOption("Leave this device without a save", syncToDeviceLeaveWithoutSave))
	prompt := huh.NewSelect[string]().
		Title("No local save exists on this device").
		Options(selections...).
		Value(selected)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
