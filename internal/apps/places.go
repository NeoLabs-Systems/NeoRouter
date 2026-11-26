package apps

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// loopbackOrigin matches an origin that only this computer can reach.
var loopbackOrigin = regexp.MustCompile(`(?i)^https?://(127\.0\.0\.1|localhost|\[::1\])(:\d+)?$`)

// privateHost matches a host name that is only reachable at home: an IP
// address, a .local or .lan name, or no dot at all.
var privateHost = regexp.MustCompile(`(?i)^(\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\]|[^.]+|.+\.(local|lan|home|internal|localhost))$`)

// Place is one address an app can use, and whether it works from there today.
type Place struct {
	ID        string  `json:"id"`      // local, docker, network or internet
	Address   *string `json:"address"` // ends in /v1; null when unknown
	Reachable bool    `json:"reachable"`
	Fix       *string `json:"fix"`  // open_network or public_url
	Note      *string `json:"note"` // docker_host
}

// HostedAddress is the internet address of a NeoRouter run on its own domain,
// or "". Set by NEOROUTER_PUBLIC_URL, or seen when the dashboard is opened over
// https at a real domain (behind a reverse proxy).
func (s *Service) HostedAddress(origin string) string {
	if publicURL := s.cfg().PublicURL; publicURL != "" {
		return strings.TrimRight(publicURL, "/")
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "https" && !privateHost.MatchString(parsed.Hostname()) {
		return parsed.Scheme + "://" + parsed.Host
	}
	return ""
}

// Places lists the address an app should use by where the app runs, and
// whether it can get through from there today. origin is the address this
// dashboard was opened at.
func (s *Service) Places(origin string) []Place {
	// On its own domain, apps reach it there; this server's loopback, Docker
	// and home network mean nothing to the people connecting their apps.
	if hosted := s.HostedAddress(origin); hosted != "" {
		return []Place{v1Place("internet", hosted)}
	}
	cfg := s.cfg()
	status := s.NetworkStatus()
	open := status.Scope == "network"
	lan := lanAddress()
	// Opened from another device already: that address works for that device.
	browserAddress := ""
	if origin != "" && !loopbackOrigin.MatchString(origin) && origin != cfg.PublicURL {
		browserAddress = origin
	}
	// Docker Desktop (macOS, Windows) passes host.docker.internal to this
	// computer's own loopback.
	desktopDocker := status.Platform == "darwin" || status.Platform == "win32"
	port := strconv.Itoa(cfg.Port)

	docker := Place{ID: "docker", Reachable: open || desktopDocker}
	docker.Address = pointer(v1("http://host.docker.internal:" + port))
	if !docker.Reachable {
		docker.Fix = pointer("open_network")
	}
	if status.Platform == "linux" && !status.Container {
		docker.Note = pointer("docker_host")
	}

	network := Place{ID: "network", Reachable: open}
	if browserAddress != "" {
		network.Address = pointer(v1(browserAddress))
	} else if lan != "" {
		network.Address = pointer(v1("http://" + lan + ":" + port))
	}
	if !open {
		network.Fix = pointer("open_network")
	}

	internet := Place{ID: "internet", Reachable: cfg.PublicURL != ""}
	if cfg.PublicURL != "" {
		internet.Address = pointer(v1(cfg.PublicURL))
	} else {
		internet.Fix = pointer("public_url")
	}

	return []Place{
		v1Place("local", "http://127.0.0.1:"+port),
		docker,
		network,
		internet,
	}
}

// v1Place is a reachable place with no fix and no note.
func v1Place(id, base string) Place {
	return Place{ID: id, Address: pointer(v1(base)), Reachable: true}
}

// v1 is a base URL with /v1 appended, whatever slashes it ends with.
func v1(base string) string {
	return strings.TrimRight(base, "/") + "/v1"
}

func pointer(value string) *string { return &value }
