package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type runtimeConfig struct {
	Bundle     bundleConfig    `yaml:"bundle" json:"bundle"`
	HTTP       httpConfig      `yaml:"http" json:"http"`
	Metrics    httpConfig      `yaml:"metrics" json:"metrics"`
	API        apiConfig       `yaml:"api" json:"api"`
	Facts      factsConfig     `yaml:"facts" json:"facts"`
	Saga       sagaConfig      `yaml:"saga" json:"saga"`
	Verbs      verbConfig      `yaml:"verbs" json:"verbs"`
	Extensions extensionConfig `yaml:"extensions" json:"extensions"`
}

type bundleConfig struct {
	File           string `yaml:"file" json:"file"`
	OCI            string `yaml:"oci" json:"oci"`
	ReloadInterval string `yaml:"reload_interval" json:"reload_interval"`
}

type httpConfig struct {
	Addr string `yaml:"addr" json:"addr"`
}

type apiConfig struct {
	Auth      string `yaml:"auth" json:"auth"`
	Token     string `yaml:"token" json:"token"`
	ReadToken string `yaml:"read_token" json:"read_token"`
	ACLFile   string `yaml:"acl_file" json:"acl_file"`
	RateLimit *int   `yaml:"rate_limit" json:"rate_limit"`
	RateBurst *int   `yaml:"rate_burst" json:"rate_burst"`
}

type factsConfig struct {
	Store          string            `yaml:"store" json:"store"`
	Path           string            `yaml:"path" json:"path"`
	MergeDefault   string            `yaml:"merge_default" json:"merge_default"`
	MergeNamespace map[string]string `yaml:"merge_namespace" json:"merge_namespace"`
	Cache          factsCacheConfig  `yaml:"cache" json:"cache"`
}

type factsCacheConfig struct {
	Policy        string `yaml:"policy" json:"policy"`
	MaxUniverses  *int   `yaml:"max_universes" json:"max_universes"`
	MaxNamespaces *int   `yaml:"max_namespaces" json:"max_namespaces"`
}

type sagaConfig struct {
	Enabled *bool  `yaml:"enabled" json:"enabled"`
	Store   string `yaml:"store" json:"store"`
}

type verbConfig struct {
	SpecDirs   []string `yaml:"spec_dirs" json:"spec_dirs"`
	PluginDirs []string `yaml:"plugin_dirs" json:"plugin_dirs"`
}

type extensionConfig struct {
	Dirs           []string `yaml:"dirs" json:"dirs"`
	OCI            []string `yaml:"oci" json:"oci"`
	ReloadInterval string   `yaml:"reload_interval" json:"reload_interval"`
}

func loadRuntimeConfig(path string) (*runtimeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &runtimeConfig{}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config json: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config yaml: %w", err)
		}
	}
	return cfg, nil
}

func applyRuntimeConfig(cfg *runtimeConfig, setFlags map[string]bool) error {
	if cfg == nil {
		return nil
	}

	if cfg.Bundle.File != "" && !setFlags["bundle"] {
		*bundleFile = cfg.Bundle.File
	}
	if cfg.Bundle.OCI != "" && !setFlags["oci-ref"] {
		*ociRef = cfg.Bundle.OCI
	}
	if cfg.Bundle.ReloadInterval != "" && !setFlags["reload-interval"] {
		interval, err := time.ParseDuration(cfg.Bundle.ReloadInterval)
		if err != nil {
			return fmt.Errorf("bundle.reload_interval: %w", err)
		}
		*reloadInterval = interval
	}

	if cfg.HTTP.Addr != "" && !setFlags["http-addr"] {
		*httpAddr = cfg.HTTP.Addr
	}
	if cfg.Metrics.Addr != "" && !setFlags["metrics-addr"] {
		*metricsAddr = cfg.Metrics.Addr
	}

	if cfg.API.Auth != "" && !setFlags["api-auth"] {
		*apiAuthMode = cfg.API.Auth
	}
	if cfg.API.Token != "" && !setFlags["api-token"] {
		*apiToken = cfg.API.Token
	}
	if cfg.API.ReadToken != "" && !setFlags["api-read-token"] {
		*apiReadToken = cfg.API.ReadToken
	}
	if cfg.API.ACLFile != "" && !setFlags["api-acl-file"] {
		*apiACLFile = cfg.API.ACLFile
	}
	if cfg.API.RateLimit != nil && !setFlags["api-rate-limit"] {
		*apiRateLimit = *cfg.API.RateLimit
	}
	if cfg.API.RateBurst != nil && !setFlags["api-rate-burst"] {
		*apiRateBurst = *cfg.API.RateBurst
	}

	if cfg.Facts.Store != "" && !setFlags["facts-store"] {
		*factsStore = cfg.Facts.Store
	}
	if cfg.Facts.Path != "" && !setFlags["facts-path"] {
		*factsPath = cfg.Facts.Path
	}
	if cfg.Facts.MergeDefault != "" && !setFlags["facts-merge-default"] {
		*factsMergeDef = cfg.Facts.MergeDefault
	}
	if len(cfg.Facts.MergeNamespace) > 0 && !setFlags["facts-merge-namespace"] {
		for ns, strategy := range cfg.Facts.MergeNamespace {
			if err := factsMergeNs.Set(fmt.Sprintf("%s=%s", ns, strategy)); err != nil {
				return fmt.Errorf("facts.merge_namespace: %w", err)
			}
		}
	}
	if cfg.Facts.Cache.Policy != "" && !setFlags["facts-cache-policy"] {
		*factsCache = cfg.Facts.Cache.Policy
	}
	if cfg.Facts.Cache.MaxUniverses != nil && !setFlags["facts-cache-max-universes"] {
		*factsCacheMax = *cfg.Facts.Cache.MaxUniverses
	}
	if cfg.Facts.Cache.MaxNamespaces != nil && !setFlags["facts-cache-max-namespaces"] {
		*factsCacheNs = *cfg.Facts.Cache.MaxNamespaces
	}

	if cfg.Saga.Enabled != nil && !setFlags["saga"] {
		*sagaEnabled = *cfg.Saga.Enabled
	}
	if cfg.Saga.Store != "" && !setFlags["saga-store"] {
		*sagaStoreType = cfg.Saga.Store
	}

	if len(cfg.Verbs.SpecDirs) > 0 && !setFlags["verb-dir"] {
		*verbDir = strings.Join(cfg.Verbs.SpecDirs, ",")
	}
	if len(cfg.Verbs.PluginDirs) > 0 && !setFlags["plugin-dir"] {
		*pluginDir = strings.Join(cfg.Verbs.PluginDirs, ",")
	}

	if len(cfg.Extensions.Dirs) > 0 && !setFlags["extensions-dir"] {
		*extensionsDir = strings.Join(cfg.Extensions.Dirs, ",")
	}
	if len(cfg.Extensions.OCI) > 0 && !setFlags["extensions-oci"] {
		*extensionsOCI = strings.Join(cfg.Extensions.OCI, ",")
	}
	if cfg.Extensions.ReloadInterval != "" && !setFlags["extensions-reload-interval"] {
		interval, err := time.ParseDuration(cfg.Extensions.ReloadInterval)
		if err != nil {
			return fmt.Errorf("extensions.reload_interval: %w", err)
		}
		*extensionsReloadInterval = interval
	}

	return nil
}
