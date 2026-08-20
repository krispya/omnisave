// Command ludusavi-manifest refreshes the pruned save-profile manifest that
// ships inside the client binary. Run it from the repository root, typically
// via make refresh-save-profiles.
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
)

const manifestURL = "https://raw.githubusercontent.com/mtkennerly/ludusavi-manifest/master/data/manifest.yaml"
const defaultPatchesDirectory = "internal/client/saveprofile/ludusavi/patches"

func main() {
	input := flag.String("input", "", "local manifest to prune instead of downloading")
	url := flag.String("url", manifestURL, "manifest to download")
	output := flag.String("output", "internal/client/saveprofile/ludusavi/embedded/manifest.yaml.gz", "pruned manifest destination")
	patches := flag.String("patches", defaultPatchesDirectory, "directory of checked-in manifest patches")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ludusavi-manifest: unexpected arguments %v; a local manifest is passed with -input\n", flag.Args())
		os.Exit(1)
	}
	if err := run(*input, *url, *output, *patches); err != nil {
		fmt.Fprintf(os.Stderr, "ludusavi-manifest: %v\n", err)
		os.Exit(1)
	}
}

func run(input, url, output, patchesDirectory string) error {
	data, err := read(input, url)
	if err != nil {
		return err
	}
	patches, err := readPatches(patchesDirectory)
	if err != nil {
		return err
	}
	data, err = ludusavi.ApplyPatches(data, patches)
	if err != nil {
		return err
	}
	pruned, err := ludusavi.Prune(data)
	if err != nil {
		return err
	}
	if err := writeCompressed(output, pruned); err != nil {
		return err
	}
	info, err := os.Stat(output)
	if err != nil {
		return err
	}
	fmt.Printf("pruned %.1f MB manifest to %.1f MB, %.1f MB compressed at %s\n",
		megabytes(len(data)), megabytes(len(pruned)), megabytes(int(info.Size())), output)
	return nil
}

func readPatches(directory string) (map[string][]byte, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Ludusavi patches: %w", err)
	}
	patches := make(map[string][]byte)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read Ludusavi patch %s: %w", entry.Name(), err)
		}
		patches[entry.Name()] = data
	}
	return patches, nil
}

// writeCompressed replaces the destination atomically so an interrupted
// refresh can never leave a truncated manifest for go:embed to ship.
func writeCompressed(output string, pruned []byte) error {
	file, err := os.CreateTemp(filepath.Dir(output), ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		file.Close()
		return err
	}
	if _, err := writer.Write(pruned); err != nil {
		file.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), output)
}

func read(input, url string) ([]byte, error) {
	if input != "" {
		return os.ReadFile(input)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download manifest: %s", response.Status)
	}
	return io.ReadAll(response.Body)
}

func megabytes(size int) float64 {
	return float64(size) / (1024 * 1024)
}
