package updates

import (
	"os"
	"regexp"
	"strings"
)

// containerCgroup matches the cgroup paths a container runtime leaves on PID 1.
var containerCgroup = regexp.MustCompile(`(?i)docker|podman|kubepods|lxc`)

// DetectContainer reports whether this process runs inside a container (Docker,
// Podman, Kubernetes), which is runtime/container.js: there is no checkout to
// pull and no user service to restart, because the image is the deployment unit.
func DetectContainer(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
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
	raw, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	return containerCgroup.Match(raw)
}
