package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/effectus/effectus-go/adapters"
	"gopkg.in/yaml.v3"
)

type runtimeConfig struct {
	Bundle        bundleConfig                  `yaml:"bundle" json:"bundle"`
	HTTP          httpConfig                    `yaml:"http" json:"http"`
	GRPC          grpcConfig                    `yaml:"grpc" json:"grpc"`
	Metrics       httpConfig                    `yaml:"metrics" json:"metrics"`
	API           apiConfig                     `yaml:"api" json:"api"`
	Facts         factsConfig                   `yaml:"facts" json:"facts"`
	Saga          sagaConfig                    `yaml:"saga" json:"saga"`
	Verbs         verbConfig                    `yaml:"verbs" json:"verbs"`
	Extensions    extensionConfig               `yaml:"extensions" json:"extensions"`
	SchemaSources []adapters.SchemaSourceConfig `yaml:"schema_sources" json:"schema_sources"`
	FixedTime     string                        `yaml:"fixed_time" json:"fixed_time"`
	FactSource    string                        `yaml:"fact_source" json:"fact_source"`
	Kafka         kafkaConfig                   `yaml:"kafka" json:"kafka"`
	Database      databaseConfig                `yaml:"database" json:"database"`
}

type bundleConfig struct {
	File           string `yaml:"file" json:"file"`
	OCI            string `yaml:"oci" json:"oci"`
	CacheDir       string `yaml:"cache_dir" json:"cache_dir"`
	ReloadInterval string `yaml:"reload_interval" json:"reload_interval"`
}

type httpConfig struct {
	Addr string `yaml:"addr" json:"addr"`
}

type grpcConfig struct {
	Addr                 string `yaml:"addr" json:"addr"`
	TLSCert              string `yaml:"tls_cert" json:"tls_cert"`
	TLSKey               string `yaml:"tls_key" json:"tls_key"`
	AllowInsecure        *bool  `yaml:"allow_insecure" json:"allow_insecure"`
	MaxReceiveBytes      *int   `yaml:"max_receive_bytes" json:"max_receive_bytes"`
	MaxSendBytes         *int   `yaml:"max_send_bytes" json:"max_send_bytes"`
	MaxExecutionDuration string `yaml:"max_execution_duration" json:"max_execution_duration"`
	MaxConcurrent        *int   `yaml:"max_concurrent" json:"max_concurrent"`
}

