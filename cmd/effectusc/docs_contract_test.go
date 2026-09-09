package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDocumentedCLIAndFlags(t *testing.T) {
	documentation, err := os.ReadFile("../../docs/COMMANDS.md")
	require.NoError(t, err)
	text := string(documentation)
	compilerDoc, _, found := strings.Cut(text, "## `effectusd`")
	require.True(t, found)
	rows := regexp.MustCompile("(?m)^\\| `([^`]+)` \\| ([^|]+) \\|").FindAllStringSubmatch(compilerDoc, -1)
	commands := defineCommands()
	require.Len(t, rows, len(commands), "document each command once")
	seen := make(map[string]bool)
	for _, row := range rows {
		name := row[1]
		command, exists := commands[name]
		require.True(t, exists, "unknown documented command %s", name)
		require.False(t, seen[name], "duplicate command %s", name)
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			required := make(map[string]bool)
			for _, match := range regexp.MustCompile("`--([^`]+)`").FindAllStringSubmatch(row[2], -1) {
				require.False(t, required[match[1]], "duplicate required flag")
				required[match[1]] = true
			}
			actual := make(map[string]bool)
			command.flags.VisitAll(func(item *flag.Flag) {
				actual[item.Name] = true
				require.Empty(t, item.DefValue, "review the required-flag contract if a default changes")
			})
			require.NotEmpty(t, required)
			require.Equal(t, actual, required)
			code, help := runDocumentedCompiler(t, name, "--help")
			require.Zero(t, code, help)
			helpFlags := make(map[string]bool)
			for _, match := range regexp.MustCompile(`(?m)^  -([^\s]+)`).FindAllStringSubmatch(help, -1) {
				helpFlags[match[1]] = true
			}
			require.Equal(t, required, helpFlags, "per-command help must match the documentation")

			values := map[string]string{"bundle": compilerSourceFixture(t), "output": filepath.Join(t.TempDir(), "checked.pb")}
			argsFor := func(omitted string) []string {
				args := []string{name}
				for key := range required {
					if key != omitted {
						value, known := values[key]
						require.True(t, known, "add a valid fixture for --%s", key)
						args = append(args, "--"+key, value)
					}
				}
				return args
			}
			for omitted := range required {
				code, output := runDocumentedCompiler(t, argsFor(omitted)...)
				require.Equal(t, 1, code, output)
				require.Contains(t, output, "--"+omitted+" is required")
			}
			code, output := runDocumentedCompiler(t, argsFor("")...)
			require.Zero(t, code, output)
		})
	}
	for _, stale := range readNegativeFixtures(t, "testdata/docs") {
		require.NotContains(t, text, stale, "stale CLI surface %q remains documented", stale)
	}
}

func runDocumentedCompiler(t *testing.T, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	binary, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(ctx, binary, append([]string{"-test.run=^TestEffectuscHelperProcess$", "--"}, args...)...)
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "EFFECTUSC_TEST_PROCESS=1")
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), string(output))
	if err == nil {
		return 0, string(output)
	}
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(output))
	return exit.ExitCode(), string(output)
}

func readNegativeFixtures(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "negative documentation fixtures must not be empty")
	var values []string
	for _, entry := range entries {
		data, readErr := os.ReadFile(directory + "/" + entry.Name())
		require.NoError(t, readErr)
		value := strings.TrimSpace(string(data))
		require.NotEmpty(t, value, "negative fixture %s is empty", entry.Name())
		values = append(values, value)
	}
	return values
}
