package processing

import (
	"net/http"
	"strings"
	"testing"
)

func TestBodiesAreLimited(t *testing.T) {
	server, _ := testServer(t, "processing.view", "processing.edit")
	body := `{"messages":[{"role":"user","content":"` + strings.Repeat("a", 3<<20) + `"}]}`
	status, answer := doJSON(t, http.MethodPost, server.URL+"/api/v1/processing/preview", body)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("preview: %d %v", status, answer)
	}
}

func TestModelDownloadsHaveNoTotalTimeout(t *testing.T) {
	if timeout := privacyDownloadClient().Timeout; timeout != 0 {
		t.Fatalf("timeout = %v", timeout)
	}
}
