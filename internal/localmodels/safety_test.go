package localmodels

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeArchive(t *testing.T, link string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped := gzip.NewWriter(file)
	archive := tar.NewWriter(zipped)
	headers := []*tar.Header{
		{Name: "build/lib.so", Typeflag: tar.TypeSymlink, Linkname: link},
		{Name: "build/lib.so.1", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4},
	}
	for _, header := range headers {
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(archive, "code"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchiveLinksStayInside(t *testing.T) {
	target := filepath.Join(t.TempDir(), "build")
	if err := extractTarGz(writeArchive(t, "lib.so.1"), target); err != nil {
		t.Fatalf("a link to a neighbour: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(target, "lib.so")); err != nil || string(content) != "code" {
		t.Fatalf("through the link: %q, %v", content, err)
	}
	for _, link := range []string{"/etc/passwd", "../outside", "sub/../../outside"} {
		if err := extractTarGz(writeArchive(t, link), filepath.Join(t.TempDir(), "build")); err == nil {
			t.Errorf("link %q was accepted", link)
		}
	}
}

// A large download is not bounded by the client's whole-request timeout.
func TestDownloadIgnoresTheClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "weights") }))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "model")
	client := &http.Client{Timeout: time.Nanosecond}
	if err := Download(context.Background(), client, File{URL: server.URL}, dest, DownloadOptions{}); err != nil {
		t.Fatal(err)
	}
}
