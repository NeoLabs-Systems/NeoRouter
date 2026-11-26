package main

// The api-docs command's contract with the Pages workflow: index.md is the
// gateway reference itself, index.html is that Markdown in the site's wrapper.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
)

func TestDocsOriginPrefersFlagThenEnvThenDefault(t *testing.T) {
	t.Setenv("NEOROUTER_DOCS_ORIGIN", "")
	if got := DocsOrigin(""); got != docsDefaultOrigin {
		t.Errorf("unset origin = %q", got)
	}
	if got := DocsOrigin("https://router.example/"); got != "https://router.example" {
		t.Errorf("trailing slashes are not removed: %q", got)
	}
	t.Setenv("NEOROUTER_DOCS_ORIGIN", "https://from-env.example//")
	if got := DocsOrigin(""); got != "https://from-env.example" {
		t.Errorf("environment origin = %q", got)
	}
	if got := DocsOrigin("https://from-flag.example"); got != "https://from-flag.example" {
		t.Errorf("the flag does not win: %q", got)
	}
}

func TestAPIDocsWritesMarkdownAndPage(t *testing.T) {
	out := t.TempDir() + "/api-docs"
	// A stale file from an earlier build must not survive into the artifact.
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	const origin = "https://router.example"
	markdownPath, pagePath, err := WriteAPIDocs(origin, out)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if markdownPath != filepath.Join(out, "index.md") || pagePath != filepath.Join(out, "index.html") {
		t.Fatalf("paths = %q, %q", markdownPath, pagePath)
	}
	if _, err := os.Stat(filepath.Join(out, "stale.html")); !os.IsNotExist(err) {
		t.Errorf("the old file is still there: %v", err)
	}

	raw, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if want := gateway.Reference(origin, true); string(raw) != want {
		t.Errorf("index.md is not Reference(%q, true)", origin)
	}
	if !strings.Contains(string(raw), "Examples use `"+origin+"`. Replace it with the address of your NeoRouter server.") {
		t.Error("index.md has no placeholder line")
	}

	page, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(page)

	// The page is the API reference rendered, not the wrapper alone.
	quickStart := strings.Index(html, "<h2>Quick start</h2>")
	if quickStart < 0 {
		t.Fatalf("the quick start is not rendered")
	}
	if auth := strings.Index(html, "<h2>Authentication</h2>"); auth < quickStart {
		t.Errorf("the sections are out of order")
	}
	for _, want := range []string{
		"<h1>NeoRouter API</h1>",
		"<h2>Quick start</h2>",
		`<pre><code class="language-bash">curl ` + origin + `/v1/chat/completions `,
		`<code>&quot;model&quot;: &quot;auto&quot;</code>`,
		"<code>nrt_…</code>",
		"<h3>POST /v1/chat/completions</h3>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page is missing %q", want)
		}
	}
	// Tables are wrapped so they scroll on a phone, as the JavaScript build did.
	if !strings.Contains(html, `<div class="table"><table>`) || !strings.Contains(html, "</table></div>") {
		t.Errorf("tables are not wrapped")
	}
	// The wrapper itself: head, fonts, styles and the agent hint.
	for _, want := range []string{
		"<!doctype html>\n<html lang=\"en\">",
		`<link rel="alternate" type="text/markdown" href="index.md">`,
		`<link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,560&family=IBM+Plex+Mono:wght@450;500&family=Inter:wght@440;600&display=swap" rel="stylesheet">`,
		"--gold: #b07d2b;",
		`<p class="agents">Reading this as an agent? Fetch <a href="index.md">index.md</a> for plain Markdown.`,
		"</main>\n</body>\n</html>\n",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the wrapper is missing %q", want)
		}
	}
	if strings.Contains(html, docsBodyMark) {
		t.Error("the body marker was not replaced")
	}
}

func TestAPIDocsCommandIsWired(t *testing.T) {
	out := t.TempDir() + "/docs"
	if err := run(context.Background(), []string{"api-docs", "--origin", "https://cli.example/", "--out", out}); err != nil {
		t.Fatalf("run api-docs: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if want := gateway.Reference("https://cli.example", true); string(raw) != want {
		t.Error("the command did not write the reference for --origin")
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("no index.html: %v", err)
	}
}

func TestAPIDocsRejectsAnOriginThatIsNotAURL(t *testing.T) {
	out := t.TempDir()
	if err := run(context.Background(), []string{"api-docs", "--origin", "router.example", "--out", out}); err == nil {
		t.Fatal("a bare host name was accepted")
	}
	if _, err := os.Stat(filepath.Join(out, "index.md")); !os.IsNotExist(err) {
		t.Error("a rejected run still wrote files")
	}
}
