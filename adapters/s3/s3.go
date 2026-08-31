package s3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/parquet-go/parquet-go"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/josephjohncox/effectus/adapters"
)

type s3API interface {
	s3.ListObjectsV2APIClient
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

// Source implements an S3-backed fact source with batch and streaming modes.
type Source struct {
	config       *Config
	client       s3API
	ctx          context.Context
	cancel       context.CancelFunc
	out          chan *adapters.TypedFact
	done         chan struct{}
	running      bool
	metrics      adapters.SourceMetrics
	mappings     []factMapping
	lastSeenTime time.Time
	lastSeenKey  string
	mu           sync.Mutex
}

// Config holds S3 source configuration.
type Config struct {
	SourceID        string        `json:"source_id" yaml:"source_id"`
	Region          string        `json:"region" yaml:"region"`
	Bucket          string        `json:"bucket" yaml:"bucket"`
	Prefix          string        `json:"prefix" yaml:"prefix"`
	Mode            string        `json:"mode" yaml:"mode"`     // "batch" or "stream"
	Format          string        `json:"format" yaml:"format"` // "json", "ndjson", or "parquet"
	PollInterval    time.Duration `json:"poll_interval" yaml:"poll_interval"`
	MaxObjects      int           `json:"max_objects" yaml:"max_objects"`
	MaxObjectBytes  int64         `json:"max_object_bytes" yaml:"max_object_bytes"`
	SchemaName      string        `json:"schema_name" yaml:"schema_name"`
	SchemaVersion   string        `json:"schema_version" yaml:"schema_version"`
	Endpoint        string        `json:"endpoint" yaml:"endpoint"`
	ForcePathStyle  bool          `json:"force_path_style" yaml:"force_path_style"`
	AccessKey       string        `json:"access_key" yaml:"access_key"`
	SecretKey       string        `json:"secret_key" yaml:"secret_key"`
	SessionToken    string        `json:"session_token" yaml:"session_token"`
	StartAfter      string        `json:"start_after" yaml:"start_after"`
	StartTime       string        `json:"start_time" yaml:"start_time"`
	MappingStrategy string        `json:"mapping_strategy" yaml:"mapping_strategy"` // prefix|exact|suffix|contains
	Timeout         time.Duration `json:"timeout" yaml:"timeout"`
}

type factMapping struct {
	SourceKey     string
	SchemaName    string
	SchemaVersion string
}

// NewSource creates a new S3 source.
func NewSource(config *Config, mappings []adapters.FactMapping) (*Source, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	source := &Source{
		config:  config,
		metrics: adapters.GetGlobalMetrics(),
	}

	for _, mapping := range mappings {
		source.mappings = append(source.mappings, factMapping{
			SourceKey:     mapping.SourceKey,
			SchemaName:    mapping.EffectusType,
			SchemaVersion: mapping.SchemaVersion,
		})
	}

	if config.StartTime != "" {
		if parsed, err := time.Parse(time.RFC3339, config.StartTime); err == nil {
			source.lastSeenTime = parsed
		}
	}
	if config.StartAfter != "" {
		source.lastSeenKey = config.StartAfter
	}

	return source, nil
}

func validateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	if config.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if config.Region == "" {
		config.Region = "us-east-1"
	}
	if config.Mode == "" {
		config.Mode = "batch"
	}
	if config.Mode != "batch" && config.Mode != "stream" {
		return fmt.Errorf("mode must be batch or stream")
	}
	if config.Format == "" {
		config.Format = "json"
	}
	if config.SchemaName == "" {
		config.SchemaName = "s3_object"
	}
	if config.SchemaVersion == "" {
		config.SchemaVersion = "v1"
	}
	if config.PollInterval == 0 {
		if config.Mode == "stream" {
			config.PollInterval = 5 * time.Second
		} else {
			config.PollInterval = 5 * time.Minute
		}
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxObjectBytes == 0 {
		config.MaxObjectBytes = 10 * 1024 * 1024 // 10MB
	}
	if config.MappingStrategy == "" {
		config.MappingStrategy = "prefix"
	}
	return nil
}

// Start initializes the S3 client.
func (s *Source) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(s.config.Region),
	}

	if s.config.AccessKey != "" && s.config.SecretKey != "" {
		creds := credentials.NewStaticCredentialsProvider(
			s.config.AccessKey,
			s.config.SecretKey,
			s.config.SessionToken,
		)
		opts = append(opts, config.WithCredentialsProvider(creds))
	}

	if s.config.Endpoint != "" {
		resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               s.config.Endpoint,
					HostnameImmutable: true,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		})
		opts = append(opts, config.WithEndpointResolverWithOptions(resolver))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.UsePathStyle = s.config.ForcePathStyle
	})
	s.mu.Lock()
	if s.client == nil {
		s.client = client
	}
	s.mu.Unlock()

	log.Printf("S3 source %s started (bucket=%s, mode=%s)", s.config.SourceID, s.config.Bucket, s.config.Mode)
	return nil
}

