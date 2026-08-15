// Package embedded ships the pruned Ludusavi manifest inside the client
// binary, so save-location knowledge needs no network, configuration, or
// server. Refresh manifest.yaml.gz with make refresh-save-profiles.
package embedded

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"sync"

	_ "embed"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

//go:embed manifest.yaml.gz
var compressed []byte

// load parses the embedded manifest once per process, on first use, so
// commands that never scan pay nothing for it.
var load = sync.OnceValues(parse)

// Provider serves save profiles from the embedded manifest.
func Provider() saveprofile.Provider {
	return provider{}
}

type provider struct{}

func (provider) Find(ctx context.Context, identity target.GameIdentity) (*saveprofile.Profile, error) {
	parsed, err := load()
	if err != nil {
		return nil, err
	}
	return parsed.Find(ctx, identity)
}

func parse() (*ludusavi.Provider, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open embedded save profiles: %w", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read embedded save profiles: %w", err)
	}
	return ludusavi.New(data)
}
