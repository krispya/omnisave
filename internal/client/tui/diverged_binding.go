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

// DivergedBindingDefault is the answer both surfaces open on. Jumping is the
// answer that ends the divergence — this Device rejoins the lineage everyone
// else is on — while forking is the deliberate decision to keep two
// playthroughs apart, so the default resolves rather than multiplies. Landing
// on it destroys nothing either way: both answers keep the local progress
// (FDR-005, decision 4).
const DivergedBindingDefault = DivergedBindingJump

// DivergedQuestion is one diverged save put to the user: the game, the
// omnisave that diverged, and the name forking would create. The pass that
// found the divergence fills it in, so both surfaces name the fork the answer
// would actually produce.
type DivergedQuestion struct {
	GameTitle    string
	OmnisaveName string
	// ForkName is the deconflict name a fork or seed would carry ("Save 1
	// (Steam Deck)"); empty when the Device is unnamed and the server's
	// default applies.
	ForkName string
}

// DivergedOption is one answer as the user reads it. The label is shared by
// the track run's form and the watch view's modal, so the two surfaces cannot
// drift into naming the same choice differently. The label is the whole
// interface: it says what the answer does and, for a fork, what it creates.
type DivergedOption struct {
	Label  string
	Choice DivergedBindingChoice
}

// DivergedOptions is the answer set, in the order both surfaces show it.
func DivergedOptions(question DivergedQuestion) []DivergedOption {
	forkAs := "a new save"
	if question.ForkName != "" {
		forkAs = question.ForkName
	}
	return []DivergedOption{
		{Label: "Fork as " + forkAs, Choice: DivergedBindingFork},
		{Label: "Jump to current", Choice: DivergedBindingJump},
	}
}

// DivergedDefaultIndex is where a surface parks its cursor: the position of
// DivergedBindingDefault in the answer set.
func DivergedDefaultIndex(options []DivergedOption) int {
	for index, option := range options {
		if option.Choice == DivergedBindingDefault {
			return index
		}
	}
	return 0
}

// PromptDivergedBinding asks how a diverged save should continue.
func PromptDivergedBinding(question DivergedQuestion) (DivergedBindingChoice, error) {
	choice := DivergedBindingDefault
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
		options = append(options, huh.NewOption(option.Label, option.Choice))
	}
	prompt := huh.NewSelect[DivergedBindingChoice]().
		Title(fmt.Sprintf("%s diverges between this device and the server", question.OmnisaveName)).
		Options(options...).
		Value(choice)
	form := huh.NewForm(huh.NewGroup(prompt).Title(question.GameTitle))
	return form.WithTheme(trackingTheme())
}
