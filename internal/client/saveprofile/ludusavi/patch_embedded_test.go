package ludusavi

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedManifestContainsEveryPatch(t *testing.T) {
	entries, err := os.ReadDir("patches")
	if err != nil {
		t.Fatal(err)
	}
	patches := make(map[string][]byte)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		patches[entry.Name()], err = os.ReadFile(filepath.Join("patches", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
	}

	file, err := os.Open("embedded/manifest.yaml.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	manifest, err := io.ReadAll(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePatchesApplied(manifest, patches); err != nil {
		t.Fatalf("run make refresh-save-profiles: %v", err)
	}
}

func TestCompiledPatchIsVerifiedBySteamIdentity(t *testing.T) {
	manifest := []byte(`
Compiled Profile Title:
  files:
    <base>/nested/*.sav:
      when:
        - os: windows
  steam:
    id: 123
`)
	patch := []byte(`
steamId: "123"
title: "Different Upstream Title"
reason: "Example correction."
upstream: "https://example.com/game"
addFiles:
  <base>/nested/*.sav:
    when:
      - os: windows
`)
	if err := validatePatchesApplied(manifest, map[string][]byte{"123-example.yaml": patch}); err != nil {
		t.Fatal(err)
	}
}

func TestCompiledManifestMustContainNewPatch(t *testing.T) {
	manifest := []byte(`
Example:
  files:
    <base>/*.sav: {}
  steam:
    id: 123
`)
	patch := []byte(`
steamId: "123"
title: "Example"
reason: "Example correction."
upstream: "https://example.com/game"
addFiles:
  <base>/nested/*.sav: {}
`)
	if err := validatePatchesApplied(manifest, map[string][]byte{"123-example.yaml": patch}); err == nil {
		t.Fatal("expected an uncompiled patch to fail")
	}
}
