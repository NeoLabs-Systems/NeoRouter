package apps

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// localHosts are the addresses that only this computer can reach.
var localHosts = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}

// restartDelay is how long the reply has to reach the browser before the
// process stops and the service manager starts it again.
const restartDelay = 800 * time.Millisecond

// privateAddress matches the addresses a home or office network hands out.
var privateAddress = [...]string{"10.", "192.168.", "172.16.", "172.17.", "172.18.", "172.19.",
	"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.", "172.26.", "172.27.",
	"172.28.", "172.29.", "172.30.", "172.31."}

// virtualInterface matches the interfaces Docker and VPNs add; they are offered
// after real network cards.
func virtualInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "utun", "tun", "vmnet", "bridge"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// NetworkStatus is network.status(): who can reach this NeoRouter.
type NetworkStatus struct {
	Scope      string `json:"scope"` // local or network
	Port       int    `json:"port"`
	Platform   string `json:"platform"`
	Container  bool   `json:"container"`
	CanRestart bool   `json:"canRestart"`
}

// ScopeResult is a network change: the new status and whether a restart follows.
type ScopeResult struct {
	NetworkStatus
	Restarting    bool `json:"restarting"`
	RestartNeeded bool `json:"restartNeeded"`
}

// platform is process.platform, which the dashboard compares with darwin and
// win32 (Docker Desktop passes host.docker.internal on those).
func platform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

// detectContainer reports whether this process runs inside a container (Docker,
// Podman, Kubernetes).
func detectContainer(getenv func(string) string) bool {
	switch strings.ToLower(getenv("NEOROUTER_CONTAINER")) {
	case "1", "true", "yes", "on":
		return true
	}
	if getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	if value := getenv("container"); value != "" {
		return value != "host"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	file, err := os.Open("/proc/1/cgroup")
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		for _, marker := range []string{"docker", "podman", "kubepods", "lxc"} {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

// listensBeyondLocal reports whether NeoRouter listens on more than loopback.
func (s *Service) listensBeyondLocal() bool {
	return detectContainer(s.getenv) || !localHosts[s.cfg().Host]
}

// canRestartItself reports whether launchd or systemd brings NeoRouter back
// after it stops.
func (s *Service) canRestartItself() bool {
	if detectContainer(s.getenv) {
		return false
	}
	return s.getenv("INVOCATION_ID") != "" || s.getenv("XPC_SERVICE_NAME") == config.ServiceLabel
}

// lanAddress is this computer's address in the local network, or "".
func lanAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	type found struct {
		name    string
		address string
	}
	var candidates []found
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			text := ip.String()
			if !isPrivateAddress(text) {
				continue
			}
			candidates = append(candidates, found{name: iface.Name, address: text})
		}
	}
	// Real network cards before Docker's and VPNs' virtual ones.
	sort.SliceStable(candidates, func(i, j int) bool {
		return !virtualInterface(candidates[i].name) && virtualInterface(candidates[j].name)
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].address
}

func isPrivateAddress(address string) bool {
	for _, prefix := range privateAddress {
		if strings.HasPrefix(address, prefix) {
			return true
		}
	}
	return false
}

// NetworkStatus reports who can reach NeoRouter right now.
func (s *Service) NetworkStatus() NetworkStatus {
	scope := "local"
	if s.listensBeyondLocal() {
		scope = "network"
	}
	return NetworkStatus{
		Scope:      scope,
		Port:       s.cfg().Port,
		Platform:   platform(),
		Container:  detectContainer(s.getenv),
		CanRestart: s.canRestartItself(),
	}
}

// SetScope writes NEOROUTER_HOST to the install's .env and, when a service
// manager runs NeoRouter, restarts it to apply the change. Inside a container
// the image decides; nothing is written.
func (s *Service) SetScope(scope string) (ScopeResult, error) {
	if detectContainer(s.getenv) {
		return ScopeResult{NetworkStatus: s.NetworkStatus()}, nil
	}
	host := "127.0.0.1"
	if scope == "network" {
		host = "0.0.0.0"
	}
	if err := config.UpsertEnvValue(s.cfg().Paths.EnvFile, "NEOROUTER_HOST", host); err != nil {
		return ScopeResult{}, err
	}
	s.log.Info("Network access changed", "scope", scope)
	if !s.canRestartItself() {
		result := ScopeResult{NetworkStatus: s.NetworkStatus()}
		result.RestartNeeded = true
		return result, nil
	}
	// After the answer is sent: stop, and the service manager starts it again.
	restart := s.restart
	time.AfterFunc(restartDelay, restart)
	result := ScopeResult{NetworkStatus: s.NetworkStatus()}
	result.Restarting = true
	return result, nil
}
