package main

// The api-docs command builds the GET /api reference as static files, which the
// Pages workflow puts at /api/ next to the landing page:
//
//	<out>/index.md    the Markdown agents read
//	<out>/index.html  the same, rendered for people
//
// The Markdown comes from gateway.Reference, so the site and a running server
// serve one document. The Markdown is rendered with goldmark.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// docsMarkdown renders the reference the way marked did: GitHub-flavoured, so
// the tables become <table>. Only Table is on — marked's linkify and task lists
// were off, and the document has neither bare URLs nor task items.
var docsMarkdown = goldmark.New(goldmark.WithExtensions(extension.Table))

const (
	// docsDefaultOrigin is the address the examples use when none is set: a
	// placeholder the reader swaps for their own server.
	docsDefaultOrigin = "https://neorouter.example.com"
	// docsDefaultOut is where the Pages workflow picks the files up.
	docsDefaultOut = "build/api-docs"
)

// docsBodyMark is where the rendered Markdown goes in docsPage.
const docsBodyMark = "{{body}}"

// docsPage is the page wrapper: doctype, meta, icon, fonts, stylesheet and
// layout. Palette and fonts follow landing/css/styles.css. A Go raw string
// cannot hold a backtick, and the wrapper has none (the Markdown is inserted
// after parsing), so it is verbatim.
const docsPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NeoRouter API</title>
  <meta name="description" content="Reference for the NeoRouter gateway API. Agents: read index.md.">
  <link rel="alternate" type="text/markdown" href="index.md">
  <link rel="icon" type="image/png" href="../assets/favicon.png">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,560&family=IBM+Plex+Mono:wght@450;500&family=Inter:wght@440;600&display=swap" rel="stylesheet">
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4f1e8; --surface: #fdfcf8; --ink: #1c2117; --ink-2: #49503f; --ink-3: #7e8470;
      --line: rgba(28, 33, 23, 0.12); --gold: #b07d2b; --gold-muted: rgba(176, 125, 43, 0.14);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0e1511; --surface: #171f1a; --ink: #ecefe5; --ink-2: #aeb7a6; --ink-3: #7e8877;
        --line: rgba(224, 240, 224, 0.12); --gold: #e1b052; --gold-muted: rgba(225, 176, 82, 0.14);
      }
    }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--ink); font: 16px/1.65 Inter, system-ui, sans-serif; }
    main { max-width: 860px; margin: 0 auto; padding: 48px 16px 96px; }
    .agents { margin: 0 0 32px; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); color: var(--ink-2); font-size: 14px; }
    h1, h2, h3 { font-family: Fraunces, Georgia, serif; font-weight: 560; line-height: 1.2; }
    h1 { font-size: 40px; margin: 0 0 16px; }
    h2 { font-size: 26px; margin: 48px 0 12px; padding-top: 16px; border-top: 1px solid var(--line); }
    h3 { font-size: 19px; margin: 28px 0 8px; }
    a { color: var(--gold); }
    code { font: 0.88em/1.5 "IBM Plex Mono", ui-monospace, monospace; background: var(--gold-muted); padding: 1px 5px; border-radius: 5px; overflow-wrap: anywhere; }
    pre { background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 14px 16px; overflow-x: auto; }
    pre code { background: none; padding: 0; overflow-wrap: normal; }
    .table { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th, td { text-align: left; vertical-align: top; padding: 8px 10px; border-bottom: 1px solid var(--line); }
    th { color: var(--ink-3); font-weight: 600; }
  </style>
</head>
<body>
  <main>
    <p class="agents">Reading this as an agent? Fetch <a href="index.md">index.md</a> for plain Markdown. A running server serves the same reference at <code>GET /api</code>.</p>
    {{body}}
  </main>
</body>
</html>
`

// DocsOrigin is the origin the examples use: the flag, else
// NEOROUTER_DOCS_ORIGIN, else docsDefaultOrigin, without trailing slashes.
func DocsOrigin(flagValue string) string {
	origin := strings.TrimSpace(flagValue)
	if origin == "" {
		origin = strings.TrimSpace(os.Getenv("NEOROUTER_DOCS_ORIGIN"))
	}
	if origin == "" {
		origin = docsDefaultOrigin
	}
	return strings.TrimRight(origin, "/")
}

// RenderDocsPage renders markdown into the static page's wrapper.
//
// goldmark renders the Markdown where marked did before. goldmark puts <tr> and
// </table> on their own lines inside <tbody>. The wrapper, the <div
// class="table"> wrapping of every table and the styles are the same.
func RenderDocsPage(markdown string) (string, error) {
	var rendered strings.Builder
	if err := docsMarkdown.Convert([]byte(markdown), &rendered); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	body := strings.ReplaceAll(rendered.String(), "<table>", `<div class="table"><table>`)
	body = strings.ReplaceAll(body, "</table>", "</table></div>")
	return strings.Replace(docsPage, docsBodyMark, body, 1), nil
}

// WriteAPIDocs writes index.md and index.html into out, replacing whatever was
// there, and returns the two paths.
func WriteAPIDocs(origin, out string) (string, string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		out = docsDefaultOut
	}
	markdown := gateway.Reference(origin, true)
	page, err := RenderDocsPage(markdown)
	if err != nil {
		return "", "", err
	}
	// A stale file from an earlier build would be published next to the new ones.
	if err := os.RemoveAll(out); err != nil {
		return "", "", fmt.Errorf("clear %s: %w", out, err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", "", fmt.Errorf("create %s: %w", out, err)
	}
	markdownPath := filepath.Join(out, "index.md")
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return "", "", err
	}
	pagePath := filepath.Join(out, "index.html")
	if err := os.WriteFile(pagePath, []byte(page), 0o644); err != nil {
		return "", "", err
	}
	return markdownPath, pagePath, nil
}

// runAPIDocs is the api-docs command: the static reference the site publishes
// at /api/.
func runAPIDocs(originFlag, outFlag string) error {
	if strings.TrimSpace(outFlag) == "" {
		outFlag = docsDefaultOut
	}
	origin := DocsOrigin(originFlag)
	if !strings.Contains(origin, "://") {
		return fmt.Errorf("--origin %q is not a URL: want e.g. https://router.example", origin)
	}
	markdownPath, pagePath, err := WriteAPIDocs(origin, outFlag)
	if err != nil {
		return err
	}
	fmt.Printf("API docs written to %s\n", filepath.Dir(markdownPath))
	fmt.Printf("  %s\n  %s\n", markdownPath, pagePath)
	return nil
}
