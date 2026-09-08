package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestEffectuscHelperProcess(t *testing.T) {
	if os.Getenv("EFFECTUSC_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"effectusc"}, os.Args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing process argument separator")
}

func runCompilerProcess(t *testing.T, args ...string) (int, string) {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestEffectuscHelperProcess$", "--"}, args...)...)
	command.Env = append(os.Environ(), "EFFECTUSC_TEST_PROCESS=1")
	output, err := command.CombinedOutput()
	if err == nil {
		return 0, string(output)
	}
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	return exit.ExitCode(), string(output)
}

func compilerSourceFixture(t *testing.T) string {
	t.Helper()
	source, err := bundle.New(bundle.Spec{Name: "noop", Version: "1", Environment: ir.Environment{}, Sources: []bundle.Source{{Path: "noop.eff", Content: `rule "noop" priority 1 { when { true } then {} }`}}})
	require.NoError(t, err)
	data, err := source.Bytes()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "source.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestCompilerCLIHelpFlagsAndExitCodes(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"check", "--help"}, {"compile", "--help"}, {"inspect", "--help"}} {
		code, text := runCompilerProcess(t, args...)
		require.Equal(t, 0, code, "%v: %s", args, text)
	}
	for _, test := range []struct {
		args []string
		code int
	}{
		{nil, 2}, {[]string{"missing"}, 2}, {[]string{"check", "--output", "ignored"}, 2},
		{[]string{"check", "extra"}, 1}, {[]string{"inspect", "extra"}, 1}, {[]string{"compile"}, 1},
	} {
		code, text := runCompilerProcess(t, test.args...)
		require.Equal(t, test.code, code, "%v: %s", test.args, text)
	}
}

func TestCompilerCLIProducesDeterministicArtifactWithoutDamagingInput(t *testing.T) {
	source := compilerSourceFixture(t)
	original, err := os.ReadFile(source)
	require.NoError(t, err)
	output := filepath.Join(filepath.Dir(source), "checked.pb")
	code, text := runCompilerProcess(t, "compile", "--bundle", source, "--output", output)
	require.Equal(t, 0, code, text)
	first, err := os.ReadFile(output)
	require.NoError(t, err)
	_, err = ir.Parse(first, ir.Environment{}, ir.Limits{})
	require.NoError(t, err)
	code, text = runCompilerProcess(t, "compile", "--bundle", source, "--output", output)
	require.Equal(t, 0, code, text)
	second, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, first, second)
	code, text = runCompilerProcess(t, "compile", "--bundle", source, "--output", source)
	require.Equal(t, 1, code, text)
	after, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestArtifactOutputRejectsSourceAliases(t *testing.T) {
	for _, kind := range []string{"same", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			source := compilerSourceFixture(t)
			before, err := os.ReadFile(source)
			require.NoError(t, err)
			output := source
			if kind != "same" {
				output = filepath.Join(filepath.Dir(source), "alias")
				if kind == "symlink" {
					err = os.Symlink(source, output)
				} else {
					err = os.Link(source, output)
				}
				require.NoError(t, err)
			}
			require.Error(t, writeCheckedArtifact(source, output, []byte("artifact")))
			after, err := os.ReadFile(source)
			require.NoError(t, err)
			require.True(t, bytes.Equal(before, after))
		})
	}
}

type failingArtifactFile struct {
	*os.File
	operation string
	failure   error
}

func (f failingArtifactFile) Write(data []byte) (int, error) {
	if f.operation == "write" {
		return 0, f.failure
	}
	if f.operation == "short" {
		return 0, nil
	}
	return f.File.Write(data)
}
func (f failingArtifactFile) Sync() error {
	if f.operation == "sync" {
		return f.failure
	}
	return f.File.Sync()
}
func (f failingArtifactFile) Close() error {
	err := f.File.Close()
	if f.operation == "close" {
		return f.failure
	}
	return err
}

func TestAtomicArtifactFailureKeepsPreviousOutputAndCleansTemporaryFiles(t *testing.T) {
	for _, operation := range []string{"create", "write", "short", "sync", "close", "rename"} {
		t.Run(operation, func(t *testing.T) {
			source := compilerSourceFixture(t)
			dir := filepath.Dir(source)
			output := filepath.Join(dir, "output.pb")
			require.NoError(t, os.WriteFile(output, []byte("old artifact"), 0o600))
			failure := errors.New("injected artifact failure")
			ops := artifactFileOps{
				create: func(dir, pattern string) (artifactFile, error) {
					if operation == "create" {
						return nil, failure
					}
					file, err := os.CreateTemp(dir, pattern)
					if err != nil {
						return nil, err
					}
					return failingArtifactFile{file, operation, failure}, nil
				},
				rename: func(old, new string) error {
					if operation == "rename" {
						return failure
					}
					return os.Rename(old, new)
				},
			}
			err := writeCheckedArtifactWithOps(source, output, []byte("new artifact"), ops)
			if operation == "short" {
				require.ErrorIs(t, err, io.ErrShortWrite)
			} else {
				require.ErrorIs(t, err, failure)
			}
			after, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Equal(t, "old artifact", string(after))
			temps, err := filepath.Glob(filepath.Join(dir, ".effectusc-*.tmp"))
			require.NoError(t, err)
			require.Empty(t, temps)
		})
	}
}
