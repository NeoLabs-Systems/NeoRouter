package localmodels

// goarchive.go unpacks the release archives llama.cpp publishes. The Node
// runtime shelled out to `tar -xzf`; the standard library does the same work
// here, with the same layout rule: releases wrap their files in one folder, and
// that folder's contents are what lands in the target.

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractTarGz unpacks archive into target. Everything is staged in
// "<target>.unpack" first, so a half-unpacked build never looks installed.
func extractTarGz(archive, target string) error {
	staging := target + ".unpack"
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	if err := untarGz(archive, staging); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	root := staging
	if len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(staging, entries[0].Name())
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	children, err := os.ReadDir(root)
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	for _, child := range children {
		from := filepath.Join(root, child.Name())
		to := filepath.Join(target, child.Name())
		if err := os.Rename(from, to); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	return os.RemoveAll(staging)
}

// untarGz writes every entry of a gzipped tar into dest, refusing entries that
// would land outside it.
func untarGz(archive, dest string) error {
	handle, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer handle.Close()
	gzipReader, err := gzip.NewReader(handle)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(strings.TrimPrefix(header.Name, "./"))
		if name == "" {
			continue
		}
		targetPath := filepath.Join(root, name)
		if targetPath != root && !strings.HasPrefix(targetPath, root+string(filepath.Separator)) {
			return fmt.Errorf("llama.cpp %s contains an unsafe path.", LlamaCppBuild)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o755
			out, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Builds ship real files; a link could point anywhere.
			return fmt.Errorf("llama.cpp %s contains a symbolic link.", LlamaCppBuild)
		}
	}
}
