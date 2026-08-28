package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateBundleArgumentsRejectsOCIReloadBeforeStartup(t *testing.T) {
	err := validateBundleArguments("", "ghcr.io/acme/rules@sha256:digest", time.Minute)
	require.EqualError(t, err, "--reload-interval cannot poll an immutable OCI reference; publish and deploy a new digest instead")
}

func TestApplyRuntimeConfigRejectsLegacyStoresAndFileLedgers(t *testing.T) {
	require.ErrorContains(t, applyRuntimeConfig(&runtimeConfig{Saga: sagaConfig{Store: "redis"}}, map[string]bool{}), "legacy saga/Redis")
	require.ErrorContains(t, applyRuntimeConfig(&runtimeConfig{Verbs: verbConfig{PluginDirs: []string{"plugins"}}}, map[string]bool{}), "plugin_dirs")
	require.ErrorContains(t, applyRuntimeConfig(&runtimeConfig{Kafka: kafkaConfig{DeliveryLedger: "attempts.jsonl"}}, map[string]bool{}), "sole daemon attempt and poison ledger")
}

func TestLoadRuntimeConfigRejectsUnknownFields(t *testing.T) {
	for _, test := range []struct {
		name    string
		ext     string
		content string
	}{
		{name: "yaml", ext: ".yaml", content: "bundle:\n  file: bundle.json\n  typo: true\n"},
		{name: "json", ext: ".json", content: `{"bundle":{"file":"bundle.json","typo":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config"+test.ext)
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0600))
			_, err := loadRuntimeConfig(path)
			require.ErrorContains(t, err, "typo")
		})
	}
}

func TestLoadRuntimeConfigReadsKafkaConsumerSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
fact_source: kafka
kafka:
  brokers: ["one:9092", "two:9092"]
  topic: facts
  consumer_group: effectusd-production
  cluster_namespace: production
  ack_contract: completed_processing
  max_attempts: 5
  retry_initial: 1s
  retry_max: 30s
  poison_policy: dlq
  dlq_topic: facts.dlq
`), 0600))
	config, err := loadRuntimeConfig(path)
	require.NoError(t, err)
	require.Equal(t, "kafka", config.FactSource)
	require.Equal(t, []string{"one:9092", "two:9092"}, config.Kafka.Brokers)
	require.Equal(t, "effectusd-production", config.Kafka.ConsumerGroup)
	require.Equal(t, "dlq", config.Kafka.PoisonPolicy)
	require.Equal(t, "facts.dlq", config.Kafka.DLQTopic)
}

func TestRuntimeConfigRejectsDeprecatedKafkaDeliveryLedger(t *testing.T) {
	config := &runtimeConfig{Kafka: kafkaConfig{DeliveryLedger: "/data/ignored.jsonl"}}
	require.ErrorContains(t, applyRuntimeConfig(config, nil), "effectus_kafka_deliveries")
}

func TestLoadRuntimeConfigReadsGeneratedGRPCServiceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
grpc:
  addr: 127.0.0.1:8081
  tls_cert: /run/secrets/tls.crt
  tls_key: /run/secrets/tls.key
  allow_insecure: false
  max_receive_bytes: 1024
  max_send_bytes: 2048
  max_execution_duration: 5s
  max_concurrent: 8
`), 0o600))
	config, err := loadRuntimeConfig(path)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8081", config.GRPC.Addr)
	require.Equal(t, "/run/secrets/tls.crt", config.GRPC.TLSCert)
	require.Equal(t, 8, *config.GRPC.MaxConcurrent)
}

func TestLoadRuntimeConfigRejectsMultipleDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("bundle: {}\n---\nhttp: {}\n"), 0600))
	_, err := loadRuntimeConfig(path)
	require.ErrorContains(t, err, "multiple configuration documents")
}
