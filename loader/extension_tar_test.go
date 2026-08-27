package loader

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractTarLayerRejectsLinks(t *testing.T) {
	for _, typeFlag := range []byte{tar.TypeSymlink, tar.TypeLink} {
		t.Run(string(rune(typeFlag)), func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "link", Linkname: "../outside", Typeflag: typeFlag, Mode: 0o777}))
			require.NoError(t, writer.Close())
			err := extractTarLayer(bytes.NewReader(archive.Bytes()), t.TempDir())
			require.Error(t, err)
		})
	}
}

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
