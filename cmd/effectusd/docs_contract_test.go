package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
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
	actual := make(map[string]bool)
	flag.CommandLine.VisitAll(func(option *flag.Flag) {
		if !strings.HasPrefix(option.Name, "test.") {
			actual[option.Name] = true
			require.Contains(t, text, "`--"+option.Name+"`", "documented daemon flag is missing")
		}
	})
	require.NotEmpty(t, actual)
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, helpFlag := range []string{"--help", "-h"} {
		t.Run(helpFlag, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestEffectusdHelperProcess$", "--", helpFlag)
			command.Dir = t.TempDir()
			command.Env = append(os.Environ(), "EFFECTUSD_TEST_PROCESS=1", "EFFECTUS_POSTGRES_DSN=", "EFFECTUS_API_TOKEN=")
			command.WaitDelay = 2 * time.Second
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), string(output))
			require.NoError(t, err, string(output))
			displayed := make(map[string]bool)
			for _, match := range regexp.MustCompile(`(?m)^  -([^\s]+)`).FindAllStringSubmatch(string(output), -1) {
				if !strings.HasPrefix(match[1], "test.") {
					displayed[match[1]] = true
				}
			}
			require.Equal(t, actual, displayed, "daemon help must expose the documented registered flags")
		})
	}
	entries, err := os.ReadDir("testdata/docs")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "negative documentation fixtures must not be empty")
	for _, entry := range entries {
		data, readErr := os.ReadFile("testdata/docs/" + entry.Name())
		require.NoError(t, readErr)
		stale := strings.TrimSpace(string(data))
		require.NotEmpty(t, stale, "negative fixture %s is empty", entry.Name())
		require.NotContains(t, text, stale, "stale daemon surface %q remains documented", stale)
	}
}
