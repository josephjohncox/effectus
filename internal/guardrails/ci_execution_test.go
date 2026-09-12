package guardrails

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Execute the checked-in CI migration command with a fake Go recorder.
// This checks shell arguments and environment without opening a database.
func TestDocumentedCIMigrationCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the CI job uses Bash on Linux")
	}
	bash, err := exec.LookPath("bash")
	require.NoError(t, err, "the selected CI command contract requires Bash")
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &workflow))
	job, ok := workflow.Jobs["durable-postgres"]
	require.True(t, ok, "the durable PostgreSQL CI gate must exist")
	matches := 0
	for _, step := range job.Steps {
		if step.Name != "Apply durable migrations once" {
			continue
		}
		matches++
		require.NotEmpty(t, step.Run)
		dsn := step.Env["EFFECTUS_POSTGRES_DSN"]
		require.NotEmpty(t, dsn, "CI must explicitly select its fixture database")
		temporary := t.TempDir()
		fakeGo := `#!/bin/sh
set -eu
umask 077
printf '%s\n' "$@" > "$CI_TRACE/args"
printf '%s' "$EFFECTUS_POSTGRES_DSN" > "$CI_TRACE/dsn"
`
		require.NoError(t, os.WriteFile(filepath.Join(temporary, "go"), []byte(fakeGo), 0o700))
		script := filepath.Join(temporary, "step.sh")
		require.NoError(t, os.WriteFile(script, []byte(step.Run+"\n"), 0o600))
		t.Setenv("PATH", temporary+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("CI_TRACE", temporary)
		for key, value := range step.Env {
			t.Setenv(key, value)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-e", "-o", "pipefail", script)
		command.Dir = root
		command.WaitDelay = 2 * time.Second
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		require.NotContains(t, string(output), dsn, "the command must not print its database credentials")
		arguments, err := os.ReadFile(filepath.Join(temporary, "args"))
		require.NoError(t, err)
		require.Equal(t, []string{"run", "./cmd/effectusd", "--mode=migrate", "--database-migrations=apply"}, strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n"))
		actualDSN, err := os.ReadFile(filepath.Join(temporary, "dsn"))
		require.NoError(t, err)
		require.Equal(t, dsn, string(actualDSN))
	}
	require.Equal(t, 1, matches, "CI must contain exactly one named migration step")
}
