// Command neorouter-release is the release tooling CI runs: resolving and
// bumping the version in internal/config/config.go and package.json, building the
// signed runtime archives an install updates itself with, rendering the
// download section of a GitHub release's notes, and writing the static API
// reference the site publishes at /api/.
//
//	go run ./cmd/neorouter-release <command> [flags]
//
// Pattern: refs/NeoRecall/cmd/neorecall-release.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/manager"
)

const (
	configFile = "internal/config/config.go"
	notesStart = "<!-- neorouter-download:start -->"
	notesEnd   = "<!-- neorouter-download:end -->"
)

var (
	versionPattern     = regexp.MustCompile(`^\d+\.\d+\.\d+(-beta\.\d+)?$`)
	goVersionLine      = regexp.MustCompile(`(Version\s*=\s*)"[^"]*"`)
	packageVersionLine = regexp.MustCompile(`("version":\s*)"[^"]*"`)
	stableTagPattern   = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	betaTagPattern     = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-beta\.(\d+)$`)
)

// platformSlot is one download a person can pick on the release page.
type platformSlot struct {
	id, label, detail, badgeMessage, badgeLogo, readyColor string
	assetName                                              string
}

// platformSlots are the CLI downloads the build workflow attaches.
var platformSlots = []platformSlot{
	{"linux-x64", "Linux", "x64", "x64", "linux", "f0a126", "neorouter-cli-linux-x64"},
	{"linux-arm64", "Linux", "arm64", "arm64", "linux", "f0a126", "neorouter-cli-linux-arm64"},
	{"macos-x64", "macOS", "Intel", "x64", "apple", "1c2117", "neorouter-cli-macos-x64"},
	{"macos-arm64", "macOS", "Apple silicon", "arm64", "apple", "1c2117", "neorouter-cli-macos-arm64"},
	{"windows-x64", "Windows", "x64", "x64", "windows", "0078d4", "neorouter-cli-windows-x64.exe"},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "neorouter-release:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: neorouter-release <version|resolve-version|set-version|release-download|runtime-bundles|runtime-keygen|api-docs> [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	channel := flags.String("channel", "beta", "")
	ghOut := flags.Bool("github-output", false, "")
	tag := flags.String("tag", "", "")
	repo := flags.String("repo", "", "")
	dryRun := flags.Bool("dry-run", false, "")
	output := flags.String("output", "dist", "")
	version := flags.String("version", "", "")
	keyFile := flags.String("key-file", "", "")
	origin := flags.String("origin", "", "")
	out := flags.String("out", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	switch args[0] {
	case "version":
		current, err := ReadVersion(".")
		if err != nil {
			return err
		}
		fmt.Println(current)
	case "resolve-version":
		next, err := ResolveNextVersion(ctx, ".", *channel)
		if err != nil {
			return err
		}
		if *ghOut {
			if outPath := os.Getenv("GITHUB_OUTPUT"); outPath != "" {
				file, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					return err
				}
				_, werr := fmt.Fprintf(file, "version=%s\nrelease_tag=v%s\n", next, next)
				closeErr := file.Close()
				if werr != nil {
					return werr
				}
				if closeErr != nil {
					return closeErr
				}
			}
		}
		fmt.Println(next)
	case "set-version":
		if flags.NArg() != 1 {
			return errors.New("usage: neoreouter-release set-version VERSION")
		}
		return SetVersion(".", flags.Arg(0))
	case "runtime-bundles":
		if strings.TrimSpace(*version) == "" {
			return errors.New("--version is required")
		}
		return BuildRuntimeBundles(".", *output, *version, os.Getenv("NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY"))
	case "runtime-keygen":
		return WriteKeypair(*output)
	case "release-download":
		if strings.TrimSpace(*tag) == "" {
			return errors.New("--tag is required")
		}
		repository := firstNonEmpty(*repo, os.Getenv("GITHUB_REPOSITORY"), manager.Repo)
		changed, body, err := UpdateDownloadNotes(ctx, *tag, repository, *dryRun)
		if err != nil {
			return err
		}
		if *dryRun {
			fmt.Print(body)
		} else if !changed {
			fmt.Println("Release notes already current.")
		}
		_ = keyFile
	case "api-docs":
		return runAPIDocs(*origin, *out)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

// ReadVersion reads config.Version out of internal/config/config.go under root.
func ReadVersion(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, configFile))
	if err != nil {
		return "", err
	}
	match := goVersionLine.FindSubmatch(raw)
	if len(match) < 2 {
		return "", fmt.Errorf("version constant not found in %s", configFile)
	}
	return strings.TrimSpace(string(regexp.MustCompile(`"([^"]*)"`).FindSubmatch(match[0])[1])), nil
}

