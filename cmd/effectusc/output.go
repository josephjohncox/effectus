package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type artifactFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

type artifactFileOps struct {
	create func(string, string) (artifactFile, error)
	rename func(string, string) error
}

func writeCheckedArtifact(source, output string, data []byte) error {
	return writeCheckedArtifactWithOps(source, output, data, artifactFileOps{
		create: func(dir, pattern string) (artifactFile, error) { return os.CreateTemp(dir, pattern) },
		rename: os.Rename,
	})
}

func rejectArtifactAlias(source, output string) error {
	input, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat source bundle: %w", err)
	}
	entry, err := os.Lstat(output)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat output: %w", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output must not be a symbolic link")
	}
	if !entry.Mode().IsRegular() {
		return fmt.Errorf("output must be a regular file")
	}
	if os.SameFile(input, entry) {
		return fmt.Errorf("output aliases the source bundle")
	}
	return nil
}

func writeCheckedArtifactWithOps(source, output string, data []byte, ops artifactFileOps) error {
	if err := rejectArtifactAlias(source, output); err != nil {
		return err
	}
	file, err := ops.create(filepath.Dir(output), ".effectusc-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary artifact: %w", err)
	}
	name := file.Name()
	defer func() { _ = file.Close(); _ = os.Remove(name) }()
	written, err := file.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary artifact: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write temporary artifact: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary artifact: %w", err)
	}
	// Check again before replacement. Rename never follows an output symlink
	// and never truncates the source through a hardlink.
	if err := rejectArtifactAlias(source, output); err != nil {
		return err
	}
	if err := ops.rename(name, output); err != nil {
		return fmt.Errorf("replace checked artifact: %w", err)
	}
	return nil
}
