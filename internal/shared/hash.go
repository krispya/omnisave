package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// HashFile computes the SHA256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return HashReader(f)
}

// HashBytes computes the SHA256 hex digest of b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashReader computes the SHA256 hex digest from an io.Reader.
func HashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
