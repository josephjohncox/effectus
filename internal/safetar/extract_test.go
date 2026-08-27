package safetar

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func makeTar(t *testing.T, entries ...tarEntry) *bytes.Reader {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0o644,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if entry.body != "" {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	return bytes.NewReader(buffer.Bytes())
}

func TestExtractWritesRegularFiles(t *testing.T) {
	target := t.TempDir()
	err := Extract(makeTar(t,
		tarEntry{name: "schema/user.json", body: `{"name":"user"}`},
		tarEntry{name: "rules/check.eff", body: "rule check {}"},
	), target, DefaultLimits())
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(target, "schema", "user.json"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if got, want := string(content), `{"name":"user"}`; got != want {
		t.Fatalf("extracted content = %q, want %q", got, want)
	}
}

func TestExtractRejectsEscapingPaths(t *testing.T) {
	absoluteParent := t.TempDir()
	absolutePath := filepath.Join(absoluteParent, "absolute-escape")

	tests := []struct {
		name string
		path string
	}{
		{name: "parent traversal", path: "../escape"},
		{name: "nested parent traversal", path: "safe/../../escape"},
		{name: "absolute path", path: absolutePath},
		{name: "Windows parent traversal", path: `..\escape`},
		{name: "Windows drive path", path: `C:\escape`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := t.TempDir()
			err := Extract(makeTar(t, tarEntry{name: test.path, body: "owned"}), target, DefaultLimits())
			if err == nil {
				t.Fatalf("Extract() accepted unsafe path %q", test.path)
			}
		})
	}

	if _, err := os.Stat(absolutePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absolute escape path exists or stat failed unexpectedly: %v", err)
	}
}

func TestExtractRejectsLinksAndSpecialFiles(t *testing.T) {
	tests := []struct {
		name     string
		typeflag byte
	}{
		{name: "symbolic link", typeflag: tar.TypeSymlink},
		{name: "hard link", typeflag: tar.TypeLink},
		{name: "character device", typeflag: tar.TypeChar},
		{name: "block device", typeflag: tar.TypeBlock},
		{name: "fifo", typeflag: tar.TypeFifo},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Extract(makeTar(t, tarEntry{
				name:     "unsafe",
				typeflag: test.typeflag,
				linkname: "../escape",
			}), t.TempDir(), DefaultLimits())
			if err == nil {
				t.Fatalf("Extract() accepted tar type %d", test.typeflag)
			}
		})
	}
}

func TestExtractRejectsExistingSymlinkEscape(t *testing.T) {
	target := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(target, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := Extract(makeTar(t, tarEntry{name: "link/escape", body: "owned"}), target, DefaultLimits())
	if err == nil {
		t.Fatal("Extract() followed a symlink outside the extraction root")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink escape path exists or stat failed unexpectedly: %v", err)
	}
}

func TestExtractEnforcesLimits(t *testing.T) {
	tests := []struct {
		name    string
		limits  Limits
		entries []tarEntry
	}{
		{
			name:    "entry count",
			limits:  Limits{MaxEntries: 1, MaxFileSize: 10, MaxTotalSize: 10},
			entries: []tarEntry{{name: "one"}, {name: "two"}},
		},
		{
			name:    "single file size",
			limits:  Limits{MaxEntries: 2, MaxFileSize: 2, MaxTotalSize: 10},
			entries: []tarEntry{{name: "large", body: "123"}},
		},
		{
			name:    "total size",
			limits:  Limits{MaxEntries: 2, MaxFileSize: 10, MaxTotalSize: 5},
			entries: []tarEntry{{name: "one", body: "123"}, {name: "two", body: "456"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Extract(makeTar(t, test.entries...), t.TempDir(), test.limits)
			if err == nil {
				t.Fatal("Extract() accepted an archive exceeding configured limits")
			}
		})
	}
}
