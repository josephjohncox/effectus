package main

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Compare documentation with the flags actually registered by the executable,
// not with a second hand-maintained list of expected names and defaults.
func TestDocumentedDaemonFlagNamesAndDefaults(t *testing.T) {
	data, err := os.ReadFile("../../docs/COMMANDS.md")
	require.NoError(t, err)
	pattern := regexp.MustCompile("(?m)^\\| `--([^`]+)` \\| `([^`]+)` \\|")
	documented := make(map[string]string)
	for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
		_, duplicate := documented[match[1]]
		require.False(t, duplicate, "duplicate documented flag %s", match[1])
		value := match[2]
		if value == `""` {
			value = ""
		}
		documented[match[1]] = value
	}
	actual := make(map[string]string)
	flag.CommandLine.VisitAll(func(option *flag.Flag) {
		if !strings.HasPrefix(option.Name, "test.") {
			actual[option.Name] = option.DefValue
		}
	})
	require.NotEmpty(t, actual)
	require.Equal(t, actual, documented, "update the documented flag inventory only after reviewing executable behavior")
}
