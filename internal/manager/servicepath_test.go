package manager

// A service starts with the bare PATH of launchd or systemd, so the binary's
// own directory (and the usual install locations) are appended to what is
// there.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServicePathAppendsTheBinaryDirectoryAfterWhatWasThere(t *testing.T) {
	got := ServicePath("/usr/bin:/bin", "/home/me/.nvm/versions/node/v22/bin/neorouter")
	dirs := filepath.SplitList(got)
	if dirs[0] != "/usr/bin" || dirs[1] != "/bin" {
		t.Fatalf("the PATH was reordered: %v", dirs)
	}
	if !contains(dirs, "/home/me/.nvm/versions/node/v22/bin") {
		t.Errorf("the binary directory is missing: %v", dirs)
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			t.Errorf("%s appears twice in %v", dir, dirs)
		}
		seen[dir] = true
	}
}

func TestServicePathKeepsAMissingPathWorking(t *testing.T) {
	dirs := filepath.SplitList(ServicePath("", "/opt/neorouter/bin/neorouter"))
	if !contains(dirs, "/opt/neorouter/bin") {
		t.Errorf("an empty PATH lost the binary directory: %v", dirs)
	}
	for _, want := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		if !contains(dirs, want) {
			t.Errorf("%s is missing from %v", want, dirs)
		}
	}
}

func TestServicePathDoesNotDuplicateTheBinaryDirectory(t *testing.T) {
	path := strings.Join([]string{"/opt/neorouter/bin", "/usr/bin"}, string(os.PathListSeparator))
	dirs := filepath.SplitList(ServicePath(path, "/opt/neorouter/bin/neorouter"))
	count := 0
	for _, dir := range dirs {
		if dir == "/opt/neorouter/bin" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the binary directory appears %d times in %v", count, dirs)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
