package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This subprocess uses the executable's real flag definitions and mode
// validation. It never opens a database, bundle, or listener.
func TestDocumentedStartupFlagHelper(t *testing.T) {
	if os.Getenv("EFFECTUS_DOC_FLAG_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" {
			continue
		}
		args := os.Args[i+1:]
		require.GreaterOrEqual(t, len(args), 2)
		require.Equal(t, []string{"run", "./cmd/effectusd"}, args[:2])
		require.NoError(t, flag.CommandLine.Parse(args[2:]))
		require.NoError(t, validateDaemonMode())
		require.Equal(t, "serve", *runMode)
		require.False(t, *migrateOnly)
		require.NotEmpty(t, *bundleFile)
		require.NotEmpty(t, os.Getenv("EFFECTUS_POSTGRES_DSN"))
		require.NotEmpty(t, os.Getenv("EFFECTUS_API_TOKEN"))
		require.NoError(t, os.WriteFile(os.Getenv("EFFECTUS_DOC_FLAG_MARKER"), []byte("validated"), 0o600))
		fmt.Println("startup flags and environment validated; no database opened")
		os.Exit(0)
	}
	t.Fatal("missing process argument separator")
}

func TestDocumentedIntegrationGuideDaemonStartupContract(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the documented recipe uses a POSIX shell")
	}
	documentation, err := os.ReadFile("../../docs/INTEGRATION.md")
	require.NoError(t, err)
	var script string
	for _, block := range strings.Split(string(documentation), "```bash\n")[1:] {
		candidate, _, closed := strings.Cut(block, "```")
		require.True(t, closed)
		if strings.Contains(candidate, "go run ./cmd/effectusd ") {
			require.Empty(t, script, "startup recipe must not be ambiguous")
			script = candidate
		}
	}
	require.NotEmpty(t, script)
	binary, err := os.Executable()
	require.NoError(t, err)
	bin := t.TempDir()
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	wrapper := "#!/bin/sh\nexec " + quoted + " -test.run=^TestDocumentedStartupFlagHelper$ -- \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(wrapper), 0o700))
	// Run the reviewed repository snippet as a test-owned file. Do not compose
	// shell source from runtime arguments, credentials, or other caller input.
	scriptPath := filepath.Join(bin, "startup.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o600))
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	for _, test := range []struct {
		name, dsn, token string
		valid            bool
	}{{"missing-dsn", "", "test-token", false}, {"missing-token", "synthetic-dsn", "", false}, {"configured", "synthetic-dsn", "test-token", true}} {
		t.Run(test.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "validated")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "sh", "-eu", scriptPath)
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "EFFECTUS_DOC_FLAG_HELPER=1", "EFFECTUS_DOC_FLAG_MARKER="+marker, "EFFECTUS_POSTGRES_DSN="+test.dsn, "EFFECTUS_API_TOKEN="+test.token)
			output, err := command.CombinedOutput()
			if test.valid {
				require.NoError(t, err, string(output))
				require.FileExists(t, marker)
				require.Contains(t, string(output), "startup flags and environment validated")
			} else {
				require.Error(t, err)
				require.NoFileExists(t, marker)
			}
		})
	}
}
