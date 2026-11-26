// Package updates owns NeoRouter's self-update: release channels, the git
// check for a newer version on the channel's branch, the update status file the
// admin UI and the update runner share, and the /api/v1/admin/update endpoints.
//
// It is the Go port of server/services/updates/update_service.js,
// server/utils/update_status.js, runtime/update_check.js,
// runtime/release_channel.js, runtime/container.js and lib/cli/updates.js.
package updates

import (
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

const (
	// DefaultChannel is the channel an install follows until one is configured.
	DefaultChannel = release.Beta
	// ChannelEnvKey is the .env key that stores the configured channel.
	ChannelEnvKey = "NEOROUTER_RELEASE_CHANNEL"
)

// ParseChannel maps a user-supplied channel name to "stable" or "beta"; "" means
// it is not a channel.
func ParseChannel(value string) string { return release.ParseChannel(value) }

// NormalizeChannel is ParseChannel with NeoRouter's beta default.
func NormalizeChannel(value string) string {
	if channel := ParseChannel(value); channel != "" {
		return channel
	}
	return DefaultChannel
}

// ChannelBranch is the git branch a channel follows.
func ChannelBranch(channel string) string {
	if NormalizeChannel(channel) == release.Stable {
		return "main"
	}
	return "beta"
}

// ChannelDistTag is the npm dist-tag a channel follows.
func ChannelDistTag(channel string) string {
	if NormalizeChannel(channel) == release.Stable {
		return "latest"
	}
	return "beta"
}

// ChannelLabel is "Stable" or "Beta".
func ChannelLabel(channel string) string { return release.ChannelLabel(NormalizeChannel(channel)) }

// DescribeChannelPolicy is the human summary of what a channel follows.
func DescribeChannelPolicy(channel string) string {
	normalized := NormalizeChannel(channel)
	branch, npm := "main only", "latest only"
	if normalized == release.Beta {
		branch, npm = "newest of beta or main", "newest of beta or latest"
	}
	return ChannelLabel(normalized) + " (git " + branch + ", npm " + npm + ")"
}

// ChannelVersions are the newest stable and beta versions a checkout knows.
type ChannelVersions struct {
	Stable string
	Beta   string
}

// NPMVersions are the newest beta and latest npm versions a checkout knows.
type NPMVersions struct {
	Beta   string
	Latest string
}

// ChoosePreferredBranchForChannel keeps beta installs on beta until a stable
// release is newer, which is what runtime/release_channel.js decides: falling
// back to main too early would move a beta install to older code.
func ChoosePreferredBranchForChannel(channel string, versions ChannelVersions) string {
	if NormalizeChannel(channel) == release.Stable {
		return "main"
	}
	if versions.Beta != "" && versions.Stable != "" && release.CompareVersions(versions.Stable, versions.Beta) > 0 {
		return "main"
	}
	return "beta"
}

// ChoosePreferredNPMTagForChannel is the npm equivalent.
func ChoosePreferredNPMTagForChannel(channel string, versions NPMVersions) string {
	if NormalizeChannel(channel) == release.Stable {
		return "latest"
	}
	if release.CompareVersions(versions.Beta, versions.Latest) > 0 {
		return "beta"
	}
	return "latest"
}

// ReadChannelFromRaw reads the channel out of .env text.
func ReadChannelFromRaw(raw string) string {
	return NormalizeChannel(ParseEnv(raw)[ChannelEnvKey])
}

// ReadChannelFromEnvFile reads the channel from an install's .env; a missing or
// unreadable file means the default channel.
func ReadChannelFromEnvFile(path string) string {
	values, err := config.EnvFileValues(path)
	if err != nil {
		return DefaultChannel
	}
	return NormalizeChannel(values[ChannelEnvKey])
}

// ReadConfiguredChannel is what the install follows right now: the environment
// wins over the .env file, and beta is the default.
func ReadConfiguredChannel(getenv func(string) string, envFile string) string {
	if getenv != nil {
		if value := getenv(ChannelEnvKey); value != "" {
			return NormalizeChannel(value)
		}
	}
	return ReadChannelFromEnvFile(envFile)
}

// WriteChannelToEnvFile stores the channel in an install's .env, creating the
// file when it does not exist, and returns the normalized channel.
func WriteChannelToEnvFile(channel, envFile string) (string, error) {
	normalized := ParseChannel(channel)
	if normalized == "" {
		return "", &ChannelError{Message: `Release channel must be "stable" or "beta".`}
	}
	if envFile == "" {
		return normalized, nil
	}
	if err := config.UpsertEnvValue(envFile, ChannelEnvKey, normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

// ChannelError is the error runtime/release_channel.js throws for a channel name
// it does not know.
type ChannelError struct{ Message string }

func (e *ChannelError) Error() string { return e.Message }

// ParseEnv parses .env text the way runtime/env.js parseEnv does: KEY=VALUE
// lines, keys trimmed, values verbatim, a later line winning.
func ParseEnv(raw string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		at := strings.Index(line, "=")
		if at < 0 {
			continue
		}
		if key := strings.TrimSpace(line[:at]); key != "" {
			values[key] = line[at+1:]
		}
	}
	return values
}
