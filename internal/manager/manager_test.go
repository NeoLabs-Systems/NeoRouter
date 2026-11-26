package manager

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// isolateEnv keeps a test's env writes from leaking into the next one: the
// config loader loads the install's .env into the process environment, which is
// what the server does too.
func isolateEnv(t *testing.T) {
	t.Helper()
	before := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		before[key] = value
	}
	t.Cleanup(func() {
		os.Clearenv()
		for key, value := range before {
			_ = os.Setenv(key, value)
		}
	})
}

// testSetup is one fake install: a home directory and the environment that
// points at it.
type testSetup struct {
	home string
	env  map[string]string
	m    *Manager
	out  *bytes.Buffer
	ctx  context.Context
}

// newTestSetup builds a manager over a temporary home. Nothing is created on
// disk until a command does it.
func newTestSetup(t *testing.T) *testSetup {
	t.Helper()
	isolateEnv(t)
	home := t.TempDir()
	env := map[string]string{
		"NEOROUTER_HOME": filepath.Join(home, ".neorouter"),
		"PATH":           "/usr/bin:/bin",
		"SHELL":          "/bin/bash",
	}
	getenv := func(key string) string { return env[key] }
	opts := Options{Getenv: getenv, UserHome: home, Cwd: home}
	m, err := New(nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var out bytes.Buffer
	m.Out, m.Err = &out, &out
	m.UI.Out, m.UI.Err = &out, &out
	m.UI.In = strings.NewReader("")
	return &testSetup{home: home, env: env, m: m, out: &out, ctx: context.Background()}
}

// config is the install's resolved configuration.
func (s *testSetup) config(t *testing.T) config.Config {
	t.Helper()
	cfg, err := s.m.configWithPaths()
	if err != nil {
		t.Fatalf("configWithPaths: %v", err)
	}
	return cfg
}

// output is everything the command printed.
func (s *testSetup) output() string { return s.out.String() }

func TestHelpShowsEverydayCommandsAndAllListsTheRest(t *testing.T) {
	s := newTestSetup(t)
	if err := s.m.cmdHelp(s.ctx, nil); err != nil {
		t.Fatalf("help: %v", err)
	}
	everyday := s.output()
	for _, want := range []string{"When something's wrong", "neorouter doctor", "neorouter install"} {
		if !strings.Contains(everyday, want) {
			t.Errorf("help output is missing %q:\n%s", want, everyday)
		}
	}
	if strings.Contains(everyday, "reset-password") {
		t.Errorf("the everyday help listed a developer command:\n%s", everyday)
	}
	s.out.Reset()
	if err := s.m.cmdHelp(s.ctx, []string{"--all"}); err != nil {
		t.Fatalf("help --all: %v", err)
	}
	if !strings.Contains(s.output(), "neorouter reset-password") {
		t.Errorf("help --all is missing the reference commands:\n%s", s.output())
	}
}

func TestUnknownCommandIsAnInputErrorPointingAtHelp(t *testing.T) {
	s := newTestSetup(t)
	err := s.m.Run(s.ctx, []string{"nope"})
	if err == nil {
		t.Fatal("an unknown command did not fail")
	}
	if got := cli.CodeOf(err, ""); got != "CLI_INPUT" {
		t.Errorf("code = %q, want CLI_INPUT", got)
	}
	if !strings.Contains(err.Error(), "There's no neorouter nope command.") {
		t.Errorf("message = %q", err)
	}
	var written bytes.Buffer
	PrintError(&written, err, false)
	for _, want := range []string{"That command didn't work", "Try", "neorouter help"} {
		if !strings.Contains(written.String(), want) {
			t.Errorf("error output is missing %q:\n%s", want, written.String())
		}
	}
	if strings.Contains(written.String(), "Technical details") {
		t.Errorf("a plain input error offered technical details:\n%s", written.String())
	}
}

func TestVersionIsOneBareLine(t *testing.T) {
	s := newTestSetup(t)
	if err := s.m.cmdVersion(s.ctx, nil); err != nil {
		t.Fatalf("version: %v", err)
	}
	lines := strings.Split(strings.TrimRight(s.output(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("version printed %d lines: %q", len(lines), s.output())
	}
	if !strings.HasPrefix(lines[0], "0.") {
		t.Errorf("version = %q, want the release version", lines[0])
	}
}

func TestEnvCommandsReadAndWriteTheInstallEnvFile(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	if err := s.m.cmdEnv(s.ctx, []string{"set", "NEOROUTER_HOST", "127.0.0.1"}); err != nil {
		t.Fatalf("env set: %v", err)
	}
	raw, err := os.ReadFile(cfg.Paths.EnvFile)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(raw), "NEOROUTER_HOST=127.0.0.1") {
		t.Errorf(".env = %q", raw)
	}
	s.out.Reset()
	if err := s.m.cmdEnv(s.ctx, []string{"get", "NEOROUTER_HOST"}); err != nil {
		t.Fatalf("env get: %v", err)
	}
	if got := strings.TrimSpace(s.output()); got != "127.0.0.1" {
		t.Errorf("env get printed %q", got)
	}
	if err := s.m.cmdEnv(s.ctx, []string{"set", "NEOROUTER_OPENAI_KEY", "sk-secret-value"}); err != nil {
		t.Fatalf("env set secret: %v", err)
	}
	s.out.Reset()
	if err := s.m.cmdEnv(s.ctx, []string{"list"}); err != nil {
		t.Fatalf("env list: %v", err)
	}
	listed := s.output()
	if strings.Contains(listed, "sk-secret-value") {
		t.Errorf("env list printed a secret:\n%s", listed)
	}
	if !strings.Contains(listed, "NEOROUTER_OPENAI_KEY=sk********ue") {
		t.Errorf("env list did not mask the secret:\n%s", listed)
	}
	s.out.Reset()
	if err := s.m.cmdEnv(s.ctx, []string{"unset", "NEOROUTER_HOST"}); err != nil {
		t.Fatalf("env unset: %v", err)
	}
	raw, _ = os.ReadFile(cfg.Paths.EnvFile)
	if strings.Contains(string(raw), "NEOROUTER_HOST=") {
		t.Errorf("env unset left the key behind: %q", raw)
	}
}

func TestEnvRejectsBadNamesAndMissingArguments(t *testing.T) {
	s := newTestSetup(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"set", "2BAD", "x"}, "Invalid environment variable name: 2BAD"},
		{[]string{"set", "OK", ""}, "Usage: neorouter env set <KEY> <VALUE>"},
		{[]string{"get"}, "Usage: neorouter env get <KEY>"},
		{[]string{"get", "MISSING"}, "Key not found: MISSING"},
		{[]string{"unset"}, "Usage: neorouter env unset <KEY>"},
		{[]string{"nonsense"}, "Usage: neorouter env [list|get|set|unset] ..."},
	}
	for _, test := range cases {
		err := s.m.cmdEnv(s.ctx, test.args)
		if err == nil {
			t.Errorf("%v did not fail", test.args)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%v: error = %q, want %q", test.args, err, test.want)
		}
	}
}

