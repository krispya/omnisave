// Package settings manages runtime owner settings and deployment overrides.
package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// AnnounceDiscovery controls whether the server announces itself locally.
const (
	AnnounceDiscovery = "discovery.announce"
	IGDBClientID      = "igdb.client_id"
	IGDBClientSecret  = "igdb.client_secret"
)

var (
	ErrNotFound = errors.New("settings: unknown setting")
	ErrPinned   = errors.New("settings: pinned by the deployment")
)

// Source identifies where the effective value came from.
type Source string

const (
	SourceDefault    Source = "default"
	SourceOwner      Source = "owner"
	SourceDeployment Source = "deployment"
)

// Kind is what sort of answer a setting takes (ADR-011). A secret is the one
// that changes how it is handled everywhere: it is written and never read
// back, so nothing that serializes a Setting can leak it by omission.
type Kind string

const (
	KindToggle Kind = "toggle"
	KindText   Kind = "text"
	KindSecret Kind = "secret"
)

// Definition declares one owner setting.
type Definition struct {
	Key     string
	EnvVar  string
	Group   string
	Summary string
	Kind    Kind
	Default bool
}

var definitions = []Definition{{
	Key:     AnnounceDiscovery,
	EnvVar:  "OMNISAVE_DISCOVERY",
	Group:   "network",
	Summary: "Announce this server on the local network",
	Kind:    KindToggle,
	Default: true,
}, {
	Key:     IGDBClientID,
	EnvVar:  "OMNISAVE_IGDB_CLIENT_ID",
	Group:   "igdb",
	Summary: "IGDB client ID",
	Kind:    KindText,
}, {
	Key:     IGDBClientSecret,
	EnvVar:  "OMNISAVE_IGDB_CLIENT_SECRET",
	Group:   "igdb",
	Summary: "IGDB client secret",
	Kind:    KindSecret,
}}

// Setting is a public setting view. Secret values are omitted; Configured reports presence.
type Setting struct {
	Key        string `json:"key"`
	Group      string `json:"group"`
	Summary    string `json:"summary"`
	Kind       Kind   `json:"kind"`
	Value      bool   `json:"value"`
	Text       string `json:"text"`
	Configured bool   `json:"configured"`
	Source     Source `json:"source"`
	Editable   bool   `json:"editable"`
	EnvVar     string `json:"env_var"`
}

// Service reads and writes owner settings.
type Service interface {
	List(ctx context.Context) ([]Setting, error)
	Get(ctx context.Context, key string) (*Setting, error)
	// Enabled is Get for callers that only act on the value, and that would
	// rather fall back to the declared default than fail.
	Enabled(ctx context.Context, key string) bool
	// Value returns what a text or secret setting holds, for the server
	// itself. It is the only way to read a secret, and it never leaves the
	// process that asked.
	Value(ctx context.Context, key string) string
	// Set stores an owner's choice, as text the setting's kind knows how to
	// read. A setting the deployment has pinned is not editable and answers
	// ErrPinned.
	Set(ctx context.Context, key, value string) (*Setting, error)
	// OnChange registers a listener run after a setting changes, which is how
	// "applies immediately" is kept true.
	OnChange(listener func(Setting))
}

type service struct {
	repository storage.SettingsRepository
	pinned     map[string]string
	now        func() time.Time

	mu        sync.Mutex
	listeners []func(Setting)
}

// New creates the settings service. Pinned values come from the environment,
// read once at startup as ADR-003 requires, and cannot be edited at runtime.
func New(repository storage.SettingsRepository, pinned map[string]string) Service {
	return &service{repository: repository, pinned: pinned, now: time.Now}
}

// ParsePins validates environment values and returns deployment-pinned settings.
func ParsePins(lookup func(string) (string, bool)) (map[string]string, error) {
	pinned := make(map[string]string)
	for _, definition := range definitions {
		raw, present := lookup(definition.EnvVar)
		if !present || strings.TrimSpace(raw) == "" {
			continue
		}
		if definition.Kind == KindToggle {
			if _, err := parseBool(raw); err != nil {
				return nil, fmt.Errorf("%s must be true or false", definition.EnvVar)
			}
		}
		pinned[definition.Key] = strings.TrimSpace(raw)
	}
	return pinned, nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "yes", "enabled":
		return true, nil
	case "off", "no", "disabled":
		return false, nil
	}
	return strconv.ParseBool(strings.TrimSpace(raw))
}

