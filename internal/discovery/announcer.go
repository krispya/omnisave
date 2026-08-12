package discovery

import (
	"sync"

	"github.com/libp2p/zeroconf/v2"
)

// Announcer starts and stops the server's local-network advertisement.
type Announcer struct {
	announcement Announcement

	mu     sync.Mutex
	server *zeroconf.Server
}

func NewAnnouncer(announcement Announcement) *Announcer {
	return &Announcer{announcement: announcement}
}

// Set applies the owner's choice. It is idempotent in both directions, so a
// setting written twice does not restart an announcement that never stopped.
func (a *Announcer) Set(enabled bool) error {
	if enabled {
		return a.Start()
	}
	a.Stop()
	return nil
}

func (a *Announcer) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		return nil
	}
	server, err := zeroconf.Register(
		a.announcement.Name, ServiceType, Domain,
		a.announcement.Port, a.announcement.txt(), nil,
	)
	if err != nil {
		return err
	}
	a.server = server
	return nil
}

func (a *Announcer) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil {
		return
	}
	// Shutdown sends the goodbye packet, so a server that stops announcing
	// also stops being listed by whoever is already browsing.
	a.server.Shutdown()
	a.server = nil
}
