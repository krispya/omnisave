package upgrade_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/upgrade"
)

// release serves one published release the way the installers expect to find
// it: an archive per platform, and one checksum file covering them.
func release(t *testing.T, version string, binary []byte) *httptest.Server {
	t.Helper()
	archive := archiveOf(t, "omnisave", binary)
	archiveName := fmt.Sprintf("omnisave-%s-linux-amd64.tar.gz", version)
	digest := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), archiveName)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/krispya/omnisave/releases/latest", func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(writer, `{"tag_name": "v%s"}`, version)
	})
	mux.HandleFunc("/"+archiveName, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write(archive)
	})
	mux.HandleFunc("/omnisave-"+version+"-checksums.txt", func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, checksums)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func archiveOf(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressor := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressor)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := archive.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}
	return buffer.Bytes()
}

func TestUpgradingInstallsTheNewestPublishedRelease(t *testing.T) {
	server := release(t, "0.2.0", []byte("the 0.2.0 client"))
	upgrader := &upgrade.Upgrader{
		APIBase:      server.URL,
		DownloadBase: server.URL,
		GOOS:         "linux",
		GOARCH:       "amd64",
	}

	newest, err := upgrader.Latest(context.Background())
	if err != nil {
		t.Fatalf("resolve latest: %v", err)
	}
	if newest != "0.2.0" {
		t.Fatalf("latest = %q, want 0.2.0", newest)
	}

	binary, err := upgrader.Download(context.Background(), newest)
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	installed := filepath.Join(t.TempDir(), "omnisave")
	if err := os.WriteFile(installed, []byte("the 0.1.0 client"), 0o755); err != nil {
		t.Fatalf("seed the installed client: %v", err)
	}
	if err := upgrade.Replace(installed, binary); err != nil {
		t.Fatalf("replace: %v", err)
	}

	content, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("read the installed client: %v", err)
	}
	if string(content) != "the 0.2.0 client" {
		t.Fatalf("installed client = %q, want the 0.2.0 client", content)
	}
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed client is not executable: %v", info.Mode())
	}
}

func TestUpgradingRefusesAnArchiveThatFailsItsChecksum(t *testing.T) {
	// A release whose checksum file disagrees with the archive beside it is
	// what a tampered or truncated download looks like.
	archive := archiveOf(t, "omnisave", []byte("a substituted client"))
	mux := http.NewServeMux()
	mux.HandleFunc("/omnisave-0.2.0-linux-amd64.tar.gz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write(archive)
	})
	mux.HandleFunc("/omnisave-0.2.0-checksums.txt", func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, "0000000000000000000000000000000000000000000000000000000000000000  omnisave-0.2.0-linux-amd64.tar.gz\n")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	upgrader := &upgrade.Upgrader{DownloadBase: server.URL, GOOS: "linux", GOARCH: "amd64"}
	if _, err := upgrader.Download(context.Background(), "0.2.0"); err == nil {
		t.Fatal("expected the download to be refused")
	}
}

func TestUpgradingReportsAReleaseThatDoesNotPublishThisPlatform(t *testing.T) {
	server := release(t, "0.2.0", []byte("the 0.2.0 client"))
	upgrader := &upgrade.Upgrader{DownloadBase: server.URL, GOOS: "linux", GOARCH: "arm64"}

	_, err := upgrader.Download(context.Background(), "0.2.0")
	if err == nil {
		t.Fatal("expected an unpublished platform to fail")
	}
}

func TestAReplacedClientKeepsTheDirectoryClean(t *testing.T) {
	// Replace stages beside the target, so a failure must not leave debris
	// that a later install would trip over.
	directory := t.TempDir()
	installed := filepath.Join(directory, "omnisave")
	if err := os.WriteFile(installed, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := upgrade.Replace(installed, []byte("new")); err != nil {
		t.Fatalf("replace: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "omnisave" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only omnisave", names)
	}
}

func TestOnlyALaterReleaseCountsAsNewer(t *testing.T) {
	cases := []struct {
		installed string
		candidate string
		newer     bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "0.1.1", true},
		{"1.0.0", "0.9.9", false},
		{"0.2.0", "0.2.0", false},
		// A development build has no released version to compare, so any
		// published release is an upgrade from it.
		{"dev", "0.1.0", true},
		{"0.1.0", "not-a-version", false},
	}
	for _, test := range cases {
		if got := upgrade.Newer(test.installed, test.candidate); got != test.newer {
			t.Errorf("Newer(%q, %q) = %v, want %v", test.installed, test.candidate, got, test.newer)
		}
	}
}