// Stop stops the source.
func (s *Source) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	if s.done == done {
		s.ctx = nil
		s.cancel = nil
		s.out = nil
		s.done = nil
		s.running = false
	}
	s.mu.Unlock()
	log.Printf("S3 source %s stopped", s.config.SourceID)
	return nil
}

// Subscribe polls S3 through one serialized worker per source generation.
func (s *Source) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	s.mu.Lock()
	if s.running {
		out := s.out
		s.mu.Unlock()
		return out, nil
	}
	clientReady := s.client != nil
	s.mu.Unlock()
	if !clientReady {
		if err := s.Start(ctx); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	if s.running {
		out := s.out
		s.mu.Unlock()
		return out, nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	factChan := make(chan *adapters.TypedFact, 100)
	done := make(chan struct{})
	s.ctx, s.cancel, s.out, s.done, s.running = workerCtx, cancel, factChan, done, true
	s.mu.Unlock()

	go s.pollLoop(workerCtx, factChan, done)
	return factChan, nil
}

func (s *Source) pollLoop(ctx context.Context, factChan chan *adapters.TypedFact, done chan struct{}) {
	defer close(done)
	defer close(factChan)
	defer func() {
		s.mu.Lock()
		if s.done == done {
			s.running = false
		}
		s.mu.Unlock()
	}()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	if err := s.pollOnce(ctx, factChan); err != nil && ctx.Err() == nil {
		log.Printf("S3 poll error: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.pollOnce(ctx, factChan); err != nil && ctx.Err() == nil {
				log.Printf("S3 poll error: %v", err)
			}
		}
	}
}

// GetSourceSchema returns schema metadata.
func (s *Source) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    s.config.SchemaName,
		Version: s.config.SchemaVersion,
		Fields: map[string]interface{}{
			"bucket":        s.config.Bucket,
			"prefix":        s.config.Prefix,
			"mode":          s.config.Mode,
			"format":        s.config.Format,
			"poll_interval": s.config.PollInterval.String(),
		},
	}
}

// HealthCheck checks bucket access.
func (s *Source) HealthCheck() error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return fmt.Errorf("s3 client not initialized")
	}
	_, err := client.HeadBucket(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(s.config.Bucket),
	})
	if err != nil {
		s.metrics.RecordHealthCheck(s.config.SourceID, false)
		return err
	}
	s.metrics.RecordHealthCheck(s.config.SourceID, true)
	return nil
}

// GetMetadata returns source metadata.
func (s *Source) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      s.config.SourceID,
		SourceType:    "s3",
		Version:       "1.0.0",
		Capabilities:  []string{"batch", "stream", "object_store"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"bucket":        s.config.Bucket,
			"prefix":        s.config.Prefix,
			"mode":          s.config.Mode,
			"poll_interval": s.config.PollInterval.String(),
		},
		Tags: []string{"s3", "object-storage"},
	}
}

