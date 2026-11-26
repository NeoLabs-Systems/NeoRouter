package updates

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

// ResolvePreferredBranch is runtime/update_check.js resolvePreferredBranch: the
// branch a channel follows, read from the refs the last fetch brought so a
// server never waits on the network here.
//
// Stable falls back to beta only while no stable branch exists, so nobody is
// moved to older code by accident.
func ResolvePreferredBranch(ctx context.Context, channel string, run GitRunner, onFallback func(string)) (string, error) {
	normalized := NormalizeChannel(channel)
	hasMain := hasBranch(ctx, run, "main")
	hasBeta := hasBranch(ctx, run, "beta")
	if normalized == release.Stable {
		if hasMain {
			return "main", nil
		}
		if hasBeta {
			if onFallback != nil {
				onFallback("Stable channel selected, but origin/main is missing; using origin/beta until a stable branch exists.")
			}
			return "beta", nil
		}
		return "", errors.New("updates: resolve branch: no origin/main or origin/beta branch was found for updates")
	}
	preferred := ChoosePreferredBranchForChannel(normalized, ChannelVersions{
		Stable: latestStableTagVersion(ctx, run),
		Beta:   latestBetaTagVersion(ctx, run),
	})
	if preferred == "beta" {
		if hasBeta {
			return "beta", nil
		}
		if hasMain {
			return "main", nil
		}
	} else if hasMain {
		return "main", nil
	} else if hasBeta {
		return "beta", nil
	}
	return "", fmt.Errorf("updates: resolve branch: release channel branch %q was not found on origin", preferred)
}

// AvailableUpdate is a newer version waiting on the channel's branch.
type AvailableUpdate struct {
	Version *string  `json:"version"`
	Branch  string   `json:"branch"`
	Changes []string `json:"changes"`
}

// CheckResult is what checkForUpdate reports: whether the check produced an
// answer, and which update is waiting. A failed or unreachable check is not an
// answer.
type CheckResult struct {
	Checked bool             `json:"checked"`
	Update  *AvailableUpdate `json:"update"`
	Reason  string           `json:"reason,omitempty"`
	Branch  string           `json:"branch,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// CheckOptions configure one check.
type CheckOptions struct {
	// Channel is the release channel to follow.
	Channel string
	// FetchRemote fetches the remote; nil fetches with git.
	FetchRemote func(ctx context.Context) bool
	// OnFallback receives the note about a channel falling back to another
	// branch.
	OnFallback func(message string)
}

// CheckForUpdate is runtime/update_check.js checkForUpdate.
func CheckForUpdate(ctx context.Context, run GitRunner, opts CheckOptions) CheckResult {
	git := NewGit(run)
	fetch := opts.FetchRemote
	if fetch == nil {
		fetch = git.FetchRemote
	}
	if !fetch(ctx) {
		return CheckResult{Reason: "offline"}
	}
	branch, err := ResolvePreferredBranch(ctx, opts.Channel, run, opts.OnFallback)
	if err != nil {
		return CheckResult{Reason: "failed", Error: err.Error()}
	}
	pending, ok := git.PendingUpdate(ctx, branch, false)
	if !ok {
		return CheckResult{Checked: true, Branch: branch}
	}
	return CheckResult{
		Checked: true,
		Branch:  branch,
		Update: &AvailableUpdate{
			Version: pending.Version,
			Branch:  branch,
			Changes: git.ChangesBetween(ctx, git.Head(ctx).SHA, pending.RemoteRef),
		},
	}
}
