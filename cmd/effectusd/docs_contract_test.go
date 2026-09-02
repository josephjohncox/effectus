package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeConfigDocumentsOnlyVerifiedSourceBundleLoading(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "RUNTIME_CONFIG.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "effectus.source-bundle.v1")
	require.Contains(t, text, "digest-pinned")
	require.Contains(t, text, "--oci-signature-verifier")
	require.Contains(t, text, "bundle:\n  file:")
	require.NotContains(t, text, "extensions:\n")
	require.NotContains(t, text, "oras push")
}

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
	documented := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		documented[match[1]] = struct{}{}
		require.NotNilf(t, flag.CommandLine.Lookup(match[1]), "documented daemon flag --%s has no executable definition", match[1])
	}
	compatibility := map[string]string{
		"db-connection-idle-time": "2027-09-01", "db-connection-lifetime": "2027-09-01",
		"db-max-idle-connections": "2027-09-01", "db-max-open-connections": "2027-09-01",
		"kafka-delivery-ledger": "2027-09-01", "kafka-poison-audit": "2027-09-01", "migrate-only": "2027-09-01", "verb-oci-warmup": "2027-09-01",
	}
	require.Empty(t, expiredCompatibilityFlags(compatibility, time.Now().UTC()))
	for name := range compatibility {
		require.NotNilf(t, flag.CommandLine.Lookup(name), "compatibility flag --%s no longer exists; remove its expiry entry", name)
	}
	flag.CommandLine.VisitAll(func(item *flag.Flag) {
		if strings.Contains(item.Name, ".") {
			return
		}
		_, isDocumented := documented[item.Name]
		_, isCompatibility := compatibility[item.Name]
		require.Truef(t, isDocumented || isCompatibility, "executable daemon flag --%s is undocumented and has no dated compatibility entry", item.Name)
	})
	for _, stale := range []string{"--pprof-addr", "--saga-postgres-dsn", "delivery_ledger:", "poison_audit:"} {
		require.NotContains(t, section, stale)
	}
}

func TestCompatibilityFlagExpiryDetectsInvalidAndExpiredEntries(t *testing.T) {
	asOf := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	violations := expiredCompatibilityFlags(map[string]string{
		"active":  "2030-01-02",
		"expired": "2030-01-01",
		"invalid": "later",
	}, asOf)
	require.Equal(t, []string{"--expired expired on 2030-01-01", "--invalid has invalid expiry later"}, violations)
}

func expiredCompatibilityFlags(flags map[string]string, asOf time.Time) []string {
	var violations []string
	for name, rawDeadline := range flags {
		deadline, err := time.Parse("2006-01-02", rawDeadline)
		if err != nil {
			violations = append(violations, "--"+name+" has invalid expiry "+rawDeadline)
			continue
		}
		if !deadline.After(asOf) {
			violations = append(violations, "--"+name+" expired on "+rawDeadline)
		}
	}
	sort.Strings(violations)
	return violations
}
