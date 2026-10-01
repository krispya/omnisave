package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/device"
	deviceservice "github.com/krisbaumgartner/omnisave/internal/device/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite/sqlitetest"
)

// A Device registers once, then reports what it is playing; the server shows
// that report to readers until it goes stale.
func TestARegisteredDeviceReportsWhatItIsPlaying(t *testing.T) {
	ctx := context.Background()
	devices := deviceservice.New(sqlitetest.Open(t))
	if _, err := devices.Register(ctx, "steam-deck", device.Registration{Name: "Steam Deck", Platform: "linux"}); err != nil {
		t.Fatal(err)
	}

	changed, err := devices.ReportPlaying(ctx, "steam-deck", []string{"hades"})
	if err != nil || !changed {
		t.Fatalf("expected the first report to change what readers see: changed=%t err=%v", changed, err)
	}
	if !devices.Playing("steam-deck", "hades") {
		t.Fatal("expected the Device to read as playing the reported game")
	}
	presence := devices.Presence()
	if len(presence) != 1 || presence[0].DeviceID != "steam-deck" || presence[0].PlayingGameIDs[0] != "hades" {
		t.Fatalf("unexpected presence: %+v", presence)
	}

	if changed, err := devices.ReportPlaying(ctx, "steam-deck", []string{"hades"}); err != nil || changed {
		t.Fatalf("expected re-affirming the same picture to change nothing: changed=%t err=%v", changed, err)
	}
	if changed, err := devices.ReportPlaying(ctx, "steam-deck", nil); err != nil || !changed {
		t.Fatalf("expected clearing a live session to change what readers see: changed=%t err=%v", changed, err)
	}
	if len(devices.Presence()) != 0 {
		t.Fatalf("expected no presence after clearing, got %+v", devices.Presence())
	}
}

func TestOnlyARegisteredDeviceCanReportPresence(t *testing.T) {
	devices := deviceservice.New(sqlitetest.Open(t))
	if _, err := devices.ReportPlaying(context.Background(), "stranger", []string{"hades"}); !errors.Is(err, device.ErrNotFound) {
		t.Fatalf("expected an unregistered Device to be refused, got %v", err)
	}
	if len(devices.Presence()) != 0 {
		t.Fatal("a refused report must not appear in presence")
	}
}
