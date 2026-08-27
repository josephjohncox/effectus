package safetar

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits bounds the resources consumed while extracting one tar stream.
type Limits struct {
	MaxEntries   int64
	MaxFileSize  int64
	MaxTotalSize int64
}

// DefaultLimits returns conservative limits for Effectus bundle layers.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:   10_000,
		MaxFileSize:  256 << 20,
		MaxTotalSize: 1 << 30,
	}
}

// Extract extracts regular files and directories beneath targetDir.
// Links, special files, paths outside targetDir, and oversized archives are rejected.
func Extract(r io.Reader, targetDir string, limits Limits) error {
	if err := validateLimits(limits); err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating extraction root: %w", err)
	}

	root, err := os.OpenRoot(targetDir)
	if err != nil {
		return fmt.Errorf("opening extraction root: %w", err)
	}
	defer root.Close()

	tr := tar.NewReader(r)
	var entries int64
	var totalSize int64

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		entries++
		if entries > limits.MaxEntries {
			return fmt.Errorf("tar contains more than %d entries", limits.MaxEntries)
		}

		name, err := cleanName(header.Name)
		if err != nil {
			return fmt.Errorf("invalid tar entry %q: %w", header.Name, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if name == "." {
				continue
			}
			if err := root.MkdirAll(name, 0o755); err != nil {
				return fmt.Errorf("creating directory %q: %w", header.Name, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if name == "." {
				return fmt.Errorf("invalid tar entry %q: file name resolves to extraction root", header.Name)
			}
			if header.Size < 0 {
				return fmt.Errorf("invalid tar entry %q: negative file size", header.Name)
			}
			if header.Size > limits.MaxFileSize {
				return fmt.Errorf("tar entry %q exceeds maximum file size of %d bytes", header.Name, limits.MaxFileSize)
			}
			if header.Size > limits.MaxTotalSize-totalSize {
				return fmt.Errorf("tar exceeds maximum total size of %d bytes", limits.MaxTotalSize)
			}

			if parent := filepath.Dir(name); parent != "." {
				if err := root.MkdirAll(parent, 0o755); err != nil {
					return fmt.Errorf("creating directory for %q: %w", header.Name, err)
				}
			}

			file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("creating file %q: %w", header.Name, err)
			}

			written, copyErr := io.CopyN(file, tr, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				_ = root.Remove(name)
				return fmt.Errorf("writing file %q after %d bytes: %w", header.Name, written, copyErr)
			}
			if closeErr != nil {
				_ = root.Remove(name)
				return fmt.Errorf("closing file %q: %w", header.Name, closeErr)
			}
			totalSize += written

		default:
			return fmt.Errorf("unsupported tar entry %q with type %d", header.Name, header.Typeflag)
		}
	}
}

func validateLimits(limits Limits) error {
	if limits.MaxEntries <= 0 {
		return fmt.Errorf("maximum entry count must be positive")
	}
	if limits.MaxFileSize <= 0 {
		return fmt.Errorf("maximum file size must be positive")
	}
	if limits.MaxTotalSize <= 0 {
		return fmt.Errorf("maximum total size must be positive")
	}
	return nil
}

func cleanName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty name")
	}
	if strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("name contains NUL")
	}

	// Tar names use slash separators. Normalize backslashes as well so an
	// archive cannot become safe on Unix but unsafe when extracted on Windows.
	normalized := strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(normalized)
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("name escapes extraction root")
	}
	if len(cleaned) >= 2 && isASCIIAlpha(cleaned[0]) && cleaned[1] == ':' {
		return "", fmt.Errorf("name has a Windows drive prefix")
	}

	return filepath.FromSlash(cleaned), nil
}

func isASCIIAlpha(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