func (s *Source) pollOnce(subscriptionCtx context.Context, factChan chan<- *adapters.TypedFact) error {
	if s.client == nil {
		return fmt.Errorf("s3 client not initialized")
	}

	ctx, cancel := context.WithTimeout(subscriptionCtx, s.config.Timeout)
	defer cancel()

	objects, err := s.listObjects(ctx)
	if err != nil {
		return err
	}

	if s.config.Mode == "stream" {
		objects = s.filterNewObjects(objects)
		// MaxObjects is an emission bound, not a bound on the raw listing. All
		// pages must be scanned before applying it or old keys on the first page
		// can permanently starve later keys.
		if s.config.MaxObjects > 0 && len(objects) > s.config.MaxObjects {
			objects = objects[:s.config.MaxObjects]
		}
	}

	for _, object := range objects {
		if err := s.emitObject(ctx, object, factChan); err != nil {
			return fmt.Errorf("emit %s: %w", aws.ToString(object.Key), err)
		}
	}
	return nil
}

func (s *Source) listObjects(ctx context.Context) ([]types.Object, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.config.Bucket),
	}
	if s.config.Prefix != "" {
		input.Prefix = aws.String(s.config.Prefix)
	}

	paginator := s3.NewListObjectsV2Paginator(s.client, input)
	var objects []types.Object

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		objects = append(objects, page.Contents...)
		// Batch mode has no stream cursor, so its listing limit is independent.
		if s.config.Mode == "batch" && s.config.MaxObjects > 0 && len(objects) >= s.config.MaxObjects {
			objects = objects[:s.config.MaxObjects]
			break
		}
	}

	return objects, nil
}

func (s *Source) filterNewObjects(objects []types.Object) []types.Object {
	s.mu.Lock()
	lastTime := s.lastSeenTime
	lastKey := s.lastSeenKey
	s.mu.Unlock()

	filtered := make([]types.Object, 0, len(objects))
	for _, object := range objects {
		key := aws.ToString(object.Key)
		modified := time.Time{}
		if object.LastModified != nil {
			modified = *object.LastModified
		}

		if modified.After(lastTime) || (modified.Equal(lastTime) && key > lastKey) {
			filtered = append(filtered, object)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		left := filtered[i]
		right := filtered[j]
		leftTime := time.Time{}
		rightTime := time.Time{}
		if left.LastModified != nil {
			leftTime = *left.LastModified
		}
		if right.LastModified != nil {
			rightTime = *right.LastModified
		}
		if leftTime.Equal(rightTime) {
			return aws.ToString(left.Key) < aws.ToString(right.Key)
		}
		return leftTime.Before(rightTime)
	})

	return filtered
}

func (s *Source) updateCursor(objects []types.Object) {
	if len(objects) == 0 {
		return
	}
	last := objects[len(objects)-1]
	lastTime := time.Time{}
	if last.LastModified != nil {
		lastTime = *last.LastModified
	}

	s.mu.Lock()
	s.lastSeenTime = lastTime
	s.lastSeenKey = aws.ToString(last.Key)
	s.mu.Unlock()
}

