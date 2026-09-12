package schema

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// bufCommandOutput bounds diagnostics while continuing to drain both pipes.
type bufCommandOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (w *bufCommandOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := (256 << 10) - w.buffer.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		_, _ = w.buffer.Write(p[:remaining])
	}
	return len(p), nil
}

func (b *BufIntegration) runBuf(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "buf", args...)
	cmd.Dir = b.workspaceRoot
	cmd.WaitDelay = 2 * time.Second
	output := &bufCommandOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return output.buffer.String(), errors.Join(ctx.Err(), err)
}

func (b *BufIntegration) configuredOutputs() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(b.workspaceRoot, "buf.gen.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read generation configuration: %w", err)
	}
	var config struct {
		Version string `yaml:"version"`
		Plugins []struct {
			Out string `yaml:"out"`
		} `yaml:"plugins"`
	}
	if err = yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.Version != "v1" && config.Version != "v2" {
		return nil, fmt.Errorf("generation configuration version must be v1 or v2")
	}
	if len(config.Plugins) == 0 {
		return nil, fmt.Errorf("generation configuration requires plugins with output paths")
	}
	paths := map[string]bool{}
	for _, plugin := range config.Plugins {
		if strings.TrimSpace(plugin.Out) == "" {
			return nil, fmt.Errorf("generation plugin output path is required")
		}
		path := plugin.Out
		if !filepath.IsAbs(path) {
			path = filepath.Join(b.workspaceRoot, path)
		}
		rel, err := filepath.Rel(b.workspaceRoot, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("generation output must remain within the workspace")
		}
		paths[rel] = true
	}
	root, err := os.OpenRoot(b.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	result := make([]string, 0, len(paths))
	for path := range paths {
		info, err := root.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("generation output must be a directory without a symlink alias")
		}
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func (b *BufIntegration) generatedGoFiles(ctx context.Context, outputs []string) ([]string, error) {
	root, err := os.OpenRoot(b.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := map[string]bool{}
	for _, output := range outputs {
		err = fs.WalkDir(root.FS(), filepath.ToSlash(output), func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if errors.Is(walkErr, os.ErrNotExist) && path == filepath.ToSlash(output) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && entry.Type().IsRegular() && strings.HasSuffix(path, ".pb.go") {
				files[filepath.FromSlash(path)] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(files))
	for file := range files {
		result = append(result, file)
	}
	sort.Strings(result)
	return result, nil
}

func (b *BufIntegration) generateCode(ctx context.Context) (*CodeGenerationResult, error) {
	if err := b.checkContext(ctx); err != nil {
		return nil, err
	}
	b.generationMutex.Lock()
	defer b.generationMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()
	result := &CodeGenerationResult{Metadata: make(map[string]interface{})}
	outputs, err := b.configuredOutputs()
	if err != nil {
		return result, err
	}
	output, err := b.runBuf(ctx, "generate")
	result.Duration = time.Since(start)
	if err != nil {
		result.Errors = []string{fmt.Sprintf("buf generate failed: %v", err), output}
		return result, err
	}
	result.GeneratedFiles, err = b.generatedGoFiles(ctx, outputs)
	if err != nil {
		result.Errors = []string{fmt.Sprintf("enumerate generated files: %v", err)}
		return result, err
	}
	b.lastGeneration = time.Now()
	result.Success = true
	result.Metadata["generation_timestamp"] = b.lastGeneration
	result.Metadata["schema_count"] = len(b.verbRegistry.schemas) + len(b.factRegistry.schemas)
	return result, nil
}

func (b *BufIntegration) validateSchemas(ctx context.Context) (*SchemaValidationResult, error) {
	if err := b.checkContext(ctx); err != nil {
		return nil, err
	}
	b.generationMutex.Lock()
	defer b.generationMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &SchemaValidationResult{Valid: true}
	output, breakingErr := b.runBuf(ctx, "breaking", "--against", ".git#branch=main")
	if breakingErr != nil {
		result.Valid = false
		result.BreakingChanges = strings.FieldsFunc(output, func(r rune) bool { return r == '\n' || r == '\r' })
		result.Errors = append(result.Errors, fmt.Sprintf("buf breaking failed: %v", breakingErr))
		result.Suggestions = []string{"Review field numbers and schema compatibility before accepting changes."}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		result.Valid = false
		return result, errors.Join(ctxErr, breakingErr)
	}
	output, lintErr := b.runBuf(ctx, "lint")
	if lintErr != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("buf lint failed: %v", lintErr), output)
	}
	return result, errors.Join(breakingErr, lintErr)
}
