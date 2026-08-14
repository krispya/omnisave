package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// The unit runs watch with no flags and no environment, so the only
// credential the service can ever use is the one connect persisted. A service
// installed before that starts, fails, and retries forever into a log nobody
// reads — the device is confidently doing nothing, which looks exactly like
// working from the outside. Refusing is what keeps those two apart.
func TestInstallingTheServiceRefusesAnUnconnectedDevice(t *testing.T) {
	if !service.Supported() {
		t.Skip("this platform has no service manager")
	}
	statePath := filepath.Join(t.TempDir(), "client.json")
	store := tracking.NewStore(statePath)
	if err := store.Save(tracking.NewState()); err != nil {
		t.Fatal(err)
	}

	err := installService(context.Background(), statePath)
	if err == nil {
		t.Fatal("install accepted a device with no connection")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Errorf("install reported %q; want the connect step it is waiting on", err)
	}
}

func TestServiceRejectsAnUnknownAction(t *testing.T) {
	err := runService(context.Background(), []string{"enable"})
	if err == nil {
		t.Fatal("service accepted an action it does not have")
	}
	for _, action := range []string{"install", "uninstall", "status"} {
		if !strings.Contains(err.Error(), action) {
			t.Errorf("service reported %q; want the actions it does have", err)
			break
		}
	}
}
