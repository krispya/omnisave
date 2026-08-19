package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
)

// DivergedBindingChoice keeps both sides recoverable: forking continues the
// local progress as its own lineage, jumping keeps it reachable — as a branch
// in the same tree when it is unsynced, or not at all when the history
// already holds it — and takes the Current Revision.
type DivergedBindingChoice string

const (
	// DivergedBindingFork continues this device's progress as a new lineage.
	DivergedBindingFork DivergedBindingChoice = "fork"
	// DivergedBindingJump takes the Current Revision, keeping any unsynced
	// local progress as a branch of the baseline first.
	DivergedBindingJump DivergedBindingChoice = "jump"
)

// DivergedKeep says what "take current" would do with the local progress, so
// the option can promise exactly that (FDR-005, decision 4).
type DivergedKeep string

const (
	// KeepAsBranch shelves unsynced progress as a branch of the baseline,
	// inside the same omnisave.
	KeepAsBranch DivergedKeep = "branch"
	// KeepAsSave seeds a new omnisave: a baseline-less binding has no node
	// to branch from.
	KeepAsSave DivergedKeep = "save"
	// KeepNothing preserves nothing: the history already holds this content.
	KeepNothing DivergedKeep = "held"
)

// DivergedQuestion is one diverged save put to the user: the game, the
// omnisave that moved on the server, the name forking would create, and what
// taking current would do with the local progress. The pass that found the
// divergence fills it in, so both surfaces promise exactly what the answers
// will do.
type DivergedQuestion struct {
	GameTitle    string
	OmnisaveName string
	// ForkName is the deconflict name a fork or seed would carry ("Save 1
	// (Steam Deck)"); empty when the Device is unnamed and the server's
	// default applies.
	ForkName string
	Keep     DivergedKeep
}

// DivergedOption is one answer as the user reads it. The label is shared by
// the track run's form and the watch view's modal, so the two surfaces
// cannot drift into naming the same choice differently. "Take current"
// rather than "jump": the glossary already spends that word on a restore
// between sibling branches, and neither surface has room to disambiguate.
type DivergedOption struct {
	Label       string
	Description string
	Choice      DivergedBindingChoice
}

// DivergedOptions is the answer set, in the order both surfaces show it,
// worded for what each answer would do to this particular save. The labels
// already say which side wins, so each description only adds what happens
// to the local progress.
func DivergedOptions(question DivergedQuestion) []DivergedOption {
	forkAs := "a new playthrough"
	if question.ForkName != "" {
		forkAs = question.ForkName
	}
	jump := "keep this progress as a branch"
	switch question.Keep {
	case KeepAsSave:
		jump = "keep this progress as " + forkAs
	case KeepNothing:
		jump = "this progress is already in the history"
	}
	return []DivergedOption{
		{
			Label:       "Fork here",
			Description: "continue as " + forkAs,
			Choice:      DivergedBindingFork,
		},
		{
			Label:       "Take current",
			Description: jump,
			Choice:      DivergedBindingJump,
		},
	}
}

// PromptDivergedBinding asks how a diverged save should continue.
func PromptDivergedBinding(question DivergedQuestion) (DivergedBindingChoice, error) {
	choice := DivergedBindingFork
	form := divergedBindingForm(question, &choice)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return choice, nil
}

func divergedBindingForm(question DivergedQuestion, choice *DivergedBindingChoice) *huh.Form {
	answers := DivergedOptions(question)
	options := make([]huh.Option[DivergedBindingChoice], 0, len(answers))
	for _, option := range answers {
		options = append(options, huh.NewOption(option.Label+" · "+option.Description, option.Choice))
	}
	prompt := huh.NewSelect[DivergedBindingChoice]().
		Title(fmt.Sprintf("Save changed here and %s moved on the server", question.OmnisaveName)).
		Options(options...).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(question.GameTitle))
	return form.WithTheme(trackingTheme())
}
