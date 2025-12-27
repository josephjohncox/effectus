package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/effectus/effectus-go/unified"
)

func newResolveCommand() *Command {
	resolveCmd := &Command{
		Name:        "resolve",
		Description: "Resolve bundle dependencies from an extension manifest",
		FlagSet:     flag.NewFlagSet("resolve", flag.ExitOnError),
	}

	manifestPath := resolveCmd.FlagSet.String("manifest", "", "Path to extension manifest (defaults to first arg)")
	cacheDir := resolveCmd.FlagSet.String("cache", "", "Bundle cache directory (defaults to EFFECTUS_BUNDLE_CACHE or ./bundles)")
	registry := resolveCmd.FlagSet.String("registry", "", "Registry override(s): name=base or base (comma-separated)")
	defaultRegistry := resolveCmd.FlagSet.String("default-registry", "", "Default registry name")
	engineVersion := resolveCmd.FlagSet.String("engine-version", "", "Effectus engine version for compatibility checks")
	verify := resolveCmd.FlagSet.Bool("verify", true, "Verify bundle checksums when provided")
	format := resolveCmd.FlagSet.String("format", "text", "Output format: text or json")

	resolveCmd.Run = func() error {
		path := strings.TrimSpace(*manifestPath)
		if path == "" {
			args := resolveCmd.FlagSet.Args()
			if len(args) > 0 {
				path = args[0]
			}
		}
		if path == "" {
			return fmt.Errorf("manifest path is required")
		}

		opts := unified.ResolverOptions{
			CacheDir:        *cacheDir,
			DefaultRegistry: strings.TrimSpace(*defaultRegistry),
			EngineVersion:   strings.TrimSpace(*engineVersion),
			VerifyChecksum:  *verify,
			AllowFile:       true,
			AllowOCI:        true,
		}

		// Apply env registry overrides.
		opts.Registries = append(opts.Registries, parseRegistryOverrides(os.Getenv("EFFECTUS_BUNDLE_REGISTRIES"))...)
		if envDefault := strings.TrimSpace(os.Getenv("EFFECTUS_BUNDLE_REGISTRY")); envDefault != "" {
			opts.Registries = append(opts.Registries, unified.RegistryConfig{Name: opts.DefaultRegistry, Base: envDefault, Default: true})
		}

		// Apply CLI registry overrides last.
		if strings.TrimSpace(*registry) != "" {
			opts.Registries = append(opts.Registries, parseRegistryOverrides(*registry)...)
		}

		resolved, err := unified.ResolveManifestFile(path, opts)
		if err != nil {
			// Still render output if we resolved some bundles.
			if len(resolved) == 0 {
				return err
			}
			fmt.Fprintf(os.Stderr, "Resolve warnings: %v\n", err)
		}

		switch strings.ToLower(strings.TrimSpace(*format)) {
		case "json":
			payload, err := json.MarshalIndent(resolved, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding output: %w", err)
			}
			fmt.Println(string(payload))
		default:
			for _, bundle := range resolved {
				fmt.Printf("%s@%s [%s] %s (%s)\n", bundle.Name, bundle.Version, bundle.Source, bundle.Location, bundle.Checksum)
			}
		}

		return nil
	}

	return resolveCmd
}

func parseRegistryOverrides(raw string) []unified.RegistryConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	configs := make([]unified.RegistryConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			name := strings.TrimSpace(kv[0])
			base := strings.TrimSpace(kv[1])
			configs = append(configs, unified.RegistryConfig{Name: name, Base: base})
			continue
		}
		configs = append(configs, unified.RegistryConfig{Name: "", Base: part, Default: true})
	}
	return configs
}
