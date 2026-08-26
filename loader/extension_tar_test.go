package loader

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarLayerRejectsTraversal(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	outside := filepath.Join(parent, "escape")

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	body := []byte("owned")
	if err := writer.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if err := extractTarLayer(bytes.NewReader(archive.Bytes()), target); err == nil {
		t.Fatal("extractTarLayer() accepted a path outside the target directory")
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape path exists or stat failed unexpectedly: %v", err)
	}
}
