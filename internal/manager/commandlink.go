package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// The `neorouter` command on PATH for an install that is not a package: a
// launcher in ~/.local/bin, which needs no sudo and is on the PATH of most
// Linux shells (lib/cli/command_link.js). Only a launcher NeoRouter wrote is
// ever replaced or removed: a `neorouter` somebody put there themselves is left
// alone.

// CommandMarker is the line that marks a launcher as NeoRouter's own.
const CommandMarker = "# Written by `neorouter install`; `neorouter uninstall` removes it."

// CommandLink states, in the order ensureCommandLink can return them.
const (
	LinkCreated     = "created"
	LinkUpdated     = "updated"
	LinkUnchanged   = "unchanged"
	LinkForeign     = "foreign"
	LinkUnsupported = "unsupported"
)

// CommandLink is the result of ensuring the launcher.
type CommandLink struct {
	Status string
	Path   string
	// Hint is the line that adds ~/.local/bin to the PATH, when it is not there.
	Hint string
}

// BinDir is where the launcher goes.
func BinDir(home string) string { return filepath.Join(home, ".local", "bin") }

// launcherScript is the launcher: it runs the binary this install runs, so an
// update that moves the runtime is followed by re-running `neorouter install`.
func launcherScript(binary string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		CommandMarker,
		"exec " + shellQuote(binary) + ` "$@"`,
		"",
	}, "\n")
}

// shellQuote quotes a path for /bin/sh.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// ensureCommandLink writes or refreshes the launcher for this binary.
func ensureCommandLink(userHome, binary string, env map[string]string) (CommandLink, error) {
	if Platform() == "windows" {
		return CommandLink{Status: LinkUnsupported}, nil
	}
	dir := BinDir(userHome)
	file := filepath.Join(dir, AppCommand)
	script := launcherScript(binary)
	status := LinkCreated
	if raw, err := os.ReadFile(file); err == nil || isSymlink(file) {
		if err == nil && !strings.Contains(string(raw), CommandMarker) {
			return CommandLink{Status: LinkForeign, Path: file}, nil
		}
		if err != nil {
			return CommandLink{Status: LinkForeign, Path: file}, nil
		}
		status = LinkUnchanged
		if string(raw) != script {
			status = LinkUpdated
		}
	}
	if status != LinkUnchanged {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return CommandLink{}, fmt.Errorf("manager: create %s: %w", dir, err)
		}
		if err := envfile.WriteAtomic(file, []byte(script), 0o755); err != nil {
			return CommandLink{}, fmt.Errorf("manager: write %s: %w", file, err)
		}
		if err := os.Chmod(file, 0o755); err != nil {
			return CommandLink{}, fmt.Errorf("manager: make %s executable: %w", file, err)
		}
	}
	link := CommandLink{Status: status, Path: file}
	if !commandReachable(env["PATH"]) {
		link.Hint = pathHint(dir, env["SHELL"], userHome)
	}
	return link, nil
}

// RemoveCommandLink deletes the launcher when it is NeoRouter's, and reports
// whether it removed one.
func RemoveCommandLink(userHome string) bool {
	file := filepath.Join(BinDir(userHome), AppCommand)
	raw, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(raw), CommandMarker) {
		return false
	}
	return os.Remove(file) == nil
}

// commandReachable reports whether typing `neorouter` in a new terminal finds a
// command.
func commandReachable(pathEnv string) bool {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, AppCommand)); err == nil && !info.IsDir() && executable(info) {
			return true
		}
	}
	return false
}

// pathHint is the one line that adds dir to the PATH for the person's shell.
func pathHint(dir, shell, userHome string) string {
	shown := dir
	if strings.HasPrefix(dir, userHome+string(filepath.Separator)) {
		shown = "~" + dir[len(userHome):]
	}
	name := filepath.Base(shell)
	if name == "fish" {
		return "fish_add_path " + shown
	}
	exported := `export PATH="` + strings.ReplaceAll(shown, "~", "$HOME") + `:$PATH"`
	switch name {
	case "zsh":
		return "echo '" + exported + "' >> ~/.zshrc"
	case "bash":
		if runtime.GOOS == "darwin" {
			return "echo '" + exported + "' >> ~/.bash_profile"
		}
		return "echo '" + exported + "' >> ~/.bashrc"
	}
	return exported
}

// needsCommandLink reports whether this install has to put `neorouter` on the
// PATH itself: a source checkout does, a container needs nothing.
func (m *Manager) needsCommandLink(cfg config.Config) bool {
	if m.inContainer() {
		return false
	}
	return exists(filepath.Join(cfg.AppDir, ".git"))
}

// linkCommand adds the command and turns a failure into a warning: not being
// able to add it never fails an install.
func (m *Manager) linkCommand() *CommandLink {
	env := map[string]string{"PATH": m.Getenv("PATH"), "SHELL": m.Getenv("SHELL")}
	link, err := ensureCommandLink(m.UserHome, m.currentBinary(), env)
	if err != nil {
		m.UI.Warn("Could not add the neorouter command: " + err.Error())
		return nil
	}
	return &link
}

// commandNote is what to tell someone about the command after install or repair
// (or "" when there is nothing to say).
func (m *Manager) commandNote(link *CommandLink) string {
	if link == nil || link.Status == LinkForeign || link.Status == LinkUnsupported {
		return ""
	}
	command := m.UI.Command("neorouter")
	if link.Hint != "" {
		return fmt.Sprintf("To use the %s command, run this once and open a new terminal: %s",
			command, m.UI.Command(link.Hint))
	}
	if link.Status == LinkUnchanged {
		return ""
	}
	return fmt.Sprintf("The %s command works in any new terminal.", command)
}

// isSymlink reports whether path is a symbolic link.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// executable reports whether the file can be run.
func executable(info os.FileInfo) bool {
	return info.Mode().Perm()&0o111 != 0
}
