package guardrails

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Shadow every external install command. Run the actual CI shell program,
// but never download a jar, write its absolute paths, or invoke sudo.
func TestDocumentedTLCInstallPinAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the CI job uses Bash on Linux")
	}
	bash, err := exec.LookPath("bash")
	require.NoError(t, err, "the selected CI install contract requires Bash")
	data, err := os.ReadFile(filepath.Join("../..", ".github/workflows/ci.yml"))
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &workflow))
	var scripts []string
	for _, step := range workflow.Jobs["formal"].Steps {
		if step.Name == "Install pinned TLC" {
			scripts = append(scripts, step.Run)
		}
	}
	require.Len(t, scripts, 1)
	require.NotEmpty(t, scripts[0])
	for _, tc := range []struct {
		name         string
		curlExit     string
		checksumExit string
		wantExit     int
	}{
		{name: "verified", curlExit: "0", checksumExit: "0"},
		{name: "download_failure", curlExit: "22", checksumExit: "0", wantExit: 22},
		{name: "checksum_failure", curlExit: "0", checksumExit: "1", wantExit: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			temporary := t.TempDir()
			for name, script := range map[string]string{
				"curl": `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TLC_TRACE/curl"
exit "$TLC_CURL_EXIT"
`,
				"sha256sum": `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TLC_TRACE/checksum-args"
cat > "$TLC_TRACE/checksum-input"
exit "$TLC_CHECKSUM_EXIT"
`,
				"sudo": `#!/bin/sh
set -eu
printf '%s\n' "$@" >> "$TLC_TRACE/install"
case "$1" in
  tee) cat > "$TLC_TRACE/launcher" ;;
  chmod) ;;
  *) exit 99 ;;
esac
`,
			} {
				require.NoError(t, os.WriteFile(filepath.Join(temporary, name), []byte(script), 0o700))
			}
			script := filepath.Join(temporary, "step.sh")
			require.NoError(t, os.WriteFile(script, []byte(scripts[0]), 0o600))
			t.Setenv("PATH", temporary+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TLC_TRACE", temporary)
			t.Setenv("TLC_CURL_EXIT", tc.curlExit)
			t.Setenv("TLC_CHECKSUM_EXIT", tc.checksumExit)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-e", "-o", "pipefail", script)
			command.WaitDelay = 2 * time.Second
			output, err := command.CombinedOutput()
			if tc.wantExit == 0 {
				require.NoError(t, err, string(output))
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, string(output))
				require.Equal(t, tc.wantExit, exit.ExitCode())
			}
			readTrace := func(name string) string {
				content, err := os.ReadFile(filepath.Join(temporary, name))
				require.NoError(t, err)
				return string(content)
			}
			require.Equal(t, "--fail\n--location\n--output\n/tmp/tla2tools.jar\nhttps://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar\n", readTrace("curl"))
			if tc.curlExit != "0" {
				require.NoFileExists(t, filepath.Join(temporary, "checksum-args"))
			} else {
				require.Equal(t, "--check\n", readTrace("checksum-args"))
				require.Equal(t, "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88  /tmp/tla2tools.jar\n", readTrace("checksum-input"))
			}
			if tc.wantExit != 0 {
				require.NoFileExists(t, filepath.Join(temporary, "install"))
				require.NoFileExists(t, filepath.Join(temporary, "launcher"))
			} else {
				require.Equal(t, "tee\n/usr/local/bin/tlc\nchmod\n0755\n/usr/local/bin/tlc\n", readTrace("install"))
				require.Equal(t, "#!/bin/sh\nexec java -cp /tmp/tla2tools.jar tlc2.TLC \"$@\"\n", readTrace("launcher"))
			}
		})
	}
}
