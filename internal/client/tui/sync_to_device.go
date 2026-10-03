package tui

import (
	"errors"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

const syncToDeviceLeaveWithoutSave = ":leave-without-save"

// PromptSyncToDevice asks which server save should be placed at which empty
// native destination. A game without save slots picks one save for its only
// destination; a game with slots is asked about one save at a time.
func PromptSyncToDevice(gameTitle string, options []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
	if len(options) > 0 && options[0].DestinationLabel != "" {
		return promptSlotPlacement(gameTitle, options)
	}
	var selected string
	if err := runPlacementForm(syncToDeviceForm(gameTitle, options, &selected)); err != nil {
		return savesync.SyncToDeviceChoice{}, err
	}
	for _, option := range options {
		if option.OmnisaveID == selected {
			return savesync.SyncToDeviceChoice{OmnisaveID: option.OmnisaveID, DestinationID: option.DestinationID}, nil
		}
	}
	return savesync.SyncToDeviceChoice{}, nil
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

// promptSlotPlacement asks about each save in turn, answering with an empty
// slot or no. The first slot chosen is placed; declining every save places
// nothing.
func promptSlotPlacement(gameTitle string, options []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
	for _, save := range placementSaves(options) {
		var selected string
		if err := runPlacementForm(slotPlacementForm(gameTitle, save, &selected)); err != nil {
			return savesync.SyncToDeviceChoice{}, err
		}
		if selected != syncToDeviceLeaveWithoutSave {
			return savesync.SyncToDeviceChoice{OmnisaveID: save[0].OmnisaveID, DestinationID: selected}, nil
		}
	}
	return savesync.SyncToDeviceChoice{}, nil
}

// placementSaves groups options by save, keeping the order they were offered.
func placementSaves(options []savesync.SyncToDeviceOption) [][]savesync.SyncToDeviceOption {
	var saves [][]savesync.SyncToDeviceOption
	index := map[string]int{}
	for _, option := range options {
		position, seen := index[option.OmnisaveID]
		if !seen {
			position = len(saves)
			index[option.OmnisaveID] = position
			saves = append(saves, nil)
		}
		saves[position] = append(saves[position], option)
	}
	return saves
}

// slotPlacementForm asks whether one save goes in an empty slot; each answer
// but no names the slot.
func slotPlacementForm(gameTitle string, save []savesync.SyncToDeviceOption, selected *string) *huh.Form {
	if *selected == "" {
		*selected = save[0].DestinationID
	}
	selections := make([]huh.Option[string], 0, len(save)+1)
	for _, option := range save {
		selections = append(selections, huh.NewOption(option.DestinationLabel, option.DestinationID))
	}
	selections = append(selections, huh.NewOption("No", syncToDeviceLeaveWithoutSave))
	prompt := huh.NewSelect[string]().
		Title("Put " + save[0].Name + " in an empty slot?").
		Options(selections...).
		Value(selected)
	if !save[0].SavedAt.IsZero() {
		prompt.Description("Saved " + save[0].SavedAt.Local().Format("Jan 2 15:04"))
	}
	return huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme())
}

func runPlacementForm(form *huh.Form) error {
	err := form.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}