// SetVersion validates version and writes it into internal/config/config.go and
// package.json, the two files the release workflow commits.
func SetVersion(root, version string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH-beta.N", version)
	}
	pkgPath := filepath.Join(root, "package.json")
	if pkg, err := os.ReadFile(pkgPath); err == nil {
		if !packageVersionLine.Match(pkg) {
			return fmt.Errorf("%s has no version field", pkgPath)
		}
		if err := os.WriteFile(pkgPath, packageVersionLine.ReplaceAll(pkg, []byte(`${1}"`+version+`"`)), 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	path := filepath.Join(root, configFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !goVersionLine.Match(raw) {
		return fmt.Errorf("version constant not found in %s", path)
	}
	return os.WriteFile(path, goVersionLine.ReplaceAll(raw, []byte(`${1}"`+version+`"`)), 0o644)
}

type semTriplet struct{ major, minor, patch int }

func parseTriplet(value string) (semTriplet, bool) {
	base := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(value), "v"), "-", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return semTriplet{}, false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	patch, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return semTriplet{}, false
	}
	return semTriplet{major, minor, patch}, true
}

func maxTriplet(a, b semTriplet) semTriplet {
	switch {
	case a.major != b.major:
		if a.major > b.major {
			return a
		}
		return b
	case a.minor != b.minor:
		if a.minor > b.minor {
			return a
		}
		return b
	case a.patch >= b.patch:
		return a
	}
	return b
}

// ResolveNextVersion computes the next release version from config.Version and
// the git tags of root.
func ResolveNextVersion(ctx context.Context, root, channel string) (string, error) {
	fileVersion, err := ReadVersion(root)
	if err != nil {
		fileVersion = "0.1.0"
	}
	cmd := exec.CommandContext(ctx, "git", "tag", "--list", "v[0-9]*.[0-9]*.[0-9]*")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list git tags: %w", err)
	}
	return NextVersionForTags(fileVersion, strings.Fields(string(out)), channel)
}

// NextVersionForTags is the version a channel releases next: a patch above the
// newest stable tag on stable, and a new beta counter on beta.
func NextVersionForTags(fileVersion string, tags []string, channel string) (string, error) {
	fileBase, ok := parseTriplet(fileVersion)
	if !ok {
		fileBase = semTriplet{0, 1, 0}
	}
	var latestStable semTriplet
	hasStable := false
	for _, tag := range tags {
		if match := stableTagPattern.FindStringSubmatch(tag); len(match) != 4 {
			continue
		}
		triplet, ok := parseTriplet(tag)
		if !ok {
			continue
		}
		if !hasStable {
			latestStable, hasStable = triplet, true
			continue
		}
		latestStable = maxTriplet(latestStable, triplet)
	}
	stableNext := semTriplet{0, 0, 1}
	if hasStable {
		stableNext = semTriplet{latestStable.major, latestStable.minor, latestStable.patch + 1}
	}
	base := maxTriplet(fileBase, stableNext)
	baseText := fmt.Sprintf("%d.%d.%d", base.major, base.minor, base.patch)
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "stable":
		return baseText, nil
	case "beta":
	default:
		return "", fmt.Errorf("unknown channel %q: want beta or stable", channel)
	}
	var counters []int
	for _, tag := range tags {
		match := betaTagPattern.FindStringSubmatch(tag)
		if len(match) != 5 {
			continue
		}
		if fmt.Sprintf("%s.%s.%s", match[1], match[2], match[3]) != baseText {
			continue
		}
		if counter, err := strconv.Atoi(match[4]); err == nil {
			counters = append(counters, counter)
		}
	}
	nextBeta := 0
	if len(counters) > 0 {
		sort.Ints(counters)
		nextBeta = counters[len(counters)-1] + 1
	}
	return fmt.Sprintf("%s-beta.%d", baseText, nextBeta), nil
}

// ReleaseAsset is one file of a GitHub release.
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// encodeURIComponent is JavaScript's encodeURIComponent, which the shields.io
// badge URLs were built with.
func encodeURIComponent(value string) string {
	return strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*", "%7E", "~").
		Replace(url.QueryEscape(value))
}

