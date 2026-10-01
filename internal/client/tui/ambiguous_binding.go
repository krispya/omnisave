package tui

import (
	"errors"

	"github.com/charmbracelet/huh"

	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

type unmatchedBindingAction string

const (
	unmatchedBindingSync   unmatchedBindingAction = "sync"
	unmatchedBindingCreate unmatchedBindingAction = "create"
	ambiguousChoiceCreate                         = ":create-new"
)

// PromptAmbiguousBinding asks where a Local Save belongs when content
// matching cannot decide alone.
func PromptAmbiguousBinding(gameTitle string, options []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
	if len(options) == 0 {
		return savesync.AmbiguousChoice{}, ErrNoOmnisaves
	}
	if matchCount(options) == 0 {
		return promptUnmatchedBinding(gameTitle, options)
	}

	matchedOptions := make([]savesync.AmbiguousOption, 0, len(options))
	for _, option := range options {
		if option.MatchedRevisionID != "" {
			matchedOptions = append(matchedOptions, option)
		}
	}
	selected := matchedOptions[0].OmnisaveID
	if err := multipleMatchesForm(gameTitle, matchedOptions, &selected).Run(); err != nil {
		return savesync.AmbiguousChoice{}, bindingPromptError(err)
	}
	if selected == ambiguousChoiceCreate {
		return savesync.AmbiguousChoice{Create: true}, nil
	}
	return savesync.AmbiguousChoice{OmnisaveID: selected}, nil
}

func promptUnmatchedBinding(gameTitle string, options []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
	action := unmatchedBindingSync
	if err := unmatchedBindingActionForm(gameTitle, &action).Run(); err != nil {
		return savesync.AmbiguousChoice{}, bindingPromptError(err)
	}
	if action == unmatchedBindingCreate {
		return savesync.AmbiguousChoice{Create: true}, nil
	}

	selected := options[0].OmnisaveID
	if err := unmatchedBindingSaveForm(gameTitle, options, &selected).Run(); err != nil {
		return savesync.AmbiguousChoice{}, bindingPromptError(err)
	}
	return savesync.AmbiguousChoice{OmnisaveID: selected}, nil
}

// PromptHeldLineageSeed asks before starting a new save for a game whose
// existing lineages are all held awaiting path-format migration. A new save
// cannot rejoin a held lineage that later migrates, so splitting the game's
// history is the user's call, never a silent default.
func PromptHeldLineageSeed(gameTitle string) (bool, error) {
	action := heldLineageWait
	prompt := huh.NewSelect[string]().
		Title("Existing saves are held for migration").
		Description("A new save cannot rejoin them later.").
		Options(
			huh.NewOption("Wait for migration", heldLineageWait),
			huh.NewOption("Create a new save", heldLineageCreate),
		).
		Value(&action)
	if err := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme()).Run(); err != nil {
		return false, bindingPromptError(err)
	}
	return action == heldLineageCreate, nil
}

const (
	heldLineageWait   = "wait"
	heldLineageCreate = "create"
)

func bindingPromptError(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func matchCount(options []savesync.AmbiguousOption) int {
	matched := 0
	for _, option := range options {
		if option.MatchedRevisionID != "" {
			matched++
		}
	}
	return matched
}

func unmatchedBindingActionForm(gameTitle string, action *unmatchedBindingAction) *huh.Form {
	prompt := huh.NewSelect[unmatchedBindingAction]().
		Title("Unmatched local save").
		Options(
			huh.NewOption("Sync with save", unmatchedBindingSync),
			huh.NewOption("Create a new save", unmatchedBindingCreate),
		).
		Value(action)
	return huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme())
}

func unmatchedBindingSaveForm(gameTitle string, options []savesync.AmbiguousOption, selected *string) *huh.Form {
	selections := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		selections = append(selections, huh.NewOption(option.Name, option.OmnisaveID))
	}
	prompt := huh.NewSelect[string]().
		Title("Choose a save").
		Options(selections...).
		Value(selected)
	return huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme())
}

func multipleMatchesForm(gameTitle string, options []savesync.AmbiguousOption, selected *string) *huh.Form {
	selections := make([]huh.Option[string], 0, len(options)+1)
	for _, option := range options {
		selections = append(selections, huh.NewOption(option.Name, option.OmnisaveID))
	}
	selections = append(selections, huh.NewOption("Create a new save", ambiguousChoiceCreate))
	prompt := huh.NewSelect[string]().
		Title("Local save matches more than one save").
		Options(selections...).
		Value(selected)
	return huh.NewForm(huh.NewGroup(prompt).Title(gameTitle)).WithTheme(trackingTheme())
}
