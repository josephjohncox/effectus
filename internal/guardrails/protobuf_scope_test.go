package guardrails

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Use the real Buf configuration and executable, without resolving imports.
// Local tool installations must not become repository protobuf inputs.
func TestDocumentedBufInputScope(t *testing.T) {
	buf, err := exec.LookPath("buf")
	if err != nil {
		if os.Getenv("EFFECTUS_REQUIRE_PROTO") == "1" {
			t.Fatal("the selected protobuf scope gate requires Buf on PATH")
		}
		t.Skip("Buf is required for the executable protobuf scope contract")
	}
	config, err := os.ReadFile(filepath.Join("../..", "buf.yaml"))
	require.NoError(t, err)
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "buf.yaml"), config, 0o600))
	for _, path := range []string{"effectus/v1/included.proto", "runtime/included.proto", ".tools/python/site-packages/google/protobuf/local_tool.proto"} {
		target := filepath.Join(workspace, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, []byte("syntax = \"proto3\";\n"), 0o600))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, buf, "ls-files")
	command.Dir = workspace
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Equal(t, []string{"effectus/v1/included.proto", "runtime/included.proto"}, strings.Fields(string(output)), "tool-cache exclusion must retain both owned protobuf roots")
}
