package localmodels

// paths.go says where local models live on disk — the engines NeoRouter
// downloads (runtimes/), the model files (models/<id>/, laid out like the
// source repository) and the processes still running (processes.json, so a
// crashed NeoRouter can end them on the next start).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths are one install's local model directories.
type Paths struct {
	Root          string
	RuntimesDir   string
	ModelsDir     string
	ProcessesFile string
}

// NewPaths returns the local model paths under a data directory (config's
// Paths.Data).
func NewPaths(dataDir string) Paths {
	root := filepath.Join(dataDir, "local-models")
	return Paths{
		Root:          root,
		RuntimesDir:   filepath.Join(root, "runtimes"),
		ModelsDir:     filepath.Join(root, "models"),
		ProcessesFile: filepath.Join(root, "processes.json"),
	}
}

// EnsureDirs creates the root, runtime and model directories with 0700.
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.Root, p.RuntimesDir, p.ModelsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("localmodels: create %s: %w", dir, err)
		}
	}
	return nil
}

// ModelDir is the directory of one model's files.
func (p Paths) ModelDir(id string) string { return filepath.Join(p.ModelsDir, id) }

// Inside resolves a repository-relative path inside base and refuses anything
// that would leave it (a model's file list is remote data).
func (p Paths) Inside(base, relative string) (string, error) {
	refuse := fmt.Errorf("Refusing a path outside %s: %s", base, relative)
	if filepath.IsAbs(relative) {
		return "", refuse
	}
	cleanBase, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", refuse
	}
	target := filepath.Join(cleanBase, filepath.FromSlash(relative))
	if target == cleanBase || strings.HasPrefix(target, cleanBase+string(filepath.Separator)) {
		return target, nil
	}
	return "", refuse
}
