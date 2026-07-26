package discovery

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

// BrowseWindow is how long a client listens before deciding what is out
// there. Long enough for a NAS to answer, short enough that a command with no
// arguments still feels like one step.
const BrowseWindow = 2 * time.Second

// Browse listens for servers announcing themselves and returns what answered,
// sorted by name so two runs in a row offer the same list in the same order.
//
// Nothing here is trusted. Anything on the network can claim to be an Omnisave
// server; what a client gets from this is an address to try, and pairing still
// decides whether it gets any further.
func Browse(ctx context.Context, window time.Duration) ([]Server, error) {
	if window <= 0 {
		window = BrowseWindow
	}
	ctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)
	found := make(chan []Server, 1)
	go func() {
		servers := []Server{}
		seen := make(map[string]bool)
		for entry := range entries {
			server, ok := serverFrom(entry.Instance, entry.HostName, entry.Port,
				append(slices.Clone(entry.AddrIPv4), entry.AddrIPv6...), entry.Text)
			if !ok || seen[server.URL] {
				continue
			}
			seen[server.URL] = true
			servers = append(servers, server)
		}
		slices.SortFunc(servers, func(left, right Server) int {
			if order := strings.Compare(left.Name, right.Name); order != 0 {
				return order
			}
			return strings.Compare(left.URL, right.URL)
		})
		found <- servers
	}()

	// Browse blocks until the window closes, then closes the entry channel.
	// A context that ran out is the ordinary ending here, not a failure.
	if err := zeroconf.Browse(ctx, ServiceType, Domain, entries); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// Browse closes the channel itself only once its loop has started. It
		// failed before that — joining multicast on a bridged container, say —
		// so closing here is what releases the collector.
		close(entries)
		return nil, err
	}
	// Waiting on the collector means every answer that arrived is in the list.
	return <-found, nil
}
