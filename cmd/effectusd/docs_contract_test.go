package main

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationGuideDaemonStartupContract(t *testing.T) {
	documentation, err := os.ReadFile("../../docs/INTEGRATION.md")
	require.NoError(t, err)
	require.Contains(t, string(documentation), "export DB_DSN=")
	require.Contains(t, string(documentation), "export EFFECTUS_API_TOKEN=")
	require.Contains(t, string(documentation), "EFFECTUS_POSTGRES_DSN=\"$DB_DSN\"")
	require.Contains(t, string(documentation), "EFFECTUS_API_TOKEN=\"$EFFECTUS_API_TOKEN\"")
	require.Contains(t, string(documentation), "go run ./cmd/effectusd --bundle")
}

func TestDocumentedCLIAndFlags(t *testing.T) {
	documentation, err := os.ReadFile("../../docs/COMMANDS.md")
	require.NoError(t, err)
	text := string(documentation)
	flags := []string{
		"bundle", "oci-ref", "oci-signature-verifier", "postgres-dsn", "database-migrations", "migrate-only",
		"http-addr", "grpc-addr", "grpc-tls-cert", "grpc-tls-key", "grpc-allow-insecure", "fact-source",
		"kafka-brokers", "kafka-topic", "kafka-consumer-group", "kafka-ack-contract",
	}
	require.NotEmpty(t, flags, "documentation contract must have daemon flags to check")
	for _, name := range flags {
		require.NotNil(t, flag.CommandLine.Lookup(name), "active daemon flag --%s is missing", name)
		require.Contains(t, text, "`--"+name+"`", "documented daemon flag --%s is missing", name)
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
