package savesync

import (
	"context"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// lineagePass is one reconciliation pass's view of the server's lineages:
// the single authority every consumer reads saves, histories, and
// path-format state through, so an in-pass migration is visible everywhere
// at once instead of wherever a copy happened to be taken.
type lineagePass struct {
	server    Server
	saves     map[string]*omnisave.Omnisave
	byGame    map[string][]string
	histories map[string][]omnisave.Revision
	loaded    map[string]bool
}

func newLineagePass(server Server, saves []omnisave.Omnisave) *lineagePass {
	pass := &lineagePass{
		server:    server,
		saves:     make(map[string]*omnisave.Omnisave, len(saves)),
		byGame:    make(map[string][]string),
		histories: make(map[string][]omnisave.Revision),
		loaded:    make(map[string]bool),
	}
	for _, listed := range saves {
		save := listed
		pass.saves[save.ID] = &save
		pass.byGame[save.GameID] = append(pass.byGame[save.GameID], save.ID)
	}
	return pass
}

// save is the current record for one lineage, reflecting any migration this
// pass has already performed.
func (p *lineagePass) save(id string) (omnisave.Omnisave, bool) {
	save, exists := p.saves[id]
	if !exists {
		return omnisave.Omnisave{}, false
	}
	return *save, true
}

// gameSaves lists a game's lineages as they stand right now.
func (p *lineagePass) gameSaves(gameID string) []omnisave.Omnisave {
	ids := p.byGame[gameID]
	saves := make([]omnisave.Omnisave, 0, len(ids))
	for _, id := range ids {
		saves = append(saves, *p.saves[id])
	}
	return saves
}

// history fetches one lineage's revisions at most once per pass.
func (p *lineagePass) history(ctx context.Context, id string) ([]omnisave.Revision, error) {
	if !p.loaded[id] {
		history, err := p.server.ListRevisions(ctx, id)
		if err != nil {
			return nil, err
		}
		p.histories[id] = history
		p.loaded[id] = true
	}
	return p.histories[id], nil
}

// migrated records a completed in-pass migration: the version advances for
// every reader and the cached history is dropped so the next read sees the
// renamed paths.
func (p *lineagePass) migrated(id string, version int) {
	if save, exists := p.saves[id]; exists {
		save.PathFormatVersion = version
	}
	delete(p.histories, id)
	delete(p.loaded, id)
}

// memoManifest reads and hashes a save at most once per candidate, however
// many decisions need the manifest in one pass.
func memoManifest(ctx context.Context, save target.Save) func() ([]omnisave.RevisionFile, error) {
	var manifest []omnisave.RevisionFile
	var err error
	ready := false
	return func() ([]omnisave.RevisionFile, error) {
		if !ready {
			ready = true
			manifest, err = binding.ManifestContext(ctx, save)
		}
		return manifest, err
	}
}

// revisionByID finds one revision in a history; a nil id finds nothing.
func revisionByID(history []omnisave.Revision, id *string) (omnisave.Revision, bool) {
	if id == nil {
		return omnisave.Revision{}, false
	}
	for _, revision := range history {
		if revision.ID == *id {
			return revision, true
		}
	}
	return omnisave.Revision{}, false
}
