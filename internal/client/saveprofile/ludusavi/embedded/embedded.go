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

// Provider serves save profiles from the embedded manifest. The manifest is
// parsed on first lookup, so commands that never scan pay nothing for it.
func Provider() saveprofile.Provider {
	return &lazy{}
}

type lazy struct {
	once     sync.Once
	provider *ludusavi.Provider
	err      error
}

func (l *lazy) Find(ctx context.Context, identity target.GameIdentity) (*saveprofile.Profile, error) {
	l.once.Do(func() {
		l.provider, l.err = parse()
	})
	if l.err != nil {
		return nil, l.err
	}
	return l.provider.Find(ctx, identity)
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
