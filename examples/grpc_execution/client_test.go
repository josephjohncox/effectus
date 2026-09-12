package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type exampleClient func(*testing.T, []string, string) (int, string, string)

func TestGRPCExampleClientProcess(t *testing.T) {
	if os.Getenv("EFFECTUS_GRPC_CLIENT_HELPER") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	require.NotEqual(t, -1, separator)
	os.Args = append([]string{"grpc_execution"}, os.Args[separator+1:]...)
	main()
}

func exampleCommand(t *testing.T, binary string, args, env []string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = env
	command.Dir = t.TempDir() // Both clients must find the shared checkout assets without cwd assumptions.
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	require.NoError(t, ctx.Err(), "client command did not terminate: %s", stderr.String())
	code := 0
	if err != nil {
		var exited *exec.ExitError
		require.True(t, errors.As(err, &exited), "client launch failed: %v", err)
		code = exited.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func exampleEnvironment(replacements map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(replacements))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range replacements {
		env = append(env, key+"="+value)
	}
	return env
}

func goExampleClient(t *testing.T, args []string, token string) (int, string, string) {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	args = append([]string{"-test.run=^TestGRPCExampleClientProcess$", "--"}, args...)
	return exampleCommand(t, binary, args, exampleEnvironment(map[string]string{"EFFECTUS_API_TOKEN": token, "EFFECTUS_GRPC_CLIENT_HELPER": "1"}))
}

func pythonExampleClient(t *testing.T) exampleClient {
	t.Helper()
	configured := os.Getenv("EFFECTUS_EXAMPLE_PYTHON")
	if configured == "" {
		t.Skip("Python gate not selected: set EFFECTUS_EXAMPLE_PYTHON to the interpreter with the pinned example requirements")
	}
	python, err := exec.LookPath(configured)
	require.NoError(t, err, "the explicitly selected Python interpreter must exist")
	python, err = filepath.Abs(python)
	require.NoError(t, err)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	generated := t.TempDir()
	// Generate from the current protos on every gate; never trust stale bindings.
	code, _, stderr := exampleCommand(t, python, []string{
		"-m", "grpc_tools.protoc", "-I" + root, "--python_out=" + generated, "--grpc_python_out=" + generated,
		filepath.Join(root, "effectus/v1/common.proto"), filepath.Join(root, "effectus/v1/execution.proto"),
	}, exampleEnvironment(map[string]string{"PYTHONNOUSERSITE": "1"}))
	require.Zero(t, code, stderr)
	return func(t *testing.T, args []string, token string) (int, string, string) {
		t.Helper()
		args = append([]string{filepath.Join(root, "examples/grpc_execution/client.py")}, args...)
		return exampleCommand(t, python, args, exampleEnvironment(map[string]string{
			"EFFECTUS_API_TOKEN": token, "PYTHONPATH": generated, "PYTHONNOUSERSITE": "1", "PYTHONDONTWRITEBYTECODE": "1",
		}))
	}
}

func requireClientFailure(t *testing.T, client exampleClient, args []string, token, category string) {
	t.Helper()
	code, stdout, stderr := client(t, args, token)
	require.Equal(t, 1, code, stderr)
	require.Empty(t, stdout)
	require.NotContains(t, stderr, exampleTestToken)
	if category != "" {
		require.Contains(t, strings.ToLower(strings.ReplaceAll(stderr, "_", "")), category)
	}
}

func exerciseAuthenticatedClients(t *testing.T, clients map[string]exampleClient) {
	t.Helper()
	for _, secure := range []bool{false, true} {
		name := "explicit-plaintext"
		if secure {
			name = "verified-TLS"
		}
		t.Run(name, func(t *testing.T) {
			config, caFile := exampleCertificate(t, true)
			if !secure {
				config = nil
			}
			address, executor := exampleService(t, config)
			args := []string{"--address=" + address}
			if secure {
				args = append(args, "--ca-file="+caFile)
			} else {
				args = append(args, "--allow-insecure")
			}
			var identity clientResult
			for language, client := range clients {
				t.Run(language, func(t *testing.T) {
					requireClientFailure(t, client, args, "wrong-test-token", "unauthenticated")
					requireClientFailure(t, client, args, "", "set effectusapitoken")
					for attempt := 0; attempt < 2; attempt++ {
						code, stdout, stderr := client(t, args, exampleTestToken)
						require.Zero(t, code, stderr)
						var result clientResult
						require.NoError(t, json.Unmarshal([]byte(stdout), &result))
						require.Equal(t, "EXECUTION_STATE_COMPLETED", result.State)
						require.True(t, result.DurablyAccepted)
						require.True(t, result.Completed)
						require.True(t, result.Success)
						require.NotEmpty(t, result.ExecutionID)
						require.NotEmpty(t, result.GenerationDigest)
						if identity.ExecutionID == "" {
							identity = result
						}
						require.Equal(t, identity, result, "both clients must replay the same typed identity")
						require.Equal(t, int64(1), executor.calls.Load())
					}
					conflict := append(slices.Clone(args), "--order-id=conflicting-order")
					requireClientFailure(t, client, conflict, exampleTestToken, "alreadyexists")
					require.Equal(t, int64(1), executor.calls.Load())
				})
			}
		})
	}
}

func exerciseTLSRejections(t *testing.T, client exampleClient, shortTimeout string) {
	t.Helper()
	config, caFile := exampleCertificate(t, true)
	address, executor := exampleService(t, config)
	_, otherCA := exampleCertificate(t, true)
	base := []string{"--address=" + address, "--timeout=" + shortTimeout}
	requireClientFailure(t, client, base, exampleTestToken, "") // System roots do not trust the test CA.
	requireClientFailure(t, client, append(slices.Clone(base), "--ca-file="+otherCA), exampleTestToken, "")
	requireClientFailure(t, client, append(slices.Clone(base), "--ca-file="+caFile, "--allow-insecure"), exampleTestToken, "choose tls")
	require.Zero(t, executor.calls.Load())
	wrongHost, wrongHostCA := exampleCertificate(t, false)
	wrongAddress, wrongExecutor := exampleService(t, wrongHost)
	requireClientFailure(t, client, []string{"--address=" + wrongAddress, "--ca-file=" + wrongHostCA, "--timeout=" + shortTimeout}, exampleTestToken, "")
	require.Zero(t, wrongExecutor.calls.Load())
	plainAddress, plainExecutor := exampleService(t, nil)
	requireClientFailure(t, client, []string{"--address=" + plainAddress, "--timeout=" + shortTimeout}, exampleTestToken, "")
	require.Zero(t, plainExecutor.calls.Load(), "TLS must not fall back to plaintext")
}

func TestDocumentedGoGRPCClient(t *testing.T) {
	exerciseAuthenticatedClients(t, map[string]exampleClient{"Go": goExampleClient})
	exerciseTLSRejections(t, goExampleClient, "250ms")
}

func TestDocumentedPythonAndGoGRPCClients(t *testing.T) {
	python := pythonExampleClient(t)
	for _, test := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"--unknown"}, 2}, {[]string{"position"}, 2}, {[]string{"--timeout=0"}, 1}, {[]string{"--timeout=301"}, 1}, {[]string{"--timeout=nan"}, 1}, {[]string{"--timeout=inf"}, 1}, {[]string{"--ca-file=missing", "--allow-insecure"}, 1}} {
		code, stdout, stderr := python(t, test.args, exampleTestToken)
		require.Equal(t, test.code, code, stderr)
		require.NotContains(t, stdout+stderr, exampleTestToken)
	}
	exerciseAuthenticatedClients(t, map[string]exampleClient{"Go": goExampleClient, "Python": python})
	exerciseTLSRejections(t, python, "0.25")
}

func TestGoClientHelpAndValidationDoNotExposeEnvironmentToken(t *testing.T) {
	t.Setenv("EFFECTUS_API_TOKEN", exampleTestToken)
	for _, test := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"--unknown"}, 2}, {[]string{"position"}, 2}, {[]string{"--timeout=0s"}, 1}, {[]string{"--timeout=6m"}, 1}, {[]string{"--ca-file=missing", "--allow-insecure"}, 1}} {
		var stdout, stderr bytes.Buffer
		require.Equal(t, test.code, runClientCLI(test.args, &stdout, &stderr), stderr.String())
		require.NotContains(t, stdout.String()+stderr.String(), exampleTestToken)
	}
}
