package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/josephjohncox/effectus/bundle"
)

// cacheVerifiedSourceBundle atomically stores the canonical document produced
// by a successful verified OCI pull. It deliberately accepts a SourceBundle,
// rather than OCI bytes, so unverified data cannot enter the daemon cache.
func cacheVerifiedSourceBundle(cacheDir string, sourceBundle *bundle.SourceBundle) error {
	if strings.TrimSpace(cacheDir) == "" {
		return nil
	}
	data, err := sourceBundle.Bytes()
	if err != nil {
		return fmt.Errorf("encode cached source bundle: %w", err)
	}
	digest := sha256.Sum256(data)
	filename := fmt.Sprintf("source-bundle-sha256-%x.json", digest)

	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("create source-bundle OCI cache: %w", err)
	}
	temporary, err := os.CreateTemp(cacheDir, ".effectus-source-bundle-*")
	if err != nil {
		return fmt.Errorf("create cached source bundle: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set cached source bundle permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write cached source bundle: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cached source bundle: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(cacheDir, filename)); err != nil {
		return fmt.Errorf("store cached source bundle: %w", err)
	}
	return nil
}
