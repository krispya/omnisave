package savesync

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// StatSignature summarizes paths by size and modification time; any
// difference between two summaries means something wrote. The watch loop
// polls watched files with it, and a pass remembers a verified save by it, so
// a write the poll would notice is a write the pass will not mistake for
// stillness.
func StatSignature(paths []string) string {
	var summary strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&summary, "%s:missing;", path)
			continue
		}
		fmt.Fprintf(&summary, "%s:%d:%d;", path, info.Size(), info.ModTime().UnixNano())
	}
	return summary.String()
}

// saveSignature summarizes one save's files as StatSignature does.
func saveSignature(save target.Save) string {
	paths := make([]string, 0, len(save.Files))
	for _, file := range save.Files {
		paths = append(paths, file.Path)
	}
	// Adapters are free to discover a save's files in any order; the summary
	// must describe the save, not the order it came back in.
	sort.Strings(paths)
	return StatSignature(paths)
}

// settledSince reports that nothing can have happened to a bound save since
// the pass that last verified it: its files carry the summary that pass
// recorded, and the Omnisave still points at the revision they were proved
// equal to. Both halves are needed — unchanged files under a moved current
// is a pull, and moved files under an unchanged current is a commit.
func settledSince(bound tracking.Binding, remoteSave omnisave.Omnisave, signature string) bool {
	if bound.LocalSignature == "" || bound.LocalSignature != signature {
		return false
	}
	return bound.LastSyncedRevisionID != nil && remoteSave.CurrentRevisionID != nil &&
		*bound.LastSyncedRevisionID == *remoteSave.CurrentRevisionID
}
