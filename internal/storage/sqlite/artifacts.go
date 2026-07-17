package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/storage"
)

func (r *Repository) storeArtifact(artifact storage.Artifact, payload io.Reader) error {
	if !validHash(artifact.SHA256) || artifact.Size < 0 {
		return fmt.Errorf("%w: invalid descriptor", storage.ErrArtifactMismatch)
	}

	temporary, err := os.CreateTemp(r.artifactDir, ".artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hash), payload)
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if size != artifact.Size || actualHash != artifact.SHA256 {
		return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
	}

	destination := r.artifactPath(artifact.SHA256)
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			return nil
		}
		return err
	}
	return os.Chmod(destination, 0644)
}

func (r *Repository) openArtifact(hash string) (io.ReadCloser, error) {
	if !validHash(hash) {
		return nil, storage.ErrNotFound
	}
	file, err := os.Open(r.artifactPath(hash))
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	return file, err
}

func (r *Repository) statArtifact(hash string) (int64, error) {
	if !validHash(hash) {
		return 0, storage.ErrNotFound
	}
	info, err := os.Stat(r.artifactPath(hash))
	if os.IsNotExist(err) {
		return 0, storage.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (r *Repository) artifactPath(hash string) string {
	return filepath.Join(r.artifactDir, hash)
}

func validHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && hash == strings.ToLower(hash)
}
