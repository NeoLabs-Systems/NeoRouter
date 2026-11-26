package updates

import (
	"context"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

const (
	// Repo is the GitHub repository releases are published in.
	Repo = "NeoLabs-Systems/NeoRouter"
	// RuntimeManifestPrefix names a release's signed runtime manifest; the
	// signature is the same name plus ".sig".
	RuntimeManifestPrefix = "neorouter-runtime-manifest-"

	checkTimeout = 20 * time.Second
)

// Latest is the version of the newest release of the channel that carries a
// signed runtime manifest. It reads release metadata only.
func Latest(ctx context.Context, client *release.Client, channel string) (string, error) {
	rel, err := client.ResolveRelease(ctx, Repo, channel, RuntimeManifestPrefix)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.Tag, "v"), nil
}

func defaultReleaseClient(version string) *release.Client {
	return &release.Client{HTTP: ssrf.NewClient(ssrf.Options{Timeout: checkTimeout}), UserAgent: "NeoRouter " + version}
}
