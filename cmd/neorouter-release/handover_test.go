package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestNodeUpdaterHandsOver replays what the Node.js updater does with this
// tree: npm install, run by the CLI or the admin UI's update runner. The npm
// step must fail (so the updater rolls back), keep node_modules, and run
// install.sh on the right channel once the updater is gone.
func TestNodeUpdaterHandsOver(t *testing.T) {
	node, errNode := exec.LookPath("node")
	_, errNpm := exec.LookPath("npm")
	if errNode != nil || errNpm != nil || runtime.GOOS == "windows" {
		t.Skip("node, npm and a POSIX shell are needed to replay the old updater")
	}
	script := func(rel ...string) func(tree, home string) []string {
		return func(tree, _ string) []string {
			return []string{node, filepath.Join(append([]string{tree}, rel...)...), tree}
		}
	}
	t.Run("command line", func(t *testing.T) {
		replayHandover(t, "", "beta", 0, script("bin", "neorouter.js"))
	})
	t.Run("admin UI", func(t *testing.T) {
		replayHandover(t, "NEOROUTER_RELEASE_CHANNEL=stable\n", "stable", 0, script("lib", "cli", "update_runner.js"))
	})
	t.Run("npm link, and a switch that fails", func(t *testing.T) {
		replayHandover(t, "NEOROUTER_RELEASE_CHANNEL=beta\n", "beta", 1, func(tree, home string) []string {
			link := filepath.Join(home, "global", "bin", "neorouter")
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(tree, "bin", "neorouter.js"), link); err != nil {
				t.Fatal(err)
			}
			return []string{node, link, "update", tree}
		})
	})
}

func replayHandover(t *testing.T, env, channel string, installStatus int, invoke func(tree, home string) []string) {
	t.Helper()
	tree, home := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		raw, _ := os.ReadFile(path)
		return string(raw)
	}
	for _, rel := range []string{"package.json", ".npmrc", "scripts/node-handover.sh"} {
		write(filepath.Join(tree, rel), read(filepath.Join("..", "..", rel)))
	}
	write(filepath.Join(tree, "node_modules", "kept", "package.json"), `{"name":"kept","version":"1.0.0"}`)
	fakeUpdater := `
const r = require('child_process').spawnSync('npm', ['install', '--omit=dev', '--no-audit', '--no-fund'], { cwd: process.argv[process.argv.length - 1], encoding: 'utf8' });
process.stdout.write(String(r.status) + '\n' + r.stderr);
`
	write(filepath.Join(tree, "bin", "neorouter.js"), "#!/usr/bin/env node\n"+fakeUpdater)
	write(filepath.Join(tree, "lib", "cli", "update_runner.js"), fakeUpdater)

	// install.sh replaces the commands the way the shared installer does.
	launcher := filepath.Join(home, ".local", "bin", "neorouter")
	write(launcher, "node launcher")
	link := filepath.Join(home, "global", "bin", "neorouter")
	ran := filepath.Join(home, "installed")
	write(filepath.Join(tree, "install.sh"), "#!/bin/bash\n"+
		"echo go-binary > '"+launcher+"'\n"+
		"rm -f '"+link+"'\n"+
		"echo \"$NEOROUTER_RELEASE_CHANNEL${NEOROUTER_APP_DIR:+ app-dir}\" > '"+ran+".tmp'\n"+
		"mv '"+ran+".tmp' '"+ran+"'\n"+
		"exit "+string(rune('0'+installStatus))+"\n")
	if env != "" {
		write(filepath.Join(home, ".neorouter", ".env"), env)
	}

	environment := append(os.Environ(), "HOME="+home, "NEOROUTER_HOME="+filepath.Join(home, ".neorouter"),
		"NEOROUTER_APP_DIR="+tree, "NEOROUTER_BIN_DIR=", "npm_config_cache="+filepath.Join(home, ".npm"),
		"PATH="+filepath.Dir(link)+string(os.PathListSeparator)+os.Getenv("PATH"))

	direct := exec.Command("npm", "install", "--no-audit", "--no-fund")
	direct.Dir, direct.Env = tree, environment
	if out, err := direct.CombinedOutput(); err == nil || !strings.Contains(string(out), "no longer uses npm") {
		t.Fatalf("direct npm install: %v\n%s", err, out)
	}

	args := invoke(tree, home)
	_, linked := os.Lstat(link)
	updater := exec.Command(args[0], args[1:]...)
	updater.Env = environment
	out, err := updater.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "1\n") || !strings.Contains(string(out), "switch then finishes by itself") {
		t.Fatalf("updater run: %v\n%s", err, out)
	}
	if read(filepath.Join(tree, "node_modules", "kept", "package.json")) == "" {
		t.Fatal("npm pruned the old node_modules")
	}

	log := filepath.Join(home, ".neorouter", "logs", "handover.log")
	done := func() bool {
		if _, err := os.Stat(ran); err != nil {
			return false
		}
		return installStatus == 0 || strings.Contains(read(log), "keeps running on Node.js")
	}
	for deadline := time.Now().Add(time.Minute); !done(); time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("install.sh did not finish after the updater exited; log: %s", read(log))
		}
	}
	if got := strings.TrimSpace(read(ran)); got != channel {
		t.Errorf("install.sh ran with %q, want channel %q and no app folder", got, channel)
	}
	if !strings.Contains(read(log), "handing over to the Go binary") {
		t.Errorf("log = %q", read(log))
	}
	if installStatus != 0 {
		if got := read(launcher); got != "node launcher" {
			t.Errorf("launcher after a failed switch = %q", got)
		}
		if linked == nil {
			if target, err := os.Readlink(link); err != nil || target != filepath.Join(tree, "bin", "neorouter.js") {
				t.Errorf("npm link after a failed switch = %q, %v", target, err)
			}
		}
	}
}

