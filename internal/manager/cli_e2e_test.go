package manager

// The CLI as people use it: `Run` with the arguments a shell would pass, over a
// temporary install. Port of the cases test/unit/cli.test.js checks through the
// Node entry point.

import (
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
	if err := s.m.migrateDatabase(s.ctx); err != nil {
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
	if !strings.HasPrefix(items[0].Key, "pre-update-") {
		t.Errorf("backup key = %q", items[0].Key)
	}
}

func TestCLIChannelSwitchesAndReports(t *testing.T) {
	s := newTestSetup(t)
	out, err := runCLI(t, s, "channel")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if !strings.Contains(out, "Updates come from the beta channel.") {
		t.Errorf("channel = %q", out)
	}
	out, err = runCLI(t, s, "channel", "stable")
	if err != nil {
		t.Fatalf("channel stable: %v", err)
	}
	if !strings.Contains(out, "Updates now come from the stable channel") {
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