func (s *Source) emitObject(ctx context.Context, object types.Object, factChan chan<- *adapters.TypedFact) error {
	key := aws.ToString(object.Key)
	if key == "" {
		return fmt.Errorf("object key is empty")
	}
	if object.Size != nil && s.config.MaxObjectBytes > 0 && *object.Size > s.config.MaxObjectBytes {
		return fmt.Errorf("object size %d exceeds maximum %d", *object.Size, s.config.MaxObjectBytes)
	}
	if strings.HasSuffix(key, "/") {
		return nil
	}

	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := readAll(resp.Body, s.config.MaxObjectBytes)
	if err != nil {
		return err
	}

	records, err := decodePayload(payload, s.config.Format)
	if err != nil {
		return err
	}

	schemaName, schemaVersion := s.resolveSchema(key)
	metadata := map[string]string{
		"s3_bucket": s.config.Bucket,
		"s3_key":    key,
	}
	if object.ETag != nil {
		metadata["s3_etag"] = strings.Trim(*object.ETag, "\"")
	}
	if object.LastModified != nil {
		metadata["s3_last_modified"] = object.LastModified.Format(time.RFC3339Nano)
	}

	var barrier *adapters.AcknowledgementBarrier
	if s.config.Mode == "stream" && len(records) > 0 {
		barrier = adapters.NewAcknowledgementBarrier(len(records), func(context.Context) error {
			s.updateCursor([]types.Object{object})
			return nil
		})
	}
	for index, record := range records {
		structData, err := structpb.NewStruct(record)
		if err != nil {
			return err
		}
		fact := &adapters.TypedFact{
			SchemaName:    schemaName,
			SchemaVersion: schemaVersion,
			Data:          structData,
			RawData:       payload,
			Timestamp:     time.Now().UTC(),
			SourceID:      s.config.SourceID,
			Metadata:      metadata,
		}
		if barrier != nil {
			fact.Acknowledge = barrier.Callback(index)
		}

		select {
		case factChan <- fact:
			s.metrics.RecordFactProcessed(s.config.SourceID, schemaName)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if barrier != nil {
		return barrier.Wait(ctx)
	}
	if s.config.Mode == "stream" {
		s.updateCursor([]types.Object{object})
	}
	return nil
}

func (s *Source) resolveSchema(key string) (string, string) {
	for _, mapping := range s.mappings {
		if matchKey(key, mapping.SourceKey, s.config.MappingStrategy) {
			return mapping.SchemaName, mapping.SchemaVersion
		}
	}
	return s.config.SchemaName, s.config.SchemaVersion
}

func matchKey(key, pattern, strategy string) bool {
	if pattern == "" {
		return false
	}
	switch strategy {
	case "exact":
		return key == pattern
	case "suffix":
		return strings.HasSuffix(key, pattern)
	case "contains":
		return strings.Contains(key, pattern)
	default:
		return strings.HasPrefix(key, pattern)
	}
}

func decodePayload(payload []byte, format string) ([]map[string]interface{}, error) {
	switch strings.ToLower(format) {
	case "ndjson", "jsonl":
		return decodeNDJSON(payload)
	case "parquet":
		return decodeParquet(payload)
	default:
		return decodeJSON(payload)
	}
}

func decodeJSON(payload []byte) ([]map[string]interface{}, error) {
	var value interface{}
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}

	switch typed := value.(type) {
	case []interface{}:
		records := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			records = append(records, normalizeRecord(item))
		}
		return records, nil
	default:
		return []map[string]interface{}{normalizeRecord(typed)}, nil
	}
}

