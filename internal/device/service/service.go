// Package service implements Device registration and playing presence.
package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/device"
)

type service struct {
	repository device.Repository
	presence   *devicePresence

	mu        sync.Mutex
	onExpired []func()
}

// New creates a Device service over repository. Presence starts empty: a
// restarted server knows no one is playing until Devices re-affirm.
func New(repository device.Repository) device.Service {
	s := &service{repository: repository}
	s.presence = newDevicePresence(s.announceExpiry)
	return s
}

func (s *service) Register(ctx context.Context, id string, input device.Registration) (*device.Device, error) {
	id = strings.TrimSpace(id)
	name := strings.TrimSpace(input.Name)
	platform := strings.TrimSpace(input.Platform)
	if id == "" || len(id) > 128 || name == "" || len(name) > 128 || len(platform) > 64 {
		return nil, device.ErrInvalid
	}
	now := time.Now().UTC()
	registered := device.Device{ID: id, Name: name, Platform: platform, CreatedAt: now, LastSeenAt: now}
	if err := s.repository.UpsertDevice(ctx, registered); err != nil {
		return nil, err
	}
	return &registered, nil
}

func (s *service) Get(ctx context.Context, id string) (*device.Device, error) {
	return s.repository.GetDevice(ctx, id)
}

// ReportPlaying accepts presence only from a registered Device, so the live
// picture never names a Device the server cannot describe.
func (s *service) ReportPlaying(ctx context.Context, id string, gameIDs []string) (bool, error) {
	if _, err := s.repository.GetDevice(ctx, id); err != nil {
		return false, err
	}
	return s.presence.report(id, gameIDs), nil
}

func (s *service) Presence() []device.Presence {
	live := s.presence.live()
	reports := make([]device.Presence, len(live))
	for index, status := range live {
		reports[index] = device.Presence{DeviceID: status.deviceID, PlayingGameIDs: status.playing}
	}
	return reports
}

func (s *service) Playing(deviceID, gameID string) bool {
	_, playing := s.presence.playing(deviceID, gameID)
	return playing
}

func (s *service) OnPresenceExpired(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExpired = append(s.onExpired, fn)
}

func (s *service) announceExpiry() {
	s.mu.Lock()
	listeners := append([]func(){}, s.onExpired...)
	s.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
}

var _ device.Service = (*service)(nil)
