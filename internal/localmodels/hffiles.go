package localmodels

// hffiles.go finds the exact files of a catalog model on Hugging Face — path,
// size and, for large files, the SHA-256 the download is checked against.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// HFBase is the Hugging Face origin.
const HFBase = "https://huggingface.co"

// HFTreeTimeoutMs bounds one file-list request.
const HFTreeTimeoutMs = 30_000

// GlobToRegex turns "onnx/*" or "*.json" into an anchored pattern where * stays
// within one folder.
func GlobToRegex(glob string) *regexp.Regexp {
	parts := strings.Split(glob, "*")
	for index, part := range parts {
		parts[index] = regexp.QuoteMeta(part)
	}
	return regexp.MustCompile("^" + strings.Join(parts, "[^/]*") + "$")
}

// FileResolver lists the files a catalog entry needs; tests replace it with a
// local list.
type FileResolver interface {
	Resolve(ctx context.Context, entry *Entry) ([]File, error)
}

// HFClient lists and resolves Hugging Face repositories.
type HFClient struct {
	// HTTP is the client to use; nil uses a client with no overall timeout,
	// because downloads are long-lived.
	HTTP *http.Client
	// Base is the origin, HFBase by default.
	Base string
}

func (c *HFClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *HFClient) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return HFBase
}

// hfTreeItem is one entry of the repository tree.
type hfTreeItem struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size *int64 `json:"size"`
	LFS  *struct {
		Size *int64 `json:"size"`
		OID  string `json:"oid"`
	} `json:"lfs"`
}

// tree lists the files of a repository, following HTTP errors with the texts
// the dashboard shows.
func (c *HFClient) tree(ctx context.Context, repo string) ([]hfTreeItem, error) {
	url := fmt.Sprintf("%s/api/models/%s/tree/main?recursive=1", c.base(), repo)
	requestCtx, cancel := context.WithTimeout(ctx, HFTreeTimeoutMs*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", downloadUserAgent)
	response, err := c.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Hugging Face answered %d for %s.", response.StatusCode, repo)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	var items []hfTreeItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("Unexpected file list for %s.", repo)
	}
	files := make([]hfTreeItem, 0, len(items))
	for _, item := range items {
		if item.Type == "file" {
			files = append(files, item)
		}
	}
	return files, nil
}

// Resolve lists the entry's files in include order, throwing when a pattern
// matches nothing.
func (c *HFClient) Resolve(ctx context.Context, entry *Entry) ([]File, error) {
	files, err := c.tree(ctx, entry.Source.Repo)
	if err != nil {
		return nil, err
	}
	wanted := append([]string{}, entry.Source.Include...)
	if entry.Source.Projector != "" {
		wanted = append(wanted, entry.Source.Projector)
	}
	chosen := []hfTreeItem{}
	seen := map[string]bool{}
	for _, pattern := range wanted {
		matcher := GlobToRegex(pattern)
		matched := false
		for _, file := range files {
			if !matcher.MatchString(file.Path) {
				continue
			}
			matched = true
			if seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			chosen = append(chosen, file)
		}
		if !matched {
			return nil, fmt.Errorf("%s has no file matching %s.", entry.Source.Repo, pattern)
		}
	}
	out := make([]File, 0, len(chosen))
	for _, file := range chosen {
		segments := strings.Split(file.Path, "/")
		for index, segment := range segments {
			segments[index] = encodeURIComponent(segment)
		}
		out = append(out, File{
			Path:   file.Path,
			Size:   hfSize(file),
			SHA256: hfOID(file),
			URL:    fmt.Sprintf("%s/%s/resolve/main/%s", c.base(), entry.Source.Repo, strings.Join(segments, "/")),
		})
	}
	return out, nil
}

// hfSize is `lfs.size ?? size ?? null`.
func hfSize(item hfTreeItem) *int64 {
	if item.LFS != nil && item.LFS.Size != nil {
		return item.LFS.Size
	}
	return item.Size
}

// hfOID is `lfs.oid || null`.
func hfOID(item hfTreeItem) string {
	if item.LFS == nil {
		return ""
	}
	return item.LFS.OID
}

// encodeURIComponent percent-encodes, leaving !'()*-._~ and the alphanumerics
// alone.
func encodeURIComponent(value string) string {
	var out strings.Builder
	for index := 0; index < len(value); index++ {
		b := value[index]
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
			out.WriteByte(b)
		case strings.IndexByte("-_.!~*'()", b) >= 0:
			out.WriteByte(b)
		default:
			out.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return out.String()
}
