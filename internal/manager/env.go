package manager

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
)

// envKeyPattern is the name of a variable people may set.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateEnvKey refuses a name that cannot be an environment variable.
func validateEnvKey(key string) error {
	if envKeyPattern.MatchString(key) {
		return nil
	}
	if key == "" {
		key = "(empty)"
	}
	return fmt.Errorf("Invalid environment variable name: %s", key)
}

// secretKeyPattern is what maskEnvValue treats as a secret: a name mentioning a
// password, secret, token, key or API.
var secretKeyPattern = regexp.MustCompile(`PASSWORD|SECRET|TOKEN|KEY|API`)

// maskEnvValue hides a secret's middle, keeping its first and last two
// characters so an admin can still tell two keys apart.
func maskEnvValue(key, value string) string {
	if value == "" || !secretKeyPattern.MatchString(strings.ToUpper(key)) {
		return value
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:2] + "********" + value[len(value)-2:]
}

// cmdEnv is `neorouter env`, which reads and changes the install's .env without
// printing a secret.
func (m *Manager) cmdEnv(_ context.Context, args []string) error {
	// Only the paths: a bad value in .env must still be listable and removable.
	if err := m.ensureEnvFile(""); err != nil {
		return err
	}
	envFile := m.installPaths().EnvFile
	action := ""
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch action {
	case "":
		m.UI.Title("Settings", "Saved in "+envFile)
		m.UI.Blank()
		for _, row := range [][2]string{
			{"env list", "List all settings (secrets masked)"},
			{"env get KEY", "Print one setting"},
			{"env set KEY VALUE", "Change a setting"},
			{"env unset KEY", "Remove a setting"},
		} {
			m.UI.Print("  " + padEndPlain("neorouter "+row[0], 30) + m.UI.Muted(row[1]))
		}
		m.UI.Blank()
		return nil
	case "list":
		values, err := envfile.Load(envFile)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			m.UI.Warn("No .env found at " + envFile)
			return nil
		}
		for _, key := range envfile.SortedKeys(values) {
			fmt.Fprintf(m.Out, "%s=%s\n", key, maskEnvValue(key, values[key]))
		}
		return nil
	case "get":
		if len(args) < 2 {
			return inputError("Usage: neorouter env get <KEY>")
		}
		values, err := envfile.Load(envFile)
		if err != nil {
			return err
		}
		value, ok := values[args[1]]
		if !ok {
			return inputError("Key not found: " + args[1])
		}
		fmt.Fprintln(m.Out, value)
		return nil
	case "set":
		if len(args) < 3 {
			return inputError("Usage: neorouter env set <KEY> <VALUE>")
		}
		key := args[1]
		value := strings.Join(args[2:], " ")
		if value == "" {
			return inputError("Usage: neorouter env set <KEY> <VALUE>")
		}
		if err := validateEnvKey(key); err != nil {
			return err
		}
		if err := envfile.Upsert(envFile, key, value); err != nil {
			return err
		}
		m.UI.OK(fmt.Sprintf("Set %s in %s", key, envFile))
		return nil
	case "unset":
		if len(args) < 2 {
			return inputError("Usage: neorouter env unset <KEY>")
		}
		if _, err := envfile.Remove(envFile, args[1]); err != nil {
			return err
		}
		m.UI.OK(fmt.Sprintf("Removed %s from %s", args[1], envFile))
		return nil
	}
	return inputError("Usage: neorouter env [list|get|set|unset] ...")
}

// padEndPlain pads text that carries no color codes.
func padEndPlain(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}