func TestStatusChecksNameTheCommandThatFixesEachProblem(t *testing.T) {
	facts := statusFacts{
		Running:          true,
		URL:              "http://localhost:4600/app",
		Version:          "1.2.0",
		RunningVersion:   "1.1.0",
		ServiceActive:    false,
		ConfigPresent:    true,
		WebClientPresent: true,
		Update:           &statusUpdate{Version: "1.3.0"},
	}
	var fixes []string
	for _, check := range statusChecks(facts) {
		if check.Fix != "" {
			fixes = append(fixes, check.Fix)
		}
	}
	want := []string{"neorouter restart", "neorouter install", "neorouter update"}
	if strings.Join(fixes, ",") != strings.Join(want, ",") {
		t.Errorf("fixes = %v, want %v", fixes, want)
	}
	inContainer := facts
	inContainer.InContainer = true
	for _, check := range statusChecks(inContainer) {
		if check.Fix == "neorouter install" {
			t.Error("a container was told to install a service")
		}
	}
	headlineCheck := headline(facts, statusChecks(facts))
	if headlineCheck.Label != "Running — 3 things need a look" {
		t.Errorf("headline = %q", headlineCheck.Label)
	}
	stopped := facts
	stopped.Running = false
	if got := headline(stopped, statusChecks(stopped)).Label; got != "NeoRouter isn't running" {
		t.Errorf("headline with a stopped server = %q", got)
	}
}

func TestStatusPrintsTheFixUnderANarrowWindow(t *testing.T) {
	s := newTestSetup(t)
	m := s.m
	m.UI.Out = &bytes.Buffer{}
	facts := statusFacts{Port: 4600, URL: "http://localhost:4600/app", Version: "0.2.1", ConfigPresent: true}
	m.printStatus(facts, true)
	printed := m.UI.Out.(*bytes.Buffer).String()
	for _, want := range []string{"NeoRouter isn't running", "neorouter start", "Details", "Installed in", "Port"} {
		if !strings.Contains(printed, want) {
			t.Errorf("status output is missing %q:\n%s", want, printed)
		}
	}
}

func TestMaskEnvValueHidesSecretsOnly(t *testing.T) {
	cases := []struct{ key, value, want string }{
		{"NEOROUTER_HOST", "127.0.0.1", "127.0.0.1"},
		{"NEOROUTER_SECRET", "abcdefghij", "ab********ij"},
		{"SOME_TOKEN", "short", "********"},
		{"NEOROUTER_API_KEY", "", ""},
	}
	for _, test := range cases {
		if got := maskEnvValue(test.key, test.value); got != test.want {
			t.Errorf("maskEnvValue(%q, %q) = %q, want %q", test.key, test.value, got, test.want)
		}
	}
}

func TestChannelShowsAndSwitches(t *testing.T) {
	s := newTestSetup(t)
	if err := s.m.cmdChannel(s.ctx, nil); err != nil {
		t.Fatalf("channel: %v", err)
	}
	if !strings.Contains(s.output(), "Updates come from the stable channel.") {
		t.Errorf("channel = %q, want stable by default", s.output())
	}
	s.out.Reset()
	if err := s.m.cmdChannel(s.ctx, []string{"beta"}); err != nil {
		t.Fatalf("channel stable: %v", err)
	}
	if !strings.Contains(s.output(), "Updates now come from the beta channel") {
		t.Errorf("channel switch = %q", s.output())
	}
	if err := s.m.cmdChannel(s.ctx, []string{"nonsense"}); err == nil ||
		!strings.Contains(err.Error(), "Usage: neorouter channel [stable|beta]") {
		t.Errorf("bad channel error = %v", err)
	}
}

func TestUserByNameReportsAnUnknownAccount(t *testing.T) {
	s := newTestSetup(t)
	err := s.m.withDB(s.ctx, func(ctx context.Context, db *dbx.DB, cfg config.Config) error {
		_, _, err := userByName(ctx, db, "nobody")
		return err
	})
	if !errors.Is(err, errUserNotFound) {
		t.Fatalf("err = %v, want %v", err, errUserNotFound)
	}
}
