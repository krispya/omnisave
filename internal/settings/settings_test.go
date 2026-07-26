package settings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/settings"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestAnnouncingIsOnUntilTheOwnerSaysOtherwise(t *testing.T) {
	ctx := context.Background()
	service := settings.New(storagetest.NewMemoryRepository(), nil)

	setting, err := service.Get(ctx, settings.AnnounceDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Value || setting.Source != settings.SourceDefault || !setting.Editable {
		t.Fatalf("the default announcement setting is %+v", setting)
	}
}

func TestAnOwnersChoiceIsStoredAndAppliesImmediately(t *testing.T) {
	ctx := context.Background()
	service := settings.New(storagetest.NewMemoryRepository(), nil)
	applied := []settings.Setting{}
	service.OnChange(func(changed settings.Setting) { applied = append(applied, changed) })

	updated, err := service.Set(ctx, settings.AnnounceDiscovery, "false")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Value || updated.Source != settings.SourceOwner {
		t.Fatalf("turning the announcement off recorded %+v", updated)
	}
	if len(applied) != 1 || applied[0].Value {
		t.Fatalf("the change was stored without being applied: %+v", applied)
	}
	if service.Enabled(ctx, settings.AnnounceDiscovery) {
		t.Fatal("the announcement is still on after being turned off")
	}
}

func TestADeploymentPinWinsAndIsNotOffered(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	// An owner who set this before the operator pinned it: the stored value
	// stays, and stops being what the server does.
	if err := repository.SetOwnerSetting(ctx, settings.AnnounceDiscovery, "true", time.Time{}); err != nil {
		t.Fatal(err)
	}
	service := settings.New(repository, map[string]string{settings.AnnounceDiscovery: "false"})

	setting, err := service.Get(ctx, settings.AnnounceDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	if setting.Value || setting.Source != settings.SourceDeployment || setting.Editable {
		t.Fatalf("a pinned setting reads as %+v", setting)
	}
	if setting.EnvVar != "OMNISAVE_DISCOVERY" {
		t.Fatalf("the Dash cannot say where the value came from: %+v", setting)
	}
	if _, err := service.Set(ctx, settings.AnnounceDiscovery, "true"); !errors.Is(err, settings.ErrPinned) {
		t.Fatalf("a pinned setting was editable: %v", err)
	}
}

func TestUnknownSettingsAreRefusedRatherThanInvented(t *testing.T) {
	ctx := context.Background()
	service := settings.New(storagetest.NewMemoryRepository(), nil)
	if _, err := service.Get(ctx, "discovery.broadcast"); !errors.Is(err, settings.ErrNotFound) {
		t.Fatalf("an undeclared setting answered: %v", err)
	}
	if _, err := service.Set(ctx, "discovery.broadcast", "true"); !errors.Is(err, settings.ErrNotFound) {
		t.Fatalf("an undeclared setting was writable: %v", err)
	}
}

func TestPinsAreParsedFromTheEnvironmentOrRefused(t *testing.T) {
	environment := map[string]string{"OMNISAVE_DISCOVERY": "off"}
	pinned, err := settings.ParsePins(func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := pinned[settings.AnnounceDiscovery]; !ok || value != "off" {
		t.Fatalf("OMNISAVE_DISCOVERY=off parsed as %q (present %v)", value, ok)
	}

	environment["OMNISAVE_DISCOVERY"] = "sometimes"
	if _, err := settings.ParsePins(func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	}); err == nil {
		t.Fatal("a value that is not a boolean was accepted")
	}
}