type apiConfig struct {
	Auth              string `yaml:"auth" json:"auth"`
	Token             string `yaml:"token" json:"token"`
	ReadToken         string `yaml:"read_token" json:"read_token"`
	ACLFile           string `yaml:"acl_file" json:"acl_file"`
	RateLimit         *int   `yaml:"rate_limit" json:"rate_limit"`
	RateBurst         *int   `yaml:"rate_burst" json:"rate_burst"`
	Hotload           *bool  `yaml:"hotload_rules" json:"hotload_rules"`
	History           *int   `yaml:"rules_history" json:"rules_history"`
	HistoryDir        string `yaml:"rules_history_dir" json:"rules_history_dir"`
	TrustedProxyCIDRs string `yaml:"trusted_proxy_cidrs" json:"trusted_proxy_cidrs"`
	LimiterCapacity   *int   `yaml:"limiter_capacity" json:"limiter_capacity"`
	LimiterIdleTTL    string `yaml:"limiter_idle_ttl" json:"limiter_idle_ttl"`
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

type kafkaConfig struct {
	Brokers          []string `yaml:"brokers" json:"brokers"`
	Topic            string   `yaml:"topic" json:"topic"`
	ConsumerGroup    string   `yaml:"consumer_group" json:"consumer_group"`
	ClusterNamespace string   `yaml:"cluster_namespace" json:"cluster_namespace"`
	AckContract      string   `yaml:"ack_contract" json:"ack_contract"`
	MaxAttempts      *int     `yaml:"max_attempts" json:"max_attempts"`
	RetryInitial     string   `yaml:"retry_initial" json:"retry_initial"`
	RetryMax         string   `yaml:"retry_max" json:"retry_max"`
	PoisonPolicy     string   `yaml:"poison_policy" json:"poison_policy"`
	DLQTopic         string   `yaml:"dlq_topic" json:"dlq_topic"`
	DLQMode          string   `yaml:"dlq_mode" json:"dlq_mode"`
	PoisonAudit      string   `yaml:"poison_audit" json:"poison_audit"`
	DeliveryLedger   string   `yaml:"delivery_ledger" json:"delivery_ledger"`
}

type databaseConfig struct {
	MaxOpenConnections *int   `yaml:"max_open_connections" json:"max_open_connections"`
	MaxIdleConnections *int   `yaml:"max_idle_connections" json:"max_idle_connections"`
	ConnectionLifetime string `yaml:"connection_lifetime" json:"connection_lifetime"`
	ConnectionIdleTime string `yaml:"connection_idle_time" json:"connection_idle_time"`
}

type sagaConfig struct {
	Enabled  *bool              `yaml:"enabled" json:"enabled"`
	Store    string             `yaml:"store" json:"store"`
	Redis    sagaRedisConfig    `yaml:"redis" json:"redis"`
	Postgres sagaPostgresConfig `yaml:"postgres" json:"postgres"`
}

type sagaRedisConfig struct {
	Addr     string `yaml:"addr" json:"addr"`
	Password string `yaml:"password" json:"password"`
	DB       *int   `yaml:"db" json:"db"`
	Prefix   string `yaml:"prefix" json:"prefix"`
	TTL      string `yaml:"ttl" json:"ttl"`
}

type sagaPostgresConfig struct {
	DSN string `yaml:"dsn" json:"dsn"`
}

type verbConfig struct {
	SpecDirs        []string `yaml:"spec_dirs" json:"spec_dirs"`
	PluginDirs      []string `yaml:"plugin_dirs" json:"plugin_dirs"`
	DuplicatePolicy string   `yaml:"duplicate_policy" json:"duplicate_policy"`
	OCIWarmup       *bool    `yaml:"oci_warmup" json:"oci_warmup"`
	Strict          *bool    `yaml:"strict" json:"strict"`
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
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(cfg); err != nil {
			return nil, fmt.Errorf("parsing config json: %w", err)
		}
		var extra interface{}
		if err := requireConfigEOF(decoder.Decode(&extra)); err != nil {
			return nil, fmt.Errorf("parsing config json: %w", err)
		}
	default:
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(cfg); err != nil {
			return nil, fmt.Errorf("parsing config yaml: %w", err)
		}
		var extra interface{}
		if err := requireConfigEOF(decoder.Decode(&extra)); err != nil {
			return nil, fmt.Errorf("parsing config yaml: %w", err)
		}
	}
	return cfg, nil
}

func requireConfigEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple configuration documents are not allowed")
	}
	return err
}

