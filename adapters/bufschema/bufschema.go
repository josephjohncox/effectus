package bufschema

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go/adapters"
)

// Config controls Buf schema loading.
type Config struct {
	Module        string   `json:"module" yaml:"module"`
	Ref           string   `json:"ref" yaml:"ref"`
	Dir           string   `json:"dir" yaml:"dir"`
	ExportDir     string   `json:"export_dir" yaml:"export_dir"`
	SchemaDir     string   `json:"schema_dir" yaml:"schema_dir"`
	SchemaFiles   []string `json:"schema_files" yaml:"schema_files"`
	IncludePkgs   []string `json:"include_packages" yaml:"include_packages"`
	ExcludePkgs   []string `json:"exclude_packages" yaml:"exclude_packages"`
	SchemaName    string   `json:"schema_name" yaml:"schema_name"`
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
}

// Provider loads schemas from a Buf registry or local module.
type Provider struct {
	config    *Config
	baseDir   string
	exportDir string
	cleanup   bool
}

// Factory creates Buf schema providers.
type Factory struct{}

func (f *Factory) ValidateConfig(config adapters.SchemaSourceConfig) error {
	cfg, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if cfg.Module == "" && cfg.Dir == "" && cfg.SchemaDir == "" && len(cfg.SchemaFiles) == 0 {
		return fmt.Errorf("module, dir, schema_dir, or schema_files is required")
	}
	return nil
}

func (f *Factory) Create(config adapters.SchemaSourceConfig) (adapters.SchemaProvider, error) {
	cfg, err := decodeConfig(config)
	if err != nil {
		return nil, err
	}
	provider := &Provider{config: cfg, baseDir: config.BaseDir}
	return provider, nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"module": {
				Type:        "string",
				Description: "buf module reference (e.g., buf.build/acme/facts)",
			},
			"ref": {
				Type:        "string",
				Description: "optional module reference tag/commit",
			},
			"dir": {
				Type:        "string",
				Description: "local buf module directory",
			},
			"export_dir": {
				Type:        "string",
				Description: "directory to write buf export output",
			},
			"schema_dir": {
				Type:        "string",
				Description: "directory with JSON schema outputs (relative to export_dir when set)",
			},
			"schema_files": {
				Type:        "array",
				Description: "explicit JSON schema files to load",
			},
			"include_packages": {
				Type:        "array",
				Description: "only include proto packages with these prefixes",
			},
			"exclude_packages": {
				Type:        "array",
				Description: "exclude proto packages with these prefixes",
			},
			"schema_name": {
				Type:        "string",
				Description: "override schema name when loading a single file",
			},
			"schema_version": {
				Type:        "string",
				Description: "schema version label",
			},
		},
	}
}

func (p *Provider) LoadSchemas(ctx context.Context) ([]adapters.SchemaDefinition, error) {
	if p == nil || p.config == nil {
		return nil, fmt.Errorf("buf schema provider not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if err := p.prepareExport(ctx); err != nil {
		return nil, err
	}

	schemaPaths, err := p.resolveSchemaFiles()
	if err != nil {
		return nil, err
	}
	if len(schemaPaths) == 0 {
		return p.generateSchemasFromProto(ctx)
	}

	defs := make([]adapters.SchemaDefinition, 0, len(schemaPaths))
	for _, path := range schemaPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading schema file %s: %w", path, err)
		}

		name := p.schemaNameFor(path, len(schemaPaths))
		defs = append(defs, adapters.SchemaDefinition{
			Name:    name,
			Version: p.config.SchemaVersion,
			Format:  adapters.SchemaFormatAuto,
			Data:    data,
			Source:  p.config.Module,
		})
	}

	return defs, nil
}

func (p *Provider) Close() error {
	if p.cleanup && p.exportDir != "" {
		return os.RemoveAll(p.exportDir)
	}
	return nil
}

func (p *Provider) prepareExport(ctx context.Context) error {
	exportNeeded := p.config.Module != "" || p.config.Dir != ""
	if !exportNeeded {
		return nil
	}

	exportDir := p.config.ExportDir
	if exportDir == "" {
		tmp, err := os.MkdirTemp("", "effectus-buf-export-*")
		if err != nil {
			return fmt.Errorf("creating export dir: %w", err)
		}
		exportDir = tmp
		p.cleanup = true
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("creating export dir: %w", err)
	}
	p.exportDir = exportDir

	args := []string{"export"}
	module := p.config.Module
	if module != "" {
		if p.config.Ref != "" && !strings.Contains(module, ":") && !strings.Contains(module, "@") {
			module = fmt.Sprintf("%s:%s", module, p.config.Ref)
		}
		args = append(args, module)
	}
	args = append(args, "-o", exportDir)

	cmd := exec.CommandContext(ctx, "buf", args...)
	if p.config.Dir != "" {
		cmd.Dir = p.config.Dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("buf export failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *Provider) resolveSchemaFiles() ([]string, error) {
	if len(p.config.SchemaFiles) > 0 {
		paths := make([]string, 0, len(p.config.SchemaFiles))
		for _, path := range p.config.SchemaFiles {
			paths = append(paths, p.resolvePath(path))
		}
		return paths, nil
	}

	if p.config.SchemaDir != "" {
		dir := p.resolvePath(p.config.SchemaDir)
		return collectSchemaFiles(dir, true)
	}

	searchDir := p.exportDir
	if searchDir == "" {
		searchDir = p.baseDir
	}
	if searchDir == "" {
		searchDir = "."
	}
	return collectSchemaFiles(searchDir, false)
}

func (p *Provider) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if p.exportDir != "" {
		candidate := filepath.Join(p.exportDir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p.baseDir != "" {
		return filepath.Join(p.baseDir, path)
	}
	return path
}

func (p *Provider) schemaNameFor(path string, total int) string {
	if p.config.SchemaName != "" && total == 1 {
		return p.config.SchemaName
	}
	base := filepath.Base(path)
	return trimSchemaSuffix(base)
}

func collectSchemaFiles(root string, allowAnyJSON bool) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if isSchemaFile(path, allowAnyJSON) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func isSchemaFile(path string, allowAnyJSON bool) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".schema.json") || strings.HasSuffix(lower, ".jsonschema") {
		return true
	}
	if allowAnyJSON && strings.HasSuffix(lower, ".json") {
		return true
	}
	return false
}

func trimSchemaSuffix(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".schema.json"):
		return strings.TrimSuffix(name, ".schema.json")
	case strings.HasSuffix(lower, ".jsonschema"):
		return strings.TrimSuffix(name, ".jsonschema")
	case strings.HasSuffix(lower, ".json"):
		return strings.TrimSuffix(name, ".json")
	default:
		return name
	}
}

func decodeConfig(config adapters.SchemaSourceConfig) (*Config, error) {
	payload, err := json.Marshal(config.Config)
	if err != nil {
		return nil, fmt.Errorf("encoding schema config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return nil, fmt.Errorf("decoding schema config: %w", err)
	}
	return &cfg, nil
}

func init() {
	_ = adapters.RegisterSchemaProvider("buf", &Factory{})
}
