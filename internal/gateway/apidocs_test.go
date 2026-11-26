package gateway

// Reference is pinned byte for byte by the golden files in testdata. Update them
// together with the document.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func goldenReference(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(raw)
}

// firstDifference describes where got departs from want, for a readable failure.
func firstDifference(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for index := 0; index < len(gotLines) && index < len(wantLines); index++ {
		if gotLines[index] != wantLines[index] {
			return "line " + strconv.Itoa(index+1) + ":\n got: " + gotLines[index] + "\nwant: " + wantLines[index]
		}
	}
	return "the documents differ in length: got " + strconv.Itoa(len(got)) + " bytes, want " + strconv.Itoa(len(want))
}

func TestReferenceMatchesTheGoldenFiles(t *testing.T) {
	cases := []struct {
		name        string
		origin      string
		placeholder bool
		golden      string
	}{
		{"placeholder", "https://neorouter.example.com", true, "api_docs_placeholder.golden"},
		{"live", "https://neorouter.example.com", false, "api_docs_live.golden"},
		{"placeholder-http", "http://10.0.0.5:8787", true, "api_docs_placeholder_http.golden"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Reference(testCase.origin, testCase.placeholder)
			want := goldenReference(t, testCase.golden)
			if got != want {
				t.Fatalf("Reference(%q, %v) differs from the golden file: %s", testCase.origin, testCase.placeholder, firstDifference(got, want))
			}
		})
	}
}

// apiDocs is exactly the reference without the placeholder line.
func TestAPIDocsIsTheLiveReference(t *testing.T) {
	f := newFixture(t, nil)
	service := f.service
	if got, want := service.apiDocs("https://neorouter.example.com"), goldenReference(t, "api_docs_live.golden"); got != want {
		t.Fatalf("apiDocs differs from the live golden file: %s", firstDifference(got, want))
	}
}

func TestReferencePlaceholderLine(t *testing.T) {
	const origin = "https://router.example"
	line := "- Examples use `" + origin + "`. Replace it with the address of your NeoRouter server.\n"
	withPlaceholder := Reference(origin, true)
	if !strings.Contains(withPlaceholder, line) {
		t.Fatalf("the placeholder line is missing")
	}
	place := strings.Index(withPlaceholder, line)
	thisDocument := strings.Index(withPlaceholder, "- This document: `GET "+origin+"/api/docs` (Markdown)\n")
	if thisDocument < 0 || place != thisDocument+len("- This document: `GET "+origin+"/api/docs` (Markdown)\n") {
		t.Fatalf("the placeholder line does not follow the This document line")
	}
	if strings.Count(withPlaceholder, line) != 1 {
		t.Fatalf("the placeholder line appears %d times", strings.Count(withPlaceholder, line))
	}
	live := Reference(origin, false)
	if strings.Contains(live, "Examples use") {
		t.Fatal("the live document carries the placeholder line")
	}
	if strings.Count(withPlaceholder, "\n") != strings.Count(live, "\n")+1 {
		t.Fatalf("the placeholder document is not the live one plus one line")
	}
}