func applyRuntimeConfig(cfg *runtimeConfig, setFlags map[string]bool) error {
	if cfg == nil {
		return nil
	}
	if cfg.Saga.Enabled != nil || cfg.Saga.Store != "" || cfg.Saga.Redis.Addr != "" || cfg.Saga.Redis.Password != "" || cfg.Saga.Redis.DB != nil || cfg.Saga.Redis.Prefix != "" || cfg.Saga.Redis.TTL != "" {
		return fmt.Errorf("legacy saga/Redis settings are not supported; remove them and configure saga.postgres.dsn for the required PostgreSQL durable runtime")
	}
	if len(cfg.Verbs.PluginDirs) != 0 {
		return fmt.Errorf("verbs.plugin_dirs is not supported; use invocation-aware extension targets")
	}
	if cfg.Kafka.PoisonAudit != "" || cfg.Kafka.DeliveryLedger != "" {
		return fmt.Errorf("kafka.poison_audit and kafka.delivery_ledger are deprecated; remove them because PostgreSQL is the sole daemon attempt and poison ledger")
	}

	if cfg.Bundle.File != "" && !setFlags["bundle"] {
		*bundleFile = cfg.Bundle.File
	}
	if cfg.Bundle.OCI != "" && !setFlags["oci-ref"] {
		*ociRef = cfg.Bundle.OCI
	}
	if cfg.Bundle.CacheDir != "" && !setFlags["oci-cache-dir"] {
		*ociCacheDir = cfg.Bundle.CacheDir
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
	if cfg.GRPC.Addr != "" && !setFlags["grpc-addr"] {
		*grpcAddr = cfg.GRPC.Addr
	}
	if cfg.GRPC.TLSCert != "" && !setFlags["grpc-tls-cert"] {
		*grpcTLSCert = cfg.GRPC.TLSCert
	}
	if cfg.GRPC.TLSKey != "" && !setFlags["grpc-tls-key"] {
		*grpcTLSKey = cfg.GRPC.TLSKey
	}
	if cfg.GRPC.AllowInsecure != nil && !setFlags["grpc-allow-insecure"] {
		*grpcAllowInsecure = *cfg.GRPC.AllowInsecure
	}
	if cfg.GRPC.MaxReceiveBytes != nil && !setFlags["grpc-max-receive-bytes"] {
		*grpcMaxReceive = *cfg.GRPC.MaxReceiveBytes
	}
	if cfg.GRPC.MaxSendBytes != nil && !setFlags["grpc-max-send-bytes"] {
		*grpcMaxSend = *cfg.GRPC.MaxSendBytes
	}
	if cfg.GRPC.MaxExecutionDuration != "" && !setFlags["grpc-max-execution-duration"] {
		duration, err := time.ParseDuration(cfg.GRPC.MaxExecutionDuration)
		if err != nil {
			return fmt.Errorf("grpc.max_execution_duration: %w", err)
		}
		*grpcMaxDuration = duration
	}
	if cfg.GRPC.MaxConcurrent != nil && !setFlags["grpc-max-concurrent"] {
		*grpcMaxConcurrent = *cfg.GRPC.MaxConcurrent
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
	if cfg.API.Hotload != nil && !setFlags["rules-hotload"] {
		*rulesHotload = *cfg.API.Hotload
	}
	if cfg.API.History != nil && !setFlags["rules-history"] {
		*rulesHistory = *cfg.API.History
	}
	if cfg.API.HistoryDir != "" && !setFlags["rules-history-dir"] {
		*rulesHistDir = cfg.API.HistoryDir
	}
	if cfg.API.TrustedProxyCIDRs != "" && !setFlags["trusted-proxy-cidrs"] {
		*trustedProxyCIDRs = cfg.API.TrustedProxyCIDRs
	}
	if cfg.API.LimiterCapacity != nil && !setFlags["api-limiter-capacity"] {
		*apiLimiterCapacity = *cfg.API.LimiterCapacity
	}
	if cfg.API.LimiterIdleTTL != "" && !setFlags["api-limiter-idle-ttl"] {
		duration, err := time.ParseDuration(cfg.API.LimiterIdleTTL)
		if err != nil {
			return fmt.Errorf("api.limiter_idle_ttl: %w", err)
		}
		*apiLimiterIdleTTL = duration
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

	if cfg.Saga.Postgres.DSN != "" && !setFlags["saga-postgres-dsn"] {
		*sagaPgDSN = cfg.Saga.Postgres.DSN
	}

	if len(cfg.Verbs.SpecDirs) > 0 && !setFlags["verb-dir"] {
		*verbDir = strings.Join(cfg.Verbs.SpecDirs, ",")
	}
	if cfg.Verbs.DuplicatePolicy != "" && !setFlags["verb-duplicate-policy"] {
		*verbDuplicatePolicy = cfg.Verbs.DuplicatePolicy
	}
	if cfg.Verbs.OCIWarmup != nil && !setFlags["verb-oci-warmup"] {
		*verbOCIWarmup = *cfg.Verbs.OCIWarmup
	}
	if cfg.Verbs.Strict != nil && !setFlags["verb-strict"] {
		*verbStrict = *cfg.Verbs.Strict
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

	if len(cfg.SchemaSources) > 0 && !setFlags["schema-sources"] {
		schemaSources = cfg.SchemaSources
	}

	if cfg.FixedTime != "" && !setFlags["fixed-time"] {
		*fixedTime = cfg.FixedTime
	}
	if cfg.FactSource != "" && !setFlags["fact-source"] {
		*factSource = cfg.FactSource
	}
	if len(cfg.Kafka.Brokers) > 0 && !setFlags["kafka-brokers"] {
		*kafkaBrokers = strings.Join(cfg.Kafka.Brokers, ",")
	}
	if cfg.Kafka.Topic != "" && !setFlags["kafka-topic"] {
		*kafkaTopic = cfg.Kafka.Topic
	}
	if cfg.Kafka.ConsumerGroup != "" && !setFlags["kafka-consumer-group"] {
		*kafkaConsumerGroup = cfg.Kafka.ConsumerGroup
	}
	if cfg.Kafka.ClusterNamespace != "" && !setFlags["kafka-cluster-namespace"] {
		*kafkaClusterNamespace = cfg.Kafka.ClusterNamespace
	}
	if cfg.Kafka.AckContract != "" && !setFlags["kafka-ack-contract"] {
		*kafkaAckContract = cfg.Kafka.AckContract
	}
	if cfg.Kafka.MaxAttempts != nil && !setFlags["kafka-max-attempts"] {
		*kafkaMaxAttempts = *cfg.Kafka.MaxAttempts
	}
	if cfg.Kafka.RetryInitial != "" && !setFlags["kafka-retry-initial"] {
		duration, err := time.ParseDuration(cfg.Kafka.RetryInitial)
		if err != nil {
			return fmt.Errorf("kafka.retry_initial: %w", err)
		}
		*kafkaRetryInitial = duration
	}
	if cfg.Kafka.RetryMax != "" && !setFlags["kafka-retry-max"] {
		duration, err := time.ParseDuration(cfg.Kafka.RetryMax)
		if err != nil {
			return fmt.Errorf("kafka.retry_max: %w", err)
		}
		*kafkaRetryMax = duration
	}
	if cfg.Kafka.PoisonPolicy != "" && !setFlags["kafka-poison-policy"] {
		*kafkaPoisonPolicy = cfg.Kafka.PoisonPolicy
	}
	if cfg.Kafka.DLQTopic != "" && !setFlags["kafka-dlq-topic"] {
		*kafkaDLQTopic = cfg.Kafka.DLQTopic
	}
	if cfg.Kafka.DLQMode != "" && !setFlags["kafka-dlq-mode"] {
		*kafkaDLQMode = cfg.Kafka.DLQMode
	}
	if cfg.Kafka.PoisonAudit != "" && !setFlags["kafka-poison-audit"] {
		*kafkaPoisonAudit = cfg.Kafka.PoisonAudit
	}
	if cfg.Kafka.DeliveryLedger != "" {
		return fmt.Errorf("kafka.delivery_ledger is no longer supported; effectusd stores delivery state in PostgreSQL table effectus_kafka_deliveries")
	}
	if cfg.Database.MaxOpenConnections != nil && !setFlags["db-max-open-connections"] {
		*dbMaxOpen = *cfg.Database.MaxOpenConnections
	}
	if cfg.Database.MaxIdleConnections != nil && !setFlags["db-max-idle-connections"] {
		*dbMaxIdle = *cfg.Database.MaxIdleConnections
	}
	if cfg.Database.ConnectionLifetime != "" && !setFlags["db-connection-lifetime"] {
		duration, err := time.ParseDuration(cfg.Database.ConnectionLifetime)
		if err != nil {
			return fmt.Errorf("database.connection_lifetime: %w", err)
		}
		*dbConnLifetime = duration
	}
	if cfg.Database.ConnectionIdleTime != "" && !setFlags["db-connection-idle-time"] {
		duration, err := time.ParseDuration(cfg.Database.ConnectionIdleTime)
		if err != nil {
			return fmt.Errorf("database.connection_idle_time: %w", err)
		}
		*dbConnIdleTime = duration
	}

	return nil
}
