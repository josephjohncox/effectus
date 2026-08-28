package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentedDaemonFlagsExist(t *testing.T) {
	registerCustomFlags()
	path := filepath.Join("..", "..", "docs", "COMMANDS.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	start := strings.Index(text, "## effectusd - Runtime Daemon")
	require.NotEqual(t, -1, start)
	section := text[start:]
	if end := strings.Index(section, "### Examples"); end >= 0 {
		section = section[:end]
	}
	matches := regexp.MustCompile(`--([a-z][a-z0-9-]+)`).FindAllStringSubmatch(section, -1)
	require.NotEmpty(t, matches)
	for _, match := range matches {
		require.NotNilf(t, flag.CommandLine.Lookup(match[1]), "documented daemon flag --%s has no executable definition", match[1])
	}
	for _, stale := range []string{"--pprof-addr", "--saga-postgres-dsn", "delivery_ledger:", "poison_audit:"} {
		require.NotContains(t, section, stale)
	}
}
