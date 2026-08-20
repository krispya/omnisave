package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestTheClientReleaseCommandIsUpdate(t *testing.T) {
	var help bytes.Buffer
	if err := runWithOutput(context.Background(), []string{"help"}, &help); err != nil {
		t.Fatalf("show help: %v", err)
	}
	if !strings.Contains(help.String(), "omnisave update") {
		t.Fatalf("help does not name the update command:\n%s", help.String())
	}
	if strings.Contains(help.String(), "omnisave upgrade") {
		t.Fatalf("help still names the old upgrade command:\n%s", help.String())
	}

	t.Setenv("OMNISAVE_BASE_URL", "https://releases.example")
	err := runWithOutput(context.Background(), []string{"update", "--check"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "pass --version") {
		t.Fatalf("update command returned %v; want its pinned-release requirement", err)
	}

	err = runWithOutput(context.Background(), []string{"upgrade"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `unknown command "upgrade"`) {
		t.Fatalf("old upgrade command returned %v; want an unknown-command error", err)
	}
}
