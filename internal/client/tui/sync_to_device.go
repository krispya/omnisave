package tui

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// SyncToDeviceOption identifies one server save that can be placed on a Device
// with no local save.
type SyncToDeviceOption struct {
	OmnisaveID string
	Name       string
}

// SyncToDeviceChoice is empty when the user decides not to place a save.
type SyncToDeviceChoice struct {
	OmnisaveID string
}

const syncToDeviceDefer = ":decide-later"

// PromptSyncToDevice asks which server save should be placed on a Device that
// has no local save for the game.
func PromptSyncToDevice(gameTitle string, options []SyncToDeviceOption) (SyncToDeviceChoice, error) {
	var selected string
	form := syncToDeviceForm(gameTitle, options, &selected)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return SyncToDeviceChoice{}, ErrAborted
		}
		return SyncToDeviceChoice{}, err
	}
	if selected == syncToDeviceDefer {
		return SyncToDeviceChoice{}, nil
	}
	return SyncToDeviceChoice{OmnisaveID: selected}, nil
}

func syncToDeviceForm(gameTitle string, options []SyncToDeviceOption, selected *string) *huh.Form {
	if *selected == "" && len(options) > 0 {
		*selected = options[0].OmnisaveID
	}
	selections := make([]huh.Option[string], 0, len(options)+1)
	for _, option := range options {
		selections = append(selections,
			huh.NewOption(option.Name+" · sync latest revision to this device", option.OmnisaveID))
	}
	selections = append(selections,
		huh.NewOption("Decide later · leave this device without a save", syncToDeviceDefer))
	prompt := huh.NewSelect[string]().
		Title("No local save exists on this device").
		Options(selections...).
		Value(selected)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
