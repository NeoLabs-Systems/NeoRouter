package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

// fetchTimeout bounds one `git fetch`; a server never waits longer than this.
const fetchTimeout = 8 * time.Second

// maxChanges is how many commit subjects an update reports.
const maxChanges = 8

// housekeeping matches commits a person would not call a change: release
// bookkeeping and merges.
var housekeeping = regexp.MustCompile(`(?i)^(chore|ci|build|test)(\(.+\))?:|\[skip ci\]|^Merge `)

// Result is one finished git command.
type Result struct {
	Status int
	Stdout string
	Stderr string
}

// GitRunner runs one git subcommand and reports its exit status and output.
// Every git call the update check makes goes through it, so a test can answer
// with a scripted repository instead of a checkout.
type GitRunner func(ctx context.Context, args []string) Result

// ExecRunner runs git in dir.
func ExecRunner(dir string) GitRunner {
	return func(ctx context.Context, args []string) Result {
		cmd := exec.CommandContext(ctx, "git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return result
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.Status = exit.ExitCode()
		} else {
			result.Status = -1
		}
		return result
	}
}

// Head is a checkout position.
type Head struct {
	SHA    string
	Branch string
}

// Pending is the update waiting on the channel's branch.
type Pending struct {
	Branch    string
	RemoteRef string
	Version   *string
}

// Git is the repository behind an update: which version the channel's branch
// carries, and what changed. It is lib/cli/updates.js createUpdateGit.
type Git struct {
	Run GitRunner
}

// NewGit builds the git facts over run.
func NewGit(run GitRunner) *Git { return &Git{Run: run} }

// runGit returns the trimmed stdout of one command; false when git failed.
func (g *Git) runGit(ctx context.Context, args ...string) (string, bool) {
	if g.Run == nil {
		return "", false
	}
	result := g.Run(ctx, args)
	if result.Status != 0 {
		return "", false
	}
	return strings.TrimSpace(result.Stdout), true
}

// FetchRemote fetches tags and branches; release tags are forced because they
// can move between builds.
func (g *Git) FetchRemote(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	_, ok := g.runGit(ctx, "fetch", "origin", "--tags", "--force", "--quiet")
	return ok
}

// Head reads the current commit and branch.
func (g *Git) Head(ctx context.Context) Head {
	sha, _ := g.runGit(ctx, "rev-parse", "HEAD")
	branch, _ := g.runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	return Head{SHA: sha, Branch: branch}
}

// VersionAt reads the version a ref carries in its package.json; nil when the
// ref has none.
func (g *Git) VersionAt(ctx context.Context, ref string) *string {
	raw, ok := g.runGit(ctx, "show", ref+":package.json")
	if !ok {
		return nil
	}
	version, ok := packageVersion(raw)
	if !ok {
		return nil
	}
	return &version
}

// PendingUpdate is what the last fetch brought for branch. An older version
// only counts when the person asked to switch channel explicitly.
func (g *Git) PendingUpdate(ctx context.Context, branch string, allowOlder bool) (Pending, bool) {
	remoteRef := "origin/" + branch
	current := g.Head(ctx)
	ahead := 0
	if raw, ok := g.runGit(ctx, "rev-list", "--count", "HEAD.."+remoteRef); ok {
		ahead = atoiOrZero(raw)
	}
	if ahead == 0 && current.Branch == branch {
		return Pending{}, false
	}
	version := g.VersionAt(ctx, remoteRef)
	if !allowOlder && CompareVersionsPtr(version, g.VersionAt(ctx, "HEAD")) < 0 {
		return Pending{}, false
	}
	return Pending{Branch: branch, RemoteRef: remoteRef, Version: version}, true
}

// ChangesBetween lists the commit subjects between two refs, without the
// bookkeeping ones.
func (g *Git) ChangesBetween(ctx context.Context, from, to string) []string {
	changes := []string{}
	log, ok := g.runGit(ctx, "log", "--no-merges", "--format=%s", from+".."+to)
	if !ok || log == "" {
		return changes
	}
	for _, subject := range strings.Split(log, "\n") {
		subject = strings.TrimSpace(subject)
		if subject == "" || housekeeping.MatchString(subject) {
			continue
		}
		changes = append(changes, subject)
		if len(changes) == maxChanges {
			break
		}
	}
	return changes
}

// Restore puts the checkout back exactly where it was before an update began. A
// pull that stopped on a conflict leaves a rebase or merge half done, and
// neither checkout nor reset clears that.
func (g *Git) Restore(ctx context.Context, at Head) bool {
	_, _ = g.runGit(ctx, "rebase", "--abort")
	_, _ = g.runGit(ctx, "merge", "--abort")
	if at.Branch != "" && at.Branch != "HEAD" {
		if _, ok := g.runGit(ctx, "checkout", "--force", at.Branch); !ok {
			return false
		}
	}
	_, ok := g.runGit(ctx, "reset", "--hard", at.SHA)
	return ok
}

// CompareVersionsPtr orders two optional versions; a missing one sorts first,
// like runtime/release_channel.js compareVersions(null, ...).
func CompareVersionsPtr(a, b *string) int {
	var left, right string
	if a != nil {
		left = *a
	}
	if b != nil {
		right = *b
	}
	return release.CompareVersions(left, right)
}

// packageVersion reads the version field of package.json text, which is where an
// install of either era records the release it is running.
func packageVersion(raw string) (string, bool) {
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil || manifest.Version == "" {
		return "", false
	}
	return manifest.Version, true
}

// hasBranch reports whether the last fetch brought origin/<branch>.
func hasBranch(ctx context.Context, run GitRunner, branch string) bool {
	_, ok := NewGit(run).runGit(ctx, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return ok
}

// listTagVersions lists the tag versions matching pattern, newest first.
func listTagVersions(ctx context.Context, run GitRunner, pattern string) []string {
	raw, ok := NewGit(run).runGit(ctx, "tag", "--list", pattern, "--sort=-v:refname")
	if !ok {
		return nil
	}
	var versions []string
	for _, value := range strings.Split(raw, "\n") {
		version := strings.TrimPrefix(strings.TrimSpace(value), "v")
		if version != "" {
			versions = append(versions, version)
		}
	}
	return versions
}

var (
	stableTagVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	betaTagVersion   = regexp.MustCompile(`^\d+\.\d+\.\d+-beta\.\d+$`)
)

// latestStableTagVersion is the newest vX.Y.Z tag, or "".
func latestStableTagVersion(ctx context.Context, run GitRunner) string {
	for _, version := range listTagVersions(ctx, run, `v[0-9]*.[0-9]*.[0-9]*`) {
		if stableTagVersion.MatchString(version) {
			return version
		}
	}
	return ""
}

// latestBetaTagVersion is the newest vX.Y.Z-beta.N tag, or "".
func latestBetaTagVersion(ctx context.Context, run GitRunner) string {
	for _, version := range listTagVersions(ctx, run, `v[0-9]*.[0-9]*.[0-9]*-beta.*`) {
		if betaTagVersion.MatchString(version) {
			return version
		}
	}
	return ""
}

func atoiOrZero(raw string) int {
	n := 0
	for _, r := range strings.TrimSpace(raw) {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
