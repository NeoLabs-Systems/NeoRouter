// Package updates owns NeoRouter's self-update as the server sees it: release
// channels, the check for a newer signed release, the status file the admin UI
// and the update runner share, and the /api/v1/admin/update endpoints.
package updates

import (
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

const (
	// DefaultChannel is the channel an install follows until one is configured.
	DefaultChannel = release.Stable
	// ChannelEnvKey is the .env key that stores the configured channel.
	ChannelEnvKey = "NEOROUTER_RELEASE_CHANNEL"
)

// ParseChannel maps a user-supplied channel name to "stable" or "beta"; "" means
// it is not a channel.
func ParseChannel(value string) string { return release.ParseChannel(value) }

// NormalizeChannel is ParseChannel with the default channel.
func NormalizeChannel(value string) string {
	if channel := ParseChannel(value); channel != "" {
		return channel
	}
	return DefaultChannel
}

// ChannelLabel is "Stable" or "Beta".
func ChannelLabel(channel string) string { return release.ChannelLabel(NormalizeChannel(channel)) }

// ChannelBranch is the branch a channel's releases are cut from.
func ChannelBranch(channel string) string {
	if NormalizeChannel(channel) == release.Stable {
		return "main"
	}
	return "beta"
}

// DescribeChannelPolicy is the human summary of what a channel follows.
func DescribeChannelPolicy(channel string) string {
	if NormalizeChannel(channel) == release.Beta {
		return "Beta (newest release, including previews)"
	}
	return "Stable (newest stable release)"
}

// ReadChannelFromEnvFile reads the channel from an install's .env; a missing or
// unreadable file means the default channel.
func ReadChannelFromEnvFile(path string) string {
	values, err := envfile.Load(path)
	if err != nil {
		return DefaultChannel
	}
	return NormalizeChannel(values[ChannelEnvKey])
}

// ReadConfiguredChannel is what the install follows right now: the environment
// wins over the .env file.
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
	if err := envfile.Upsert(envFile, ChannelEnvKey, normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

// ChannelError is the error for a channel name that does not exist.
type ChannelError struct{ Message string }

func (e *ChannelError) Error() string { return e.Message }
