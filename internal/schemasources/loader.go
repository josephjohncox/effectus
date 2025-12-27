package schemasources

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go/adapters"
	"github.com/effectus/effectus-go/schema/types"
	"gopkg.in/yaml.v3"
)

// LoadFromFile reads schema source configurations from a YAML/JSON file.
func LoadFromFile(path string) ([]adapters.SchemaSourceConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema sources: %w", err)
	}
	sources, err := decodeSources(path, data)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(path)
	for i := range sources {
		if sources[i].BaseDir == "" {
			sources[i].BaseDir = baseDir
		}
	}
	return sources, nil
}

// Apply loads schema definitions into the provided type system.
func Apply(ctx context.Context, typeSystem *types.TypeSystem, sources []adapters.SchemaSourceConfig, verbose bool) error {
	if typeSystem == nil || len(sources) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for idx, source := range sources {
		if strings.TrimSpace(source.Type) == "" {
			return fmt.Errorf("schema source %d has empty type", idx)
		}
		if verbose {
			label := source.Name
			if label == "" {
				label = source.Type
			}
			fmt.Printf("Loading schema source %s\n", label)
		}

		provider, err := adapters.CreateSchemaProvider(source)
		if err != nil {
			return fmt.Errorf("creating schema provider %s: %w", source.Type, err)
		}

		definitions, loadErr := provider.LoadSchemas(ctx)
		closeErr := provider.Close()
		if loadErr != nil {
			return fmt.Errorf("loading schemas from %s: %w", source.Type, loadErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing schema provider %s: %w", source.Type, closeErr)
		}

		for _, def := range definitions {
			if len(def.Data) == 0 {
				return fmt.Errorf("schema provider %s returned empty schema payload", source.Type)
			}
			format := def.Format
			if format == "" {
				format = adapters.SchemaFormatAuto
			}
			prefix := joinPrefix(source.Namespace, def.Name)
			version := def.Version
			if strings.TrimSpace(version) == "" {
				version = source.Version
			}
			if verbose {
				fmt.Printf("  -> %s (format=%s, version=%s)\n", prefix, format, version)
			}
			if err := applyDefinition(typeSystem, format, prefix, version, def.Data); err != nil {
				return fmt.Errorf("applying schema %s: %w", prefix, err)
			}
		}
	}
	return nil
}

func applyDefinition(typeSystem *types.TypeSystem, format adapters.SchemaFormat, prefix, version string, data []byte) error {
	switch format {
	case adapters.SchemaFormatJSONSchema:
		if strings.TrimSpace(version) != "" {
			return typeSystem.LoadJSONSchemaBytesVersion(prefix, version, data, true)
		}
		return typeSystem.LoadJSONSchemaBytes(prefix, data)
	case adapters.SchemaFormatEffectus:
		if strings.TrimSpace(version) != "" {
			return typeSystem.LoadSchemaJSONBytesVersion(prefix, version, data, true)
		}
		return typeSystem.LoadSchemaJSONBytes(prefix, data)
	case adapters.SchemaFormatAuto:
		if strings.TrimSpace(version) != "" {
			return typeSystem.LoadSchemaJSONBytesVersion(prefix, version, data, true)
		}
		return typeSystem.LoadSchemaJSONBytes(prefix, data)
	case adapters.SchemaFormatProto:
		return fmt.Errorf("proto schema format is not supported by the type system")
	default:
		return fmt.Errorf("unsupported schema format: %s", format)
	}
}

func joinPrefix(namespace, name string) string {
	namespace = strings.Trim(namespace, ".")
	name = strings.Trim(name, ".")
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "." + name
}

func decodeSources(path string, data []byte) ([]adapters.SchemaSourceConfig, error) {
	wrapper := struct {
		SchemaSources []adapters.SchemaSourceConfig `json:"schema_sources" yaml:"schema_sources"`
	}{
		SchemaSources: nil,
	}

	ext := strings.ToLower(filepath.Ext(path))
	var err error
	if ext == ".json" {
		err = json.Unmarshal(data, &wrapper)
	} else {
		err = yaml.Unmarshal(data, &wrapper)
	}
	if err == nil && len(wrapper.SchemaSources) > 0 {
		return wrapper.SchemaSources, nil
	}

	trimmed := strings.TrimSpace(string(data))
	isList := strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "-")
	if err == nil && !isList {
		return wrapper.SchemaSources, nil
	}

	var sources []adapters.SchemaSourceConfig
	if ext == ".json" {
		if err := json.Unmarshal(data, &sources); err != nil {
			return nil, fmt.Errorf("parsing schema sources: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &sources); err != nil {
			return nil, fmt.Errorf("parsing schema sources: %w", err)
		}
	}
	return sources, nil
}
