package tui

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// AmbiguousBindingOption describes one existing Omnisave a save could sync
// with, and whether its history contains the save's exact content.
type AmbiguousBindingOption struct {
	OmnisaveID        string
	Name              string
	MatchedRevisionID string
}

// AmbiguousBindingChoice resolves a save that matches zero or several
// Omnisaves. The zero value defers the decision to a later bind.
type AmbiguousBindingChoice struct {
	OmnisaveID string
	Seed       bool
}

// Sentinel select values for the non-Omnisave choices. A leading colon can
// never collide with a server-issued Omnisave ID.
const (
	ambiguousChoiceSeed  = ":seed-new"
	ambiguousChoiceDefer = ":decide-later"
)

// PromptAmbiguousBinding asks which Omnisave a save should sync with when
// content matching cannot decide alone.
func PromptAmbiguousBinding(gameTitle string, options []AmbiguousBindingOption) (AmbiguousBindingChoice, error) {
	var selected string
	form := ambiguousBindingForm(gameTitle, options, &selected)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return AmbiguousBindingChoice{}, ErrAborted
		}
		return AmbiguousBindingChoice{}, err
	}
	switch selected {
	case ambiguousChoiceSeed:
		return AmbiguousBindingChoice{Seed: true}, nil
	case ambiguousChoiceDefer:
		return AmbiguousBindingChoice{}, nil
	default:
		return AmbiguousBindingChoice{OmnisaveID: selected}, nil
	}
}

// ambiguousBindingForm selects over plain string values: huh's Select
// renders struct-typed options incorrectly, so the choice travels as the
// Omnisave ID with sentinels for seeding and deferring.
func ambiguousBindingForm(gameTitle string, options []AmbiguousBindingOption, selected *string) *huh.Form {
	matched := 0
	for _, option := range options {
		if option.MatchedRevisionID != "" {
			matched++
		}
	}
	// Open with the cursor on the likeliest answer: the first content match,
	// or the first Omnisave.
	if *selected == "" && len(options) > 0 {
		preferred := options[0]
		for _, option := range options {
			if option.MatchedRevisionID != "" {
				preferred = option
				break
			}
		}
		*selected = preferred.OmnisaveID
	}
	title := "Save matches none of this game's Omnisaves"
	if matched > 1 {
		title = "Save matches more than one Omnisave"
	}
	selections := make([]huh.Option[string], 0, len(options)+2)
	for _, option := range options {
		detail := " · different content"
		if option.MatchedRevisionID != "" {
			detail = " · matches this save"
		}
		selections = append(selections, huh.NewOption(option.Name+detail, option.OmnisaveID))
	}
	selections = append(selections,
		huh.NewOption("Seed a new Omnisave · start syncing from this save", ambiguousChoiceSeed),
		huh.NewOption("Decide later · leave this save unsynced", ambiguousChoiceDefer),
	)
	prompt := huh.NewSelect[string]().
		Title(title).
		Options(selections...).
		Value(selected)
	form := huh.NewForm(huh.NewGroup(prompt).Title(gameTitle))
	return form.WithTheme(trackingTheme())
}
