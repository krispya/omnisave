package gamesave

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// DirectoryFiles captures regular-file membership relative to one literal
// native root. Symlinks are excluded. Enumeration failures refuse a partial
// save slot instead of silently shrinking its ownership boundary.
func DirectoryFiles(ctx context.Context, root, locationID string) ([]target.File, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("save directory unavailable")
	}
	var files []target.File
	err = filepath.WalkDir(root, func(native string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("save directory cannot be enumerated completely")
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("save file metadata unavailable")
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, native)
		if err != nil {
			return fmt.Errorf("save file is outside its directory")
		}
		files = append(files, target.File{Path: native, LocationID: locationID, RelativePath: relative, Size: info.Size(), Modified: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
