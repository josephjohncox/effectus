package guardrails

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Use the actual site configuration and renderer. A positive build must pass
// before negative fixtures can establish that the configured checks reject them.
func TestDocumentedStrictSiteContracts(t *testing.T) {
	mkdocs, err := exec.LookPath("mkdocs")
	if err != nil && os.Getenv("EFFECTUS_REQUIRE_DOCS") != "1" {
		t.Skip("MkDocs unavailable; install requirements-docs.txt and set EFFECTUS_REQUIRE_DOCS=1 for the required gate")
	}
	require.NoError(t, err)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	config, err := os.ReadFile(filepath.Join(root, "mkdocs.yml"))
	require.NoError(t, err)

	for _, test := range []struct {
		name    string
		link    string
		newPage string
		want    string
	}{
		{name: "valid site"},
		{name: "missing document", link: "r34-missing-page.md", want: "r34-missing-page.md"},
		{name: "missing anchor", link: "#r34-missing-anchor", want: "r34-missing-anchor"},
		{name: "unlisted page", newPage: "r34-unlisted.md", want: "r34-unlisted.md"},
		{name: "unrecognized link", link: "r34-unrecognized/", want: "r34-unrecognized/"},
		{name: "absolute link", link: "/r34-absolute", want: "/r34-absolute"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			docs := filepath.Join(workspace, "docs")
			require.NoError(t, copyDocumentationTree(os.DirFS(filepath.Join(root, "docs")), docs))
			configPath := filepath.Join(workspace, "mkdocs.yml")
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			if test.link != "" {
				index := filepath.Join(docs, "index.md")
				data, err := os.ReadFile(index)
				require.NoError(t, err)
				data = append(data, []byte("\n[Negative contract fixture]("+test.link+")\n")...)
				require.NoError(t, os.WriteFile(index, data, 0o600))
			}
			if test.newPage != "" {
				require.NoError(t, os.WriteFile(filepath.Join(docs, test.newPage), []byte("# Unlisted page\n"), 0o600))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, mkdocs, "build", "--strict", "--config-file", configPath, "--site-dir", filepath.Join(workspace, "site"))
			command.Dir = workspace
			command.Env = append(os.Environ(), "NO_MKDOCS_2_WARNING=true")
			command.WaitDelay = 2 * time.Second
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), string(output))
			if test.want == "" {
				require.NoError(t, err, string(output))
				require.NotContains(t, string(output), "WARNING")
				require.FileExists(t, filepath.Join(workspace, "site", "index.html"))
				return
			}
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, 1, exit.ExitCode(), string(output))
			require.Contains(t, string(output), "WARNING")
			require.Contains(t, string(output), test.want)
			require.Contains(t, string(output), "Aborted with")
		})
	}
}

func copyDocumentationTree(source fs.FS, destination string) error {
	return fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return &fs.PathError{Op: "copy documentation", Path: path, Err: fs.ErrInvalid}
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

func TestDocumentationCopyRejectsLinks(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX symlink fixture")
	}
	source := t.TempDir()
	require.NoError(t, os.Symlink("outside", filepath.Join(source, "link.md")))
	err := copyDocumentationTree(os.DirFS(source), t.TempDir())
	require.ErrorIs(t, err, fs.ErrInvalid)
	require.Contains(t, err.Error(), "link.md")
}

func TestDocumentationToolProbe(t *testing.T) {
	if os.Getenv("EFFECTUS_DOC_TOOL_PROBE") == "1" {
		TestDocumentedStrictSiteContracts(t)
	}
}

func TestDocumentedMissingRendererIsNotValidation(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, required := range []string{"0", "1"} {
		t.Run("required="+required, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.v", "-test.run=^TestDocumentationToolProbe$")
			command.Dir = t.TempDir()
			command.Env = append(os.Environ(), "PATH=", "EFFECTUS_DOC_TOOL_PROBE=1", "EFFECTUS_REQUIRE_DOCS="+required)
			command.WaitDelay = 2 * time.Second
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), string(output))
			if required == "0" {
				require.NoError(t, err, string(output))
				require.Contains(t, string(output), "SKIP")
				require.Contains(t, string(output), "MkDocs unavailable")
				return
			}
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, 1, exit.ExitCode(), string(output))
			require.Contains(t, string(output), "mkdocs")
			require.NotContains(t, string(output), "SKIP")
		})
	}
}
