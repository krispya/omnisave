package ludusavi_test

import (
	"context"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func TestPatchAddsAReviewedPathWithoutReplacingUpstreamKnowledge(t *testing.T) {
	patched, err := ludusavi.ApplyPatches([]byte(`
"Lisa: The First":
  files:
    <base>/*.lsd:
      tags: [save]
      when:
        - os: windows
  steam:
    id: 2743030
`), map[string][]byte{
		"2743030-lisa-the-first.yaml": []byte(`
steamId: "2743030"
title: "Lisa: The First"
reason: "The Steam package nests the game beneath its install root."
upstream: "https://www.pcgamingwiki.com/wiki/Lisa:_The_First"
addFiles:
  <base>/Lisa_1/Lisa_1/*.lsd:
    tags: [save]
    when:
      - os: windows
`),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := ludusavi.New(patched)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := provider.Find(context.Background(), target.GameIdentity{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "2743030"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, rule := range profile.Rules {
		found[rule.Path] = true
	}
	for _, path := range []string{"<base>/*.lsd", "<base>/Lisa_1/Lisa_1/*.lsd"} {
		if !found[path] {
			t.Fatalf("expected profile to retain %q, got %+v", path, profile.Rules)
		}
	}
}

func TestPatchRefusesToDriftFromItsUpstreamTitle(t *testing.T) {
	_, err := ludusavi.ApplyPatches([]byte(`
Renamed Game:
  files:
    <base>/*.sav: {}
  steam:
    id: 123
`), map[string][]byte{
		"123-example.yaml": []byte(`
steamId: "123"
title: "Old Game Name"
reason: "Example correction."
upstream: "https://example.com/game"
addFiles:
  <base>/nested/*.sav: {}
`),
	})
	if err == nil {
		t.Fatal("expected a stale patch title to fail")
	}
}

func TestPatchIsIdempotentAfterItsPathArrivesUpstream(t *testing.T) {
	manifest := []byte(`
Example:
  files:
    <base>/nested/*.sav:
      when:
        - os: windows
  steam:
    id: 123
`)
	patch := []byte(`
steamId: "123"
title: "Example"
reason: "Example correction."
upstream: "https://example.com/game"
addFiles:
  <base>/nested/*.sav:
    when:
      - os: windows
`)
	patched, err := ludusavi.ApplyPatches(manifest, map[string][]byte{"123-example.yaml": patch})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := ludusavi.New(patched)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := provider.Find(context.Background(), target.GameIdentity{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 1 {
		t.Fatalf("expected the upstream and patched path to collapse, got %+v", profile.Rules)
	}
}
