package guardrails

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Execute the real Just recipe with an isolated command recorder. This catches
// argument, environment, and sequencing errors without touching a database.
func TestDocumentedIntegrationRecipeExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Just recipe uses a POSIX shell")
	}
	just, err := exec.LookPath("just")
	if err != nil {
		t.Skip("just is required for executable recipe contracts")
	}
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	bin := t.TempDir()
	fakeGo := `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$RECIPE_TRACE/$1.args"
case "$1" in
  run)
    printf '%s' "$EFFECTUS_POSTGRES_DSN" > "$RECIPE_TRACE/run.dsn"
    exit "${RECIPE_MIGRATION_EXIT:-0}"
    ;;
  test)
    printf '%s' "$POSTGRES_DSN" > "$RECIPE_TRACE/test.dsn"
    ;;
  *) exit 99 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(fakeGo), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, test := range []struct {
		name      string
		dsn       string
		migration string
		wantError bool
	}{{"missing-dsn", "", "0", true}, {"explicit-dsn", `postgres://user:a'"$b@localhost/test`, "0", false}, {"migration-fails", "postgres://localhost/test", "7", true}} {
		t.Run(test.name, func(t *testing.T) {
			trace := t.TempDir()
			t.Setenv("DB_DSN", test.dsn)
			t.Setenv("RECIPE_TRACE", trace)
			t.Setenv("RECIPE_MIGRATION_EXIT", test.migration)
			cmd := exec.Command(just, "--justfile", filepath.Join(root, "justfile"), "test-integration")
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err, string(output))
			}
			if test.dsn == "" {
				require.Contains(t, string(output), "DB_DSN is required")
				entries, err := os.ReadDir(trace)
				require.NoError(t, err)
				require.Empty(t, entries, "missing DSN must prevent all Go commands")
				return
			}
			require.NotContains(t, string(output), test.dsn, "recipe output must not print database credentials")
			args, err := os.ReadFile(filepath.Join(trace, "run.args"))
			require.NoError(t, err)
			require.Equal(t, []string{"run", "./cmd/effectusd", "--mode=migrate", "--database-migrations=apply"}, strings.Fields(string(args)))
			dsn, err := os.ReadFile(filepath.Join(trace, "run.dsn"))
			require.NoError(t, err)
			require.Equal(t, test.dsn, string(dsn))
			args, err = os.ReadFile(filepath.Join(trace, "test.args"))
			if test.migration != "0" {
				require.ErrorIs(t, err, os.ErrNotExist, "migration failure must prevent integration tests")
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"test", "-p", "1", "-tags=integration", "-timeout=90s", "./runtime/...", "./schema", "./cmd/effectusd"}, strings.Fields(string(args)))
			dsn, err = os.ReadFile(filepath.Join(trace, "test.dsn"))
			require.NoError(t, err)
			require.Equal(t, test.dsn, string(dsn))
		})
	}
}
