// Package backup writes the snapshot of an install's settings and data that
// `neorouter backup`, the update and the legacy migration take before they
// touch anything.
//
// The snapshot is a plain directory under <home>/backups, exactly what
// lib/manager.js backupRuntimeData builds: pre-update-<stamp>/.env next to
// pre-update-<stamp>/data/, keeping the newest three so a rollback always has a
// copy of the database, secret.key and settings.
package backup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Prefix is the directory-name prefix of a snapshot; the prune step only ever
// removes directories that start with it.
const Prefix = "pre-update-"

// Keep is how many snapshots are kept.
const Keep = 3

var artifactPattern = regexp.MustCompile(`^pre-update-\d{8}T\d{6}Z$`)

// Artifact is one saved snapshot.
type Artifact struct {
	// Key is the directory name, such as pre-update-20260101T101010Z.
	Key string `json:"key"`
	// Path is the snapshot directory.
	Path string `json:"path"`
	// Bytes is the size of everything it holds.
	Bytes int64 `json:"bytes"`
	// CreatedAt is the directory's modification time in ISO milliseconds.
	CreatedAt string `json:"createdAt"`
}

// Options describe one install's snapshot.
type Options struct {
	// Dir holds the snapshots: config.Paths.Backups.
	Dir string
	// EnvFile and DataDir are what gets copied; either may be empty or missing.
	EnvFile string
	DataDir string
	// Keep overrides how many snapshots survive; 0 uses Keep, negative keeps all.
	Keep int
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Stamp is the timestamp in a snapshot's name: an ISO instant with the colons
// and milliseconds removed, so the name is safe on every filesystem.
func Stamp(now time.Time) string {
	return now.UTC().Format("20060102T150405Z")
}

// Create copies the install's settings and data into a new snapshot and prunes
// the older ones. A missing .env or data directory is skipped: an install that
// never started has nothing to copy, and that is not an error.
func Create(opts Options) (Artifact, error) {
	if opts.Dir == "" {
		return Artifact{}, errors.New("backup: no backup directory configured")
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	keep := opts.Keep
	if keep == 0 {
		keep = Keep
	}
	target := filepath.Join(opts.Dir, Prefix+Stamp(now))
	if err := os.MkdirAll(target, 0o700); err != nil {
		return Artifact{}, fmt.Errorf("backup: create snapshot: %w", err)
	}
	if opts.EnvFile != "" {
		if err := copyFile(opts.EnvFile, filepath.Join(target, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Artifact{}, err
		}
	}
	if opts.DataDir != "" {
		if err := copyTree(opts.DataDir, filepath.Join(target, "data")); err != nil {
			return Artifact{}, err
		}
	}
	Prune(opts.Dir, keep)
	return artifactOf(target)
}

// Prune removes the oldest snapshots beyond keep. It is best effort: a snapshot
// that cannot be read or removed never fails the backup that just succeeded.
func Prune(dir string, keep int) {
	if keep < 0 {
		return
	}
	items, err := List(dir)
	if err != nil || len(items) <= keep {
		return
	}
	for _, item := range items[keep:] {
		_ = os.RemoveAll(item.Path)
	}
}

// List returns the snapshots in dir, newest first.
func List(dir string) ([]Artifact, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: list snapshots: %w", err)
	}
	var out []Artifact
	for _, entry := range entries {
		if !entry.IsDir() || !artifactPattern.MatchString(entry.Name()) {
			continue
		}
		item, err := artifactOf(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Key > out[j].Key
	})
	return out, nil
}

func artifactOf(path string) (Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	size, err := treeSize(path)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Key:       filepath.Base(path),
		Path:      path,
		Bytes:     size,
		CreatedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}

func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			if info, ierr := entry.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// copyTree copies a directory tree, keeping directories private (0700) and
// files private (0600). Symbolic links are refused: an install's data
// directory holds keys and a database, never links.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == src {
				return fs.SkipAll
			}
			return fmt.Errorf("backup: read %s: %w", path, err)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("backup: resolve %s: %w", path, err)
		}
		target := filepath.Join(dst, rel)
		switch {
		case entry.IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("backup: create %s: %w", target, err)
			}
			return nil
		case entry.Type()&os.ModeSymlink != 0:
			return fmt.Errorf("backup: refusing to copy symbolic link %s", path)
		default:
			return copyFile(path, target)
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("backup: create %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("backup: copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("backup: write %s: %w", dst, err)
	}
	return nil
}
