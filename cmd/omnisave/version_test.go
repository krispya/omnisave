package main

import (
	"bytes"
	"context"
	"testing"
)

func TestVersionFlagsPrintTheClientVersion(t *testing.T) {
	for _, argument := range []string{"--version", "-v"} {
		t.Run(argument, func(t *testing.T) {
			var output bytes.Buffer
			if err := runWithOutput(context.Background(), []string{argument}, &output); err != nil {
				t.Fatal(err)
			}
			if got, want := output.String(), formattedVersion()+"\n"; got != want {
				t.Fatalf("expected %q, got %q", want, got)
			}
		})
	}
}

func TestVersionIdentifiesTheBuild(t *testing.T) {
	originalVersion, originalBuild := version, buildNumber
	t.Cleanup(func() {
		version, buildNumber = originalVersion, originalBuild
	})
	version = "0.2.0"
	buildNumber = "1234"

	if got, want := formattedVersion(), "omnisave 0.2.0-1234"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
