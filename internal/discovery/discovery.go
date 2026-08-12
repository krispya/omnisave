// Package discovery advertises and finds server addresses over mDNS with DNS-SD.
package discovery

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// ServiceType is the DNS-SD service Omnisave answers to.
	ServiceType = "_omnisave._tcp"
	// Domain is the mDNS domain; the local link and nothing beyond it.
	Domain = "local."
)

// Announcement is what a server puts on the wire.
type Announcement struct {
	// Name is what a person reads in a list of servers. Two Omnisave servers
	// on one network are told apart by this, so it is worth choosing.
	Name string
	Port int
	// TLS says which scheme reaches the server, so a client can build a URL
	// without guessing at it.
	TLS bool
}

// Server is one server a client found: a name for the person choosing between
// two of them, and the URL to try.
type Server struct {
	Name string
	URL  string
}

// txt renders the announcement's key-value records. It carries what a client
// needs to build a URL and what a person needs to recognize the server —
// never a token, a credential, or anything about who may connect.
func (a Announcement) txt() []string {
	return []string{
		"name=" + a.Name,
		"tls=" + strconv.FormatBool(a.TLS),
	}
}

// parseTXT reads the records back, tolerating anything it does not know: an
// older client meeting a newer server should still be able to reach it.
func parseTXT(records []string) map[string]string {
	parsed := make(map[string]string, len(records))
	for _, record := range records {
		key, value, found := strings.Cut(record, "=")
		if !found {
			continue
		}
		parsed[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return parsed
}

// serverFrom builds one discovered server from what an announcement resolved
// to. It prefers IPv4 because that is what a home network hands out, and
// falls back to the announced host name when no address came with the answer.
func serverFrom(instance, hostName string, port int, addresses []net.IP, records []string) (Server, bool) {
	if port <= 0 {
		return Server{}, false
	}
	fields := parseTXT(records)
	host := ""
	for _, address := range addresses {
		if address.To4() != nil {
			host = address.String()
			break
		}
	}
	if host == "" {
		for _, address := range addresses {
			// A link-local IPv6 address needs a zone to be dialable, which is
			// more than a URL can carry; skip it rather than hand back a URL
			// that fails at connect time.
			if address.To4() == nil && !address.IsLinkLocalUnicast() {
				host = address.String()
				break
			}
		}
	}
	if host == "" {
		host = strings.TrimSuffix(hostName, ".")
	}
	if host == "" {
		return Server{}, false
	}

	scheme := "http"
	if secure, found := fields["tls"]; found {
		if value, err := strconv.ParseBool(secure); err == nil && value {
			scheme = "https"
		}
	}
	name := fields["name"]
	if name == "" {
		name = instance
	}
	return Server{
		Name: name,
		URL:  (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}).String(),
	}, true
}