func (s *service) List(ctx context.Context) ([]Setting, error) {
	settings := make([]Setting, 0, len(definitions))
	for _, definition := range definitions {
		setting, err := s.resolve(ctx, definition)
		if err != nil {
			return nil, err
		}
		settings = append(settings, *setting)
	}
	return settings, nil
}

func (s *service) Get(ctx context.Context, key string) (*Setting, error) {
	definition, found := find(key)
	if !found {
		return nil, ErrNotFound
	}
	return s.resolve(ctx, definition)
}

func (s *service) Enabled(ctx context.Context, key string) bool {
	setting, err := s.Get(ctx, key)
	if err != nil {
		definition, found := find(key)
		return found && definition.Default
	}
	return setting.Value
}

func (s *service) Value(ctx context.Context, key string) string {
	definition, found := find(key)
	if !found {
		return ""
	}
	if pinned, ok := s.pinned[key]; ok {
		return pinned
	}
	stored, err := s.repository.GetOwnerSetting(ctx, definition.Key)
	if err != nil {
		return ""
	}
	return stored
}

func (s *service) Set(ctx context.Context, key, value string) (*Setting, error) {
	definition, found := find(key)
	if !found {
		return nil, ErrNotFound
	}
	if _, pinned := s.pinned[key]; pinned {
		return nil, ErrPinned
	}
	value = strings.TrimSpace(value)
	if definition.Kind == KindToggle {
		parsed, err := parseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s is on or off", ErrNotFound, definition.Key)
		}
		value = strconv.FormatBool(parsed)
	}
	if err := s.repository.SetOwnerSetting(ctx, key, value, s.now()); err != nil {
		return nil, err
	}
	setting, err := s.resolve(ctx, definition)
	if err != nil {
		return nil, err
	}
	s.notify(*setting)
	return setting, nil
}

func (s *service) OnChange(listener func(Setting)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *service) notify(setting Setting) {
	s.mu.Lock()
	listeners := slices.Clone(s.listeners)
	s.mu.Unlock()
	for _, listener := range listeners {
		listener(setting)
	}
}

func (s *service) resolve(ctx context.Context, definition Definition) (*Setting, error) {
	setting := Setting{
		Key:      definition.Key,
		Group:    definition.Group,
		Summary:  definition.Summary,
		Kind:     definition.Kind,
		Value:    definition.Default,
		Source:   SourceDefault,
		Editable: true,
		EnvVar:   definition.EnvVar,
	}
	if pinned, ok := s.pinned[definition.Key]; ok {
		apply(&setting, definition, pinned)
		setting.Source = SourceDeployment
		setting.Editable = false
		return &setting, nil
	}

	stored, err := s.repository.GetOwnerSetting(ctx, definition.Key)
	if errors.Is(err, storage.ErrNotFound) {
		return &setting, nil
	}
	if err != nil {
		return nil, err
	}
	if definition.Kind == KindToggle {
		if _, parseErr := parseBool(stored); parseErr != nil {
			// A value the server cannot read is not a reason to refuse to
			// start or to answer; the declared default is the honest fallback.
			return &setting, nil
		}
	}
	apply(&setting, definition, stored)
	setting.Source = SourceOwner
	return &setting, nil
}

// apply creates a public setting view without exposing stored secrets.
func apply(setting *Setting, definition Definition, stored string) {
	switch definition.Kind {
	case KindToggle:
		value, err := parseBool(stored)
		setting.Value = err == nil && value
		setting.Configured = true
	case KindSecret:
		setting.Configured = stored != ""
	default:
		setting.Text = stored
		setting.Configured = stored != ""
	}
}

func find(key string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Key == key {
			return definition, true
		}
	}
	return Definition{}, false
}
