// Package upgrade replaces the running client with a published release.
//
// It reads the same release layout the installers do (ADR-015): one archive
// per platform named for its version, and one checksum file covering all of
// them. A download that does not match its recorded digest is never installed.
package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DefaultRepo is the repository releases are published from.
const DefaultRepo = "krispya/omnisave"

// maxArchiveBytes bounds what a release download may expand to. The client is
// a few megabytes; this is large enough not to constrain it and small enough
// that a wrong or hostile URL cannot exhaust memory.
const maxArchiveBytes = 128 << 20

// ErrNotFound reports that a release, or an archive for this platform within
// it, is not published.
var ErrNotFound = errors.New("release not found")

// Upgrader resolves and downloads client releases.
type Upgrader struct {
	// Repo is the owner/name releases are published under.
	Repo string
	// APIBase is where release metadata is resolved. Empty means GitHub.
	APIBase string
	// DownloadBase is a directory URL holding one release's archives. Empty
	// derives it from Repo and the version being installed, which is the
	// normal case; setting it installs from somewhere else, exactly as
	// OMNISAVE_BASE_URL does for the install script.
	DownloadBase string
	// HTTP is the client used for every request. Empty means http.DefaultClient.
	HTTP *http.Client
	// GOOS and GOARCH name the platform to download for. Empty means this one.
	GOOS   string
	GOARCH string
}

func (u *Upgrader) repo() string {
	if u.Repo != "" {
		return u.Repo
	}
	return DefaultRepo
}

func (u *Upgrader) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return http.DefaultClient
}

func (u *Upgrader) platform() (string, string) {
	goos, goarch := u.GOOS, u.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

// These names are the release layout itself, shared with scripts/package-client.sh
// and the installers. Changing one without the others breaks upgrading.
func archiveName(version, goos, goarch string) string {
	extension := "tar.gz"
	if goos == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("omnisave-%s-%s-%s.%s", version, goos, goarch, extension)
}

func checksumsName(version string) string {
	return "omnisave-" + version + "-checksums.txt"
}

// binaryName is what the archive holds, and what lands on disk.
func binaryName(goos string) string {
	if goos == "windows" {
		return "omnisave.exe"
	}
	return "omnisave"
}

// Latest reports the newest published version, without its leading v.
func (u *Upgrader) Latest(ctx context.Context) (string, error) {
	base := u.APIBase
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimSuffix(base, "/"), u.repo())
	body, err := u.get(ctx, url)
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("could not read the release listing: %w", err)
	}
	if release.TagName == "" {
		return "", ErrNotFound
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// Download returns the client binary from a release, verified against that
// release's checksum file. It never writes to disk.
func (u *Upgrader) Download(ctx context.Context, version string) ([]byte, error) {
	goos, goarch := u.platform()
	base := u.DownloadBase
	if base == "" {
		base = fmt.Sprintf("https://github.com/%s/releases/download/v%s", u.repo(), version)
	}
	base = strings.TrimSuffix(base, "/")

	archive := archiveName(version, goos, goarch)
	payload, err := u.get(ctx, base+"/"+archive)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w", archive, err)
	}
	checksums, err := u.get(ctx, base+"/"+checksumsName(version))
	if err != nil {
		return nil, fmt.Errorf("could not download the checksums for %s: %w", version, err)
	}

	expected, err := recordedDigest(checksums, archive)
	if err != nil {
		return nil, err
	}
	actual := sha256.Sum256(payload)
	if hex.EncodeToString(actual[:]) != expected {
		return nil, fmt.Errorf("%s does not match its recorded checksum", archive)
	}

	return extract(payload, binaryName(goos))
}

func (u *Upgrader) get(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := u.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxArchiveBytes))
}

// recordedDigest finds one archive's digest in a checksum file.
func recordedDigest(checksums []byte, archive string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archive {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s is not listed in the release checksums", archive)
}

// extract pulls the named binary out of a release archive.
func extract(payload []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".exe") {
		return extractZip(payload, name)
	}
	return extractTarGz(payload, name)
}

func extractTarGz(payload []byte, name string) ([]byte, error) {
	decompressed, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("release archive is not gzip: %w", err)
	}
	defer decompressed.Close()
	archive := tar.NewReader(decompressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the release archive: %w", err)
		}
		if filepath.Base(header.Name) != name {
			continue
		}
		return io.ReadAll(io.LimitReader(archive, maxArchiveBytes))
	}
	return nil, fmt.Errorf("release archive does not contain %s", name)
}

func extractZip(payload []byte, name string) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("release archive is not a zip: %w", err)
	}
	for _, entry := range archive.File {
		if filepath.Base(entry.Name) != name {
			continue
		}
		file, err := entry.Open()
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, maxArchiveBytes))
	}
	return nil, fmt.Errorf("release archive does not contain %s", name)
}

// Replace installs a binary at path, keeping the running client usable
// throughout: the new file is written beside the old one and moved over it, so
// no reader ever sees a partial binary.
func Replace(path string, binary []byte) error {
	directory := filepath.Dir(path)
	staged, err := os.CreateTemp(directory, ".omnisave-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", directory, err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)

	if _, err := staged.Write(binary); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}

	// Windows refuses to overwrite a file that is open for execution, so the
	// running binary is renamed out of the way first. The displaced copy
	// cannot be deleted until it stops running; the next upgrade sweeps it.
	if runtime.GOOS == "windows" {
		displaced := path + ".old-" + strconv.Itoa(os.Getpid())
		os.Remove(displaced)
		if err := os.Rename(path, displaced); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot replace %s; stop a running omnisave watch and try again: %w", path, err)
		}
		if err := os.Rename(stagedPath, path); err != nil {
			// Put the working client back rather than leaving nothing there.
			os.Rename(displaced, path)
			return err
		}
		sweepDisplaced(directory)
		return nil
	}

	if err := os.Rename(stagedPath, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	return nil
}

// sweepDisplaced removes binaries a previous Windows upgrade could not delete.
func sweepDisplaced(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "omnisave.exe.old-") {
			os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
}

// Path is where the running client lives, with symlinks resolved so an
// upgrade replaces the binary rather than a link to it.
func Path() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return executable, nil
	}
	return resolved, nil
}

// Newer reports whether candidate is a later version than installed. An
// installed version that is not a release version — a development build —
// counts as older than anything published.
func Newer(installed, candidate string) bool {
	candidateParts, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	installedParts, ok := parseVersion(installed)
	if !ok {
		return true
	}
	for index := range candidateParts {
		if candidateParts[index] != installedParts[index] {
			return candidateParts[index] > installedParts[index]
		}
	}
	return false
}

func parseVersion(version string) ([3]int, bool) {
	var parsed [3]int
	fields := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(fields) != 3 {
		return parsed, false
	}
	for index, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return parsed, false
		}
		parsed[index] = value
	}
	return parsed, true
}