func decodeNDJSON(payload []byte) ([]map[string]interface{}, error) {
	reader := bufio.NewScanner(bytes.NewReader(payload))
	buf := make([]byte, 0, 64*1024)
	reader.Buffer(buf, 10*1024*1024)
	var records []map[string]interface{}
	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" {
			continue
		}
		var value interface{}
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, err
		}
		records = append(records, normalizeRecord(value))
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func decodeParquet(payload []byte) ([]map[string]interface{}, error) {
	reader := parquet.NewGenericReader[map[string]interface{}](bytes.NewReader(payload))
	defer reader.Close()

	var records []map[string]interface{}
	batch := make([]map[string]interface{}, 256)

	for {
		n, err := reader.Read(batch)
		if n > 0 {
			for i := 0; i < n; i++ {
				records = append(records, normalizeRecord(batch[i]))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return records, nil
}

func normalizeRecord(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{"value": nil}
	}
	if record, ok := value.(map[string]interface{}); ok {
		return record
	}
	return map[string]interface{}{"value": value}
}

func readAll(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("object exceeds max_object_bytes")
	}
	return data, nil
}

// Factory creates S3 sources from generic config.
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	s3Config := &Config{
		SourceID: config.SourceID,
	}

	if v, ok := config.Config["region"].(string); ok {
		s3Config.Region = v
	}
	if v, ok := config.Config["bucket"].(string); ok {
		s3Config.Bucket = v
	}
	if v, ok := config.Config["prefix"].(string); ok {
		s3Config.Prefix = v
	}
	if v, ok := config.Config["mode"].(string); ok {
		s3Config.Mode = v
	}
	if v, ok := config.Config["format"].(string); ok {
		s3Config.Format = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		s3Config.SchemaName = v
	}
	if v, ok := config.Config["schema_version"].(string); ok {
		s3Config.SchemaVersion = v
	}
	if v, ok := config.Config["endpoint"].(string); ok {
		s3Config.Endpoint = v
	}
	if v, ok := config.Config["force_path_style"].(bool); ok {
		s3Config.ForcePathStyle = v
	}
	if v, ok := config.Config["access_key"].(string); ok {
		s3Config.AccessKey = v
	}
	if v, ok := config.Config["secret_key"].(string); ok {
		s3Config.SecretKey = v
	}
	if v, ok := config.Config["session_token"].(string); ok {
		s3Config.SessionToken = v
	}
	if v, ok := config.Config["start_after"].(string); ok {
		s3Config.StartAfter = v
	}
	if v, ok := config.Config["start_time"].(string); ok {
		s3Config.StartTime = v
	}
	if v, ok := config.Config["mapping_strategy"].(string); ok {
		s3Config.MappingStrategy = v
	}
	if v, ok := config.Config["poll_interval"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			s3Config.PollInterval = parsed
		}
	}
	if v, ok := config.Config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			s3Config.Timeout = parsed
		}
	}
	if v, ok := config.Config["max_objects"].(float64); ok {
		s3Config.MaxObjects = int(v)
	}
	if v, ok := config.Config["max_objects"].(int); ok {
		s3Config.MaxObjects = v
	}
	if v, ok := config.Config["max_object_bytes"].(float64); ok {
		s3Config.MaxObjectBytes = int64(v)
	}
	if v, ok := config.Config["max_object_bytes"].(int); ok {
		s3Config.MaxObjectBytes = int64(v)
	}
	if v, ok := config.Config["max_object_bytes"].(int64); ok {
		s3Config.MaxObjectBytes = v
	}

	if s3Config.SchemaName == "" && len(config.Mappings) == 1 {
		s3Config.SchemaName = config.Mappings[0].EffectusType
		s3Config.SchemaVersion = config.Mappings[0].SchemaVersion
	}

	return NewSource(s3Config, config.Mappings)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["bucket"]; !ok {
		return fmt.Errorf("bucket is required")
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"region": {
				Type:        "string",
				Description: "AWS region (defaults to us-east-1)",
			},
			"bucket": {
				Type:        "string",
				Description: "S3 bucket name",
			},
			"prefix": {
				Type:        "string",
				Description: "Optional prefix filter",
			},
			"mode": {
				Type:        "string",
				Description: "batch or stream",
				Default:     "batch",
				Examples:    []string{"batch", "stream"},
			},
			"format": {
				Type:        "string",
				Description: "json, ndjson, or parquet",
				Default:     "json",
			},
			"poll_interval": {
				Type:        "string",
				Description: "Polling interval (e.g., 30s, 5m)",
			},
			"max_objects": {
				Type:        "int",
				Description: "Maximum objects per poll",
			},
			"max_object_bytes": {
				Type:        "int",
				Description: "Maximum object size in bytes",
			},
			"schema_name": {
				Type:        "string",
				Description: "Effectus schema name for emitted facts",
			},
			"schema_version": {
				Type:        "string",
				Description: "Schema version tag",
				Default:     "v1",
			},
			"endpoint": {
				Type:        "string",
				Description: "Custom S3 endpoint (for MinIO, R2, etc.)",
			},
			"force_path_style": {
				Type:        "bool",
				Description: "Force path-style addressing (S3-compatible services)",
			},
			"access_key": {
				Type:        "string",
				Description: "Static access key (optional)",
			},
			"secret_key": {
				Type:        "string",
				Description: "Static secret key (optional)",
			},
			"session_token": {
				Type:        "string",
				Description: "Session token for temporary credentials",
			},
			"start_after": {
				Type:        "string",
				Description: "Initial cursor key for streaming",
			},
			"start_time": {
				Type:        "string",
				Description: "Initial cursor time (RFC3339) for streaming",
			},
			"mapping_strategy": {
				Type:        "string",
				Description: "prefix, exact, suffix, or contains",
				Default:     "prefix",
			},
		},
		Required: []string{"bucket"},
	}
}

func init() {
	adapters.RegisterSourceType("s3", &Factory{})
}