// badge is one shields.io badge URL.
func badge(label, message, color, logo string) string {
	return "https://img.shields.io/badge/" + encodeURIComponent(label) + "-" + encodeURIComponent(message) + "-" + color +
		"?style=for-the-badge&logo=" + encodeURIComponent(logo) + "&logoColor=white"
}

// DownloadNotes renders the download section of a release's notes.
func DownloadNotes(tag, repository string, assets []ReleaseAsset) string {
	if repository = strings.TrimSpace(repository); repository == "" {
		repository = manager.Repo
	}
	ready := map[string]string{}
	for _, asset := range assets {
		for _, slot := range platformSlots {
			if asset.Name != slot.assetName {
				continue
			}
			link := asset.URL
			if link == "" {
				link = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
					repository, encodeURIComponent(tag), encodeURIComponent(asset.Name))
			}
			ready[slot.id] = link
			break
		}
	}
	releaseURL := fmt.Sprintf("https://github.com/%s/releases/tag/%s", repository, encodeURIComponent(tag))
	badges := make([]string, 0, len(platformSlots))
	for _, slot := range platformSlots {
		href, color, message := releaseURL, "6b7280", "building"
		if link, ok := ready[slot.id]; ok {
			href, color, message = link, slot.readyColor, slot.badgeMessage
		}
		badges = append(badges, fmt.Sprintf(`<a href="%s"><img alt="%s %s" src="%s"></a>`,
			href, slot.label, slot.detail, badge(slot.label, message, color, slot.badgeLogo)))
	}
	status := "Every download for this release is ready."
	switch {
	case len(ready) == 0:
		status = "The downloads are building now. Each one appears here as soon as its build finishes."
	case len(ready) < len(platformSlots):
		status = "Ready downloads link straight to the file. The rest attach as soon as their builds finish."
	}
	return strings.Join([]string{
		notesStart, "", `<p align="center">`, "  <strong>Download NeoRouter " + tag + "</strong>", "</p>", "",
		`<p align="center">`, "  " + strings.Join(badges, "\n  "), "</p>", "",
		`<p align="center"><sub>` + status + `</sub></p>`, "", "---", "", notesEnd,
	}, "\n")
}

// MergeDownloadNotes replaces the download section of body, or prepends it.
func MergeDownloadNotes(body, section string) string {
	current := strings.TrimPrefix(body, "\uFEFF")
	section = strings.TrimSpace(section)
	if section == "" {
		return current
	}
	start, end := strings.Index(current, notesStart), strings.Index(current, notesEnd)
	if start >= 0 && end > start {
		before := strings.TrimRight(current[:start], " \t\r\n")
		after := strings.TrimLeft(current[end+len(notesEnd):], " \t\r\n")
		var parts []string
		for _, part := range []string{before, section, after} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n")) + "\n"
	}
	if strings.TrimSpace(current) == "" {
		return section + "\n"
	}
	return section + "\n\n" + strings.TrimLeft(current, " \t\r\n")
}

// UpdateDownloadNotes updates the download section of a draft or published
// release and reports whether it changed.
func UpdateDownloadNotes(ctx context.Context, tag, repository string, dryRun bool) (bool, string, error) {
	// `gh release view` works on drafts too, unlike the tags endpoint.
	out, err := exec.CommandContext(ctx, "gh", "release", "view", tag, "--repo", repository, "--json", "body,assets").Output()
	if err != nil {
		return false, "", fmt.Errorf("gh release view: %w", err)
	}
	var release struct {
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return false, "", err
	}
	assets := make([]ReleaseAsset, 0, len(release.Assets))
	for _, asset := range release.Assets {
		assets = append(assets, ReleaseAsset{Name: asset.Name, URL: asset.URL})
	}
	next := MergeDownloadNotes(release.Body, DownloadNotes(tag, repository, assets))
	if next == strings.TrimSpace(strings.TrimPrefix(release.Body, "\uFEFF"))+"\n" {
		return false, next, nil
	}
	if dryRun {
		return true, next, nil
	}
	notes := filepath.Join(os.TempDir(), "neorouter-release-notes-"+strconv.Itoa(os.Getpid())+".md")
	if err := os.WriteFile(notes, []byte(next), 0o600); err != nil {
		return false, "", err
	}
	defer os.Remove(notes)
	cmd := exec.CommandContext(ctx, "gh", "release", "edit", tag, "--repo", repository, "--notes-file", notes)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return false, next, fmt.Errorf("gh release edit: %w", err)
	}
	fmt.Println("Release notes updated.")
	return true, next, nil
}

// firstNonEmpty returns the first value that is not empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