// A global npm install updates with `npm install -g` from the repository. The
// installed Node.js package must survive it, and the handover still run.
func TestNodeGlobalInstallHandsOver(t *testing.T) {
	for _, tool := range []string{"node", "npm", "git", "ps"} {
		if _, err := exec.LookPath(tool); err != nil || runtime.GOOS == "windows" {
			t.Skip("node, npm, git and a POSIX shell are needed to replay the old updater")
		}
	}
	root := t.TempDir()
	home, prefix, repo, old := filepath.Join(root, "home"), filepath.Join(root, "prefix"), filepath.Join(root, "repo"), filepath.Join(root, "old")
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"package.json", ".npmrc", "scripts/node-handover.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(repo, rel), string(raw))
	}
	ran := filepath.Join(home, "installed")
	write(filepath.Join(repo, "install.sh"), "#!/bin/bash\necho \"$NEOROUTER_RELEASE_CHANNEL\" > '"+ran+".tmp'\nmv '"+ran+".tmp' '"+ran+"'\n")
	write(filepath.Join(old, "package.json"), `{"name":"neorouter","version":"0.2.2-beta.1","bin":{"neorouter":"bin/neorouter.js"}}`)
	write(filepath.Join(old, "server", "index.js"), "// node server\n")
	write(filepath.Join(old, "bin", "neorouter.js"), `#!/usr/bin/env node
const r = require('child_process').spawnSync('npm', ['install', '-g', process.env.NEW_SPEC, '--force'], { encoding: 'utf8' });
process.stdout.write(String(r.status) + '\n' + r.stderr);
`)
	write(filepath.Join(root, "npmrc"), "prefix="+prefix+"\n")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "HOME="+home, "NEOROUTER_HOME="+filepath.Join(home, ".neorouter"),
		"NPM_CONFIG_USERCONFIG="+filepath.Join(root, "npmrc"), "npm_config_cache="+filepath.Join(root, "cache"),
		"NEW_SPEC=git+file://"+repo, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
	run := func(dir, name string, args ...string) string {
		command := exec.Command(name, args...)
		command.Dir, command.Env = dir, environment
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run(repo, "git", "init", "-q", "-b", "beta")
	run(repo, "git", "add", "-A")
	run(repo, "git", "commit", "-q", "-m", "go")
	run(old, "npm", "pack", "--pack-destination", root)
	run(root, "npm", "install", "-g", filepath.Join(root, "neorouter-0.2.2-beta.1.tgz"))

	out := run(root, node(t), filepath.Join(prefix, "bin", "neorouter"), "update")
	if strings.HasPrefix(out, "0\n") {
		t.Fatalf("the update did not fail:\n%s", out)
	}
	installed := filepath.Join(prefix, "lib", "node_modules", "neorouter", "server", "index.js")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("the Node.js install did not survive: %v\n%s", err, out)
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(200 * time.Millisecond) {
		if raw, err := os.ReadFile(ran); err == nil {
			if got := strings.TrimSpace(string(raw)); got != "beta" {
				t.Errorf("channel = %q", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("install.sh never ran:\n%s", out)
		}
	}
}

func node(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
