package tui

import (
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
)

// PromptStaleBinding asks how a local snapshot on a non-current revision
// should continue. It reads exactly like the divergence question: sync with
// the omnisave and choose which save becomes current, or fork.
func PromptStaleBinding(question savesync.StaleQuestion) (savesync.DivergedChoice, error) {
	return askSteps(question.GameTitle, staleSteps(question))
}

func staleSteps(question savesync.StaleQuestion) DivergedStep {
	return DivergedSteps(savesync.DivergedQuestion{
		GameTitle:    question.GameTitle,
		OmnisaveName: question.OmnisaveName,
		ForkName:     question.ForkName,
	})
}
