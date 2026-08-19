package tui

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// AmbiguousBindingOption describes one existing save a Local Save could sync
// with, and whether its history contains the Local Save's exact content.
type AmbiguousBindingOption struct {
	OmnisaveID        string
	Name              string
	MatchedRevisionID string
}

// AmbiguousBindingChoice resolves a Local Save that matches zero or several
// saves. Create makes the Local Save a new save instead of choosing one.
type AmbiguousBindingChoice struct {
	OmnisaveID string
	Create     bool
}

type unmatchedBindingAction string

const (
	unmatchedBindingSync   unmatchedBindingAction = "sync"
	unmatchedBindingCreate unmatchedBindingAction = "create"
	ambiguousChoiceCreate                         = ":create-new"
)

// PromptAmbiguousBinding asks where a Local Save belongs when content
// matching cannot decide alone.
func PromptAmbiguousBinding(gameTitle string, options []AmbiguousBindingOption) (AmbiguousBindingChoice, error) {
	if len(options) == 0 {
		return AmbiguousBindingChoice{}, ErrNoOmnisaves
	}
	if matchCount(options) == 0 {
		return promptUnmatchedBinding(gameTitle, options)
	}

	matchedOptions := make([]AmbiguousBindingOption, 0, len(options))
	for _, option := range options {
		if option.MatchedRevisionID != "" {
			matchedOptions = append(matchedOptions, option)
		}
	}
	selected := matchedOptions[0].OmnisaveID
	if err := multipleMatchesForm(gameTitle, matchedOptions, &selected).Run(); err != nil {
		return AmbiguousBindingChoice{}, bindingPromptError(err)
	}
	if selected == ambiguousChoiceCreate {
		return AmbiguousBindingChoice{Create: true}, nil
	}
	return AmbiguousBindingChoice{OmnisaveID: selected}, nil
}

func promptUnmatchedBinding(gameTitle string, options []AmbiguousBindingOption) (AmbiguousBindingChoice, error) {
	action := unmatchedBindingSync
	if err := unmatchedBindingActionForm(gameTitle, &action).Run(); err != nil {
		return AmbiguousBindingChoice{}, bindingPromptError(err)
	}
	if action == unmatchedBindingCreate {
		return AmbiguousBindingChoice{Create: true}, nil
	}

	selected := options[0].OmnisaveID
	if err := unmatchedBindingSaveForm(gameTitle, options, &selected).Run(); err != nil {
		return AmbiguousBindingChoice{}, bindingPromptError(err)
	}
	return AmbiguousBindingChoice{OmnisaveID: selected}, nil
}

func bindingPromptError(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func matchCount(options []AmbiguousBindingOption) int {
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

func unmatchedBindingSaveForm(gameTitle string, options []AmbiguousBindingOption, selected *string) *huh.Form {
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

func multipleMatchesForm(gameTitle string, options []AmbiguousBindingOption, selected *string) *huh.Form {
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
