package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandLinkPointsAtThisBinaryAndIsIdempotent(t *testing.T) {
	if Platform() == "windows" {
		t.Skip("no launcher on Windows")
	}
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "neorouter")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho ran\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	binDir := BinDir(home)
	env := map[string]string{"PATH": binDir + string(os.PathListSeparator) + "/usr/bin:/bin", "SHELL": "/bin/zsh"}

	first, err := ensureCommandLink(home, binary, env)
	if err != nil {
		t.Fatalf("ensureCommandLink: %v", err)
	}
	if first.Status != LinkCreated {
		t.Errorf("status = %q, want created", first.Status)
	}
	if first.Hint != "" {
		t.Errorf("hint = %q, want none while the directory is on the PATH", first.Hint)
	}
	info, err := os.Stat(first.Path)
	if err != nil {
		t.Fatalf("stat launcher: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("launcher mode = %v, want it executable", info.Mode())
	}
	out, err := exec.Command(first.Path, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run launcher: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "ran" {
		t.Errorf("launcher ran %q, want the binary it points at", out)
	}
	if again, err := ensureCommandLink(home, binary, env); err != nil || again.Status != LinkUnchanged {
		t.Errorf("second ensure = %+v, %v; want unchanged", again, err)
	}
	moved := filepath.Join(t.TempDir(), "neorouter")
	if err := os.WriteFile(moved, []byte("#!/bin/sh\necho moved\n"), 0o755); err != nil {
		t.Fatalf("write moved binary: %v", err)
	}
	updated, err := ensureCommandLink(home, moved, env)
	if err != nil {
		t.Fatalf("ensure after a move: %v", err)
	}
	if updated.Status != LinkUpdated {
		t.Errorf("status after a move = %q, want updated", updated.Status)
	}
	if !commandReachable(env["PATH"]) {
		t.Error("commandReachable = false with the launcher on the PATH")
	}
}

func TestCommandLinkHintsAtTheShellOnlyWhenTheDirectoryIsOffPath(t *testing.T) {
	home := t.TempDir()
	link, err := ensureCommandLink(home, "/opt/neorouter/neorouter", map[string]string{"PATH": "/usr/bin:/bin", "SHELL": "/usr/local/bin/fish"})
	if err != nil {
		t.Fatalf("ensureCommandLink: %v", err)
	}
	if link.Hint != "fish_add_path ~/.local/bin" {
		t.Errorf("hint = %q, want the fish line", link.Hint)
	}
	if got := pathHint(BinDir(home), "/bin/zsh", home); got != `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc` {
		t.Errorf("zsh hint = %q", got)
	}
	profile := "~/.bashrc"
	if runtime.GOOS == "darwin" {
		profile = "~/.bash_profile"
	}
	if got := pathHint(BinDir(home), "/bin/bash", home); got != `echo 'export PATH="$HOME/.local/bin:$PATH"' >> `+profile {
		t.Errorf("bash hint = %q", got)
	}
	if got := pathHint(BinDir(home), "", home); got != `export PATH="$HOME/.local/bin:$PATH"` {
		t.Errorf("plain hint = %q", got)
	}
	if commandReachable("/usr/bin:/bin") {
		t.Error("commandReachable found a command that is not installed")
	}
}

func TestCommandLinkLeavesSomebodyElsesCommandAlone(t *testing.T) {
	if Platform() == "windows" {
		t.Skip("no launcher on Windows")
	}
	home := t.TempDir()
	if err := os.MkdirAll(BinDir(home), 0o700); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	foreign := filepath.Join(BinDir(home), AppCommand)
	if err := os.WriteFile(foreign, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatalf("write foreign command: %v", err)
	}
	link, err := ensureCommandLink(home, "/opt/neorouter/neorouter", map[string]string{"PATH": ""})
	if err != nil {
		t.Fatalf("ensureCommandLink: %v", err)
	}
	if link.Status != LinkForeign {
		t.Errorf("status = %q, want foreign", link.Status)
	}
	if RemoveCommandLink(home) {
		t.Error("RemoveCommandLink removed a command it did not write")
	}
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "#!/bin/sh\necho mine\n" {
		t.Errorf("foreign command = %q, %v", raw, err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatalf("remove foreign command: %v", err)
	}
	if _, err := ensureCommandLink(home, "/opt/neorouter/neorouter", map[string]string{"PATH": ""}); err != nil {
		t.Fatalf("ensureCommandLink after removal: %v", err)
	}
	if !RemoveCommandLink(home) {
		t.Error("RemoveCommandLink did not remove the launcher it wrote")
	}
	if exists(foreign) {
		t.Error("the launcher is still there after RemoveCommandLink")
	}
}

func TestCommandNoteExplainsTheCommandOnlyWhenItIsNewOrOffPath(t *testing.T) {
	s := newTestSetup(t)
	if note := s.m.commandNote(&CommandLink{Status: LinkCreated, Hint: "fish_add_path ~/.local/bin"}); !strings.Contains(note, "fish_add_path ~/.local/bin") {
		t.Errorf("note = %q, want the hint", note)
	}
	if note := s.m.commandNote(&CommandLink{Status: LinkUnchanged}); note != "" {
		t.Errorf("note = %q after an unchanged launcher, want none", note)
	}
	if note := s.m.commandNote(&CommandLink{Status: LinkForeign}); note != "" {
		t.Errorf("note = %q for somebody else's command, want none", note)
	}
	if note := s.m.commandNote(&CommandLink{Status: LinkCreated}); !strings.Contains(note, "The neorouter command works in any new terminal.") {
		t.Errorf("note = %q", note)
	}
}

// The binary install.sh downloads carries the marker text; it is not a launcher.
func TestRemovingTheCommandKeepsTheDownloadedBinary(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(BinDir(home), AppCommand)
	writeFile(t, binary, "\x7fELF..."+CommandMarker+"\n...", 0o755)
	if RemoveCommandLink(home) {
		t.Error("the binary was removed")
	}
	link, err := ensureCommandLink(home, "/opt/neorouter", map[string]string{})
	if err != nil || link.Status != LinkForeign {
		t.Errorf("ensureCommandLink over a binary = %+v, %v", link, err)
	}
	if !exists(binary) {
		t.Error("the binary is gone")
	}
}
