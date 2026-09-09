package schema

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func installFakeBuf(t *testing.T, body string) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX command fixture")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "buf"), []byte("#!/bin/sh\nset -eu\n"+body), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func configureBufOutput(t *testing.T, root, output string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "buf.gen.yaml"), []byte("version: v2\nplugins:\n  - local: protoc-gen-go\n    out: "+output+"\n"), 0644))
}

func TestBufCommandsUseConfiguredOutputsAndOwnConcurrentSnapshots(t *testing.T) {
	installFakeBuf(t, "case \"$1\" in\ngenerate) mkdir -p custom/generated; printf 'package generated\\n' > custom/generated/example.pb.go;;\nbreaking|lint) :;;\n*) exit 9;;\nesac\n")
	root := t.TempDir()
	configureBufOutput(t, root, "custom/generated")
	b, err := NewBufIntegration(root)
	require.NoError(t, err)
	var wg sync.WaitGroup
	failures := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			if err := b.RegisterFactSchema(t.Context(), &FactSchema{Name: fmt.Sprintf("record_%d", i), Schema: map[string]any{"id": "string"}}); err != nil {
				failures <- err
			}
		}(i)
		go func() {
			defer wg.Done()
			result, err := b.GenerateCode(t.Context())
			if err != nil {
				failures <- err
				return
			}
			if !result.Success || len(result.GeneratedFiles) != 1 || result.GeneratedFiles[0] != filepath.FromSlash("custom/generated/example.pb.go") {
				failures <- fmt.Errorf("unexpected generation result: %+v", result)
			}
		}()
		go func() {
			defer wg.Done()
			for _, value := range b.ListFactSchemas() {
				value.Schema["id"] = "caller mutation"
			}
			result, err := b.ValidateSchemas(t.Context())
			if err != nil {
				failures <- err
			} else if !result.Valid {
				failures <- errors.New("unexpected invalid fixture")
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Len(t, b.ListFactSchemas(), 16)
	for _, value := range b.ListFactSchemas() {
		require.Equal(t, "string", value.Schema["id"])
	}
	result, err := b.GenerateCode(t.Context())
	require.NoError(t, err)
	require.Equal(t, 16, result.Metadata["schema_count"])
	_, err = os.Stat(filepath.Join(root, "effectus-go"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBufRejectsInvalidGenerationConfigurationBeforeCommand(t *testing.T) {
	installFakeBuf(t, ": > command-ran\n")
	root, outside := t.TempDir(), t.TempDir()
	b, err := NewBufIntegration(root)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	for _, output := range []string{"../outside", outside, "escape", "escape/subdir", "''"} {
		configureBufOutput(t, root, output)
		_, err := b.GenerateCode(t.Context())
		require.Error(t, err)
	}
	_, err = os.Stat(filepath.Join(root, "command-ran"))
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestBufReportsBreakingAndLintFailures(t *testing.T) {
	for _, failure := range []string{"breaking", "lint"} {
		t.Run(failure, func(t *testing.T) {
			installFakeBuf(t, fmt.Sprintf("printf '%%s\\n' \"$1\" >> calls\nif [ \"$1\" = %s ]; then echo diagnostic; exit 1; fi\n", failure))
			root := t.TempDir()
			b, err := NewBufIntegration(root)
			require.NoError(t, err)
			result, err := b.ValidateSchemas(t.Context())
			require.Error(t, err)
			require.False(t, result.Valid)
			require.NotEmpty(t, result.Errors)
			data, err := os.ReadFile(filepath.Join(root, "calls"))
			require.NoError(t, err)
			require.Equal(t, "breaking\nlint\n", string(data))
		})
	}
}

func TestBufCancellationStopsCommandSequence(t *testing.T) {
	for _, operation := range []string{"generate", "validate"} {
		t.Run(operation, func(t *testing.T) {
			installFakeBuf(t, "if [ \"$1\" = lint ]; then : > unexpected-lint; exit 0; fi\n: > started\nexec sleep 60\n")
			root := t.TempDir()
			configureBufOutput(t, root, "custom/generated")
			b, err := NewBufIntegration(root)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				var err error
				if operation == "generate" {
					_, err = b.GenerateCode(ctx)
				} else {
					_, err = b.ValidateSchemas(ctx)
				}
				done <- err
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("Buf command did not stop")
				}
			})
			require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(root, "started")); return err == nil }, 2*time.Second, time.Millisecond)
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation did not join command")
			}
			_, err = os.Stat(filepath.Join(root, "unexpected-lint"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
