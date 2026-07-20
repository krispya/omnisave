package sqlite

import (
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// Artifacts are identified by the SHA-256 and size of their uncompressed
// content, but rest on disk gzip-compressed as "<sha>.gz" with the logical
// size in the artifacts table. Artifacts predating compression are plain
// "<sha>" files and stay readable; nothing rewrites them.

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
	compressor := gzip.NewWriter(temporary)
	size, copyErr := io.Copy(io.MultiWriter(compressor, hash), payload)
	compressErr := compressor.Close()
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if compressErr != nil {
		return compressErr
	}
	if closeErr != nil {
		return closeErr
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if size != artifact.Size || actualHash != artifact.SHA256 {
		return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
	}

	destination := r.compressedArtifactPath(artifact.SHA256)
	switch _, err := os.Stat(destination); {
	case err == nil:
		// Identical content already rests here; only ensure its metadata.
		return r.recordArtifact(artifact.SHA256, artifact.Size)
	case !os.IsNotExist(err):
		return err
	}
	if _, err := os.Stat(r.artifactPath(artifact.SHA256)); err == nil {
		// A pre-compression artifact carries this content already.
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			return r.recordArtifact(artifact.SHA256, artifact.Size)
		}
		return err
	}
	if err := os.Chmod(destination, 0644); err != nil {
		return err
	}
	return r.recordArtifact(artifact.SHA256, artifact.Size)
}

func (r *Repository) recordArtifact(hash string, size int64) error {
	_, err := r.db.Exec(`INSERT OR REPLACE INTO artifacts(sha256, size) VALUES (?, ?)`, hash, size)
	return err
}

func (r *Repository) openArtifact(hash string) (io.ReadCloser, error) {
	if !validHash(hash) {
		return nil, storage.ErrNotFound
	}
	if file, err := os.Open(r.compressedArtifactPath(hash)); err == nil {
		decompressor, gzErr := gzip.NewReader(file)
		if gzErr != nil {
			file.Close()
			return nil, gzErr
		}
		return &compressedArtifact{decompressor: decompressor, file: file}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.Open(r.artifactPath(hash))
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	return file, err
}

type compressedArtifact struct {
	decompressor *gzip.Reader
	file         *os.File
}

func (c *compressedArtifact) Read(p []byte) (int, error) {
	return c.decompressor.Read(p)
}

func (c *compressedArtifact) Close() error {
	decompressErr := c.decompressor.Close()
	fileErr := c.file.Close()
	if decompressErr != nil {
		return decompressErr
	}
	return fileErr
}

func (r *Repository) statArtifact(hash string) (int64, error) {
	if !validHash(hash) {
		return 0, storage.ErrNotFound
	}
	var size int64
	err := r.db.QueryRow(`SELECT size FROM artifacts WHERE sha256 = ?`, hash).Scan(&size)
	if err == nil {
		return size, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	info, statErr := os.Stat(r.artifactPath(hash))
	if os.IsNotExist(statErr) {
		return 0, storage.ErrNotFound
	}
	if statErr != nil {
		return 0, statErr
	}
	return info.Size(), nil
}

// removeArtifact deletes an unreferenced artifact's content — compressed
// or legacy raw — and its metadata row.
func (r *Repository) removeArtifact(hash string) error {
	if err := os.Remove(r.compressedArtifactPath(hash)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(r.artifactPath(hash)); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM artifacts WHERE sha256 = ?`, hash)
	return err
}

func (r *Repository) artifactPath(hash string) string {
	return filepath.Join(r.artifactDir, hash)
}

func (r *Repository) compressedArtifactPath(hash string) string {
	return r.artifactPath(hash) + ".gz"
}

func validHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && hash == strings.ToLower(hash)
}
