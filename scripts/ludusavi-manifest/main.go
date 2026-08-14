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

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
)

const manifestURL = "https://raw.githubusercontent.com/mtkennerly/ludusavi-manifest/master/data/manifest.yaml"

func main() {
	input := flag.String("input", "", "local manifest to prune instead of downloading")
	url := flag.String("url", manifestURL, "manifest to download")
	output := flag.String("output", "internal/client/saveprofile/ludusavi/embedded/manifest.yaml.gz", "pruned manifest destination")
	flag.Parse()
	if err := run(*input, *url, *output); err != nil {
		fmt.Fprintf(os.Stderr, "ludusavi-manifest: %v\n", err)
		os.Exit(1)
	}
}

func run(input, url, output string) error {
	data, err := read(input, url)
	if err != nil {
		return err
	}
	pruned, err := ludusavi.Prune(data)
	if err != nil {
		return err
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(pruned); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
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

func read(input, url string) ([]byte, error) {
	if input != "" {
		return os.ReadFile(input)
	}
	response, err := http.Get(url)
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
