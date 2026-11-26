package manager

// The CLI as people use it: `Run` with the arguments a shell would pass, over a
// temporary install. Tests of the command line end to end.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
)

// runCLI runs one command the way cmd/neorouter does, over the test install.
func runCLI(t *testing.T, s *testSetup, args ...string) (string, error) {
	t.Helper()
	s.out.Reset()
	s.m.Out, s.m.Err = s.out, s.out
	s.m.UI.Out, s.m.UI.Err = s.out, s.out
	err := s.m.Run(s.ctx, args)
	return s.output(), err
}

func TestCLIHelpGroupsEverydayCommands(t *testing.T) {
	s := newTestSetup(t)
	out, err := runCLI(t, s, "help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"Here's what you can do", "When something's wrong", "neorouter doctor", "neorouter help --all"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q:\n%s", want, out)
		}
	}
	out, err = runCLI(t, s, "help", "--all")
	if err != nil {
		t.Fatalf("help --all: %v", err)
	}
	if !strings.Contains(out, "neorouter reset-password") {
		t.Errorf("help --all is missing the reference commands:\n%s", out)
	}
}

func TestCLIOutputForScriptsStaysBare(t *testing.T) {
	s := newTestSetup(t)
	out, err := runCLI(t, s, "env", "get", "NEOROUTER_HOST")
	if err != nil {
		t.Fatalf("env get: %v", err)
	}
	if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != 1 || lines[0] != "127.0.0.1" {
		t.Errorf("env get printed %q, want one bare line", out)
	}
	out, err = runCLI(t, s, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != 1 {
		t.Errorf("version printed %q, want one line", out)
	}
}

func TestCLIMistypedCommandFails(t *testing.T) {
	s := newTestSetup(t)
	_, err := runCLI(t, s, "nope")
	if err == nil {
		t.Fatal("a mistyped command did not fail")
	}
	if cli.CodeOf(err, "") != "CLI_INPUT" || !strings.Contains(err.Error(), "There's no neorouter nope command.") {
		t.Errorf("error = %v (%s)", err, cli.CodeOf(err, ""))
	}
}

func TestCLIStatusAndDoctorRunWithoutChangingAnything(t *testing.T) {
	s := newTestSetup(t)
	out, err := runCLI(t, s, "status", "--details")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"NeoRouter isn't running", "Details", "Release channel"} {
		if !strings.Contains(out, want) {
			t.Errorf("status is missing %q:\n%s", want, out)
		}
	}
	out, err = runCLI(t, s, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	for _, want := range []string{"Checking NeoRouter", "Nothing is changed while this runs.", "Settings", "neorouter setup"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor is missing %q:\n%s", want, out)
		}
	}
	if _, err := runCLI(t, s, "logs"); err != nil {
		t.Fatalf("logs: %v", err)
	}
}

func TestCLIBackupSavesASnapshot(t *testing.T) {
	s := newTestSetup(t)
	if _, err := s.m.migrateDatabase(s.ctx, s.config(t)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	out, err := runCLI(t, s, "backup")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(out, "Backup saved") {
		t.Errorf("backup printed %q", out)
	}
	cfg := s.config(t)
	items, err := backup.List(cfg.Paths.Backups)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("backups = %+v, want one", items)
	}
	if !strings.HasPrefix(items[0].Key, "manual-") {
		t.Errorf("backup key = %q", items[0].Key)
	}
}

func TestCLIChannelSwitchesAndReports(t *testing.T) {
	s := newTestSetup(t)
	out, err := runCLI(t, s, "channel")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if !strings.Contains(out, "Updates come from the stable channel.") {
		t.Errorf("channel = %q", out)
	}
	out, err = runCLI(t, s, "channel", "beta")
	if err != nil {
		t.Fatalf("channel beta: %v", err)
	}
	if !strings.Contains(out, "Updates now come from the beta channel") {
		t.Errorf("channel switch = %q", out)
	}
}

func TestCLIResetPasswordNeedsTheRightArguments(t *testing.T) {
	s := newTestSetup(t)
	_, err := runCLI(t, s, "reset-password", "ada", "short")
	if err == nil || !strings.Contains(err.Error(), "Usage: neorouter reset-password") {
		t.Fatalf("err = %v, want the usage line", err)
	}
	_, err = runCLI(t, s, "reset-2fa")
	if err == nil || !strings.Contains(err.Error(), "Usage: neorouter reset-2fa <username-or-email>") {
		t.Fatalf("err = %v, want the usage line", err)
	}
	_, err = runCLI(t, s, "providers", "import")
	if err == nil || !strings.Contains(err.Error(), "Usage: neorouter providers import") {
		t.Fatalf("err = %v, want the usage line", err)
	}
}

func TestCLIRefusesWhatItDoesNotKnowAndChangesNothing(t *testing.T) {
	s := newTestSetup(t)
	for _, command := range []string{"update", "install", "setup", "start", "stop", "restart", "uninstall", "repair", "backup", "restore", "logs", "status", "doctor"} {
		for _, bad := range []string{"--dry-run", "--chek", "-x", "now please"} {
			args := append([]string{command}, strings.Fields(bad)...)
			if _, err := runCLI(t, s, args...); cli.CodeOf(err, "") != "CLI_INPUT" {
				t.Errorf("%v: err = %v, want it refused", args, err)
			}
		}
		for _, flag := range []string{"--help", "-h"} {
			out, err := runCLI(t, s, command, flag)
			if err != nil || !strings.Contains(out, "Every neorouter command") {
				t.Errorf("%s %s: %v\n%s", command, flag, err, out)
			}
		}
	}
	if exists(s.env["NEOROUTER_HOME"]) {
		t.Error("a refused command or a help flag changed the install")
	}
}

func TestABadEnvValueDoesNotBrickTheCLI(t *testing.T) {
	s := newTestSetup(t)
	delete(s.env, "NEOROUTER_PORT")
	s.m.Getenv = os.Getenv
	t.Setenv("NEOROUTER_HOME", s.env["NEOROUTER_HOME"])
	writeFile(t, filepath.Join(s.env["NEOROUTER_HOME"], ".env"), "NEOROUTER_PORT=abc\n", 0o600)
	for _, args := range [][]string{{"help"}, {"version"}, {"env", "list"}, {"env", "set", "NEOROUTER_HOST", "127.0.0.1"}, {"env", "unset", "NEOROUTER_PORT"}} {
		if out, err := runCLI(t, s, args...); err != nil {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
}

func TestCLIBackupListAndRestore(t *testing.T) {
	s := newTestSetup(t)
	newFakeService(t, s)
	if out, err := runCLI(t, s, "backup", "list"); err != nil || !strings.Contains(out, "No backups yet.") {
		t.Fatalf("empty list: %v\n%s", err, out)
	}
	cfg := s.config(t)
	if _, err := s.m.migrateDatabase(s.ctx, cfg); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg.Paths.EnvFile, "A=saved\n", 0o600)
	if _, err := runCLI(t, s, "backup"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, s, "backup", "list")
	if err != nil || !strings.HasPrefix(out, "manual-") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	writeFile(t, cfg.Paths.EnvFile, "A=changed\n", 0o600)
	if out, err := runCLI(t, s, "restore", strings.Fields(out)[0]); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if got := readFile(t, cfg.Paths.EnvFile); got != "A=saved\n" {
		t.Errorf(".env = %q", got)
	}
	if _, err := runCLI(t, s, "restore", "nothing"); cli.CodeOf(err, "") != "CLI_INPUT" {
		t.Errorf("unknown backup: %v", err)
	}
}
