package config

import (
	"os"
	"path/filepath"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

// LoadEnvironment is runtime/env.js loadEnvironment: create the runtime
// directories, then load the install's .env and the working directory's .env
// without overriding variables that are already set.
func LoadEnvironment(paths Paths, cwd string) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	if err := runtimecfg.LoadEnvFile(paths.EnvFile, os.LookupEnv, os.Setenv); err != nil {
		return err
	}
	if cwd == "" {
		return nil
	}
	return runtimecfg.LoadEnvFile(filepath.Join(cwd, ".env"), os.LookupEnv, os.Setenv)
}

// EnvFileValues reads KEY=VALUE pairs from an env file; a missing file is empty.
func EnvFileValues(path string) (map[string]string, error) {
	return envfile.Load(path)
}

// UpsertEnvValue replaces the KEY= line in an env file, or appends one.
func UpsertEnvValue(path, key, value string) error {
	return envfile.Upsert(path, key, value)
}

// RemoveEnvValue deletes every KEY= line; false means the file did not exist.
func RemoveEnvValue(path, key string) (bool, error) {
	return envfile.Remove(path, key)
}

// ValidEnvKey reports whether key may be written to an env file.
func ValidEnvKey(key string) bool { return envfile.ValidKey(key) }
