package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/effectus/effectus-go/adapters"
)

// HTTPSource implements the FactSource interface for HTTP webhooks
type HTTPSource struct {
	config      *Config
	server      *http.Server
	factChan    chan *adapters.TypedFact
	transformer *MessageTransformer
	metrics     adapters.SourceMetrics
	started     bool
}

// Config holds HTTP source configuration
type Config struct {
	SourceID        string                    `json:"source_id" yaml:"source_id"`
	ListenPort      int                       `json:"listen_port" yaml:"listen_port"`
	Path            string                    `json:"path" yaml:"path"`
	AuthMethod      string                    `json:"auth_method" yaml:"auth_method"`
	AuthConfig      map[string]string         `json:"auth_config" yaml:"auth_config"`
	ContentTypes    []string                  `json:"content_types" yaml:"content_types"`
	Transformations []adapters.Transformation `json:"transformations" yaml:"transformations"`
}

// MessageTransformer transforms HTTP requests to TypedFacts
type MessageTransformer struct {
	config          *Config
	transformations []adapters.Transformation
}

// NewHTTPSource creates a new HTTP webhook fact source
func NewHTTPSource(config *Config) (*HTTPSource, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if config.ListenPort == 0 {
		config.ListenPort = 8080
	}
	if config.Path == "" {
		config.Path = "/webhook"
	}
	if len(config.ContentTypes) == 0 {
		config.ContentTypes = []string{"application/json"}
	}

	transformer := &MessageTransformer{
		config:          config,
		transformations: config.Transformations,
	}

	return &HTTPSource{
		config:      config,
		factChan:    make(chan *adapters.TypedFact, 100),
		transformer: transformer,
		metrics:     adapters.GetGlobalMetrics(),
	}, nil
}

func validateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	if config.SourceID == "" {
		return fmt.Errorf("source ID is required")
	}
	if config.ListenPort < 0 || config.ListenPort > 65535 {
		return fmt.Errorf("invalid port: %d", config.ListenPort)
	}
	return nil
}

// Start implements FactSource.Start
func (h *HTTPSource) Start(ctx context.Context) error {
	if h.started {
		return fmt.Errorf("source already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(h.config.Path, h.handleWebhook)

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.config.ListenPort),
		Handler: mux,
	}

	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	h.started = true
	log.Printf("HTTP source %s started on port %d", h.config.SourceID, h.config.ListenPort)
	return nil
}

// Stop implements FactSource.Stop
func (h *HTTPSource) Stop(ctx context.Context) error {
	if !h.started {
		return nil
	}

	if err := h.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	close(h.factChan)
	h.started = false

	log.Printf("HTTP source %s stopped", h.config.SourceID)
	return nil
}

// Subscribe implements FactSource.Subscribe
func (h *HTTPSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	if !h.started {
		return nil, fmt.Errorf("source not started")
	}

	return h.factChan, nil
}

// GetSourceSchema implements FactSource.GetSourceSchema
func (h *HTTPSource) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    fmt.Sprintf("http_%s", h.config.SourceID),
		Version: "v1.0.0",
		Fields: map[string]interface{}{
			"method":       "string",
			"path":         "string",
			"headers":      "map[string]string",
			"body":         "bytes",
			"content_type": "string",
			"remote_addr":  "string",
		},
	}
}

// HealthCheck implements FactSource.HealthCheck
func (h *HTTPSource) HealthCheck() error {
	if !h.started {
		return fmt.Errorf("HTTP source not started")
	}

	h.metrics.RecordHealthCheck(h.config.SourceID, true)
	return nil
}

// GetMetadata implements FactSource.GetMetadata
func (h *HTTPSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      h.config.SourceID,
		SourceType:    "http",
		Version:       "v1.0.0",
		Capabilities:  []string{"webhook", "realtime", "push"},
		SchemaFormats: []string{"json", "xml", "form"},
		Config: map[string]string{
			"listen_port": fmt.Sprintf("%d", h.config.ListenPort),
			"path":        h.config.Path,
			"auth_method": h.config.AuthMethod,
		},
		Tags: []string{"webhook", "http", "api"},
	}
}

// handleWebhook handles incoming webhook requests
func (h *HTTPSource) handleWebhook(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Authenticate request
	if !h.authenticateRequest(r) {
		h.metrics.RecordError(h.config.SourceID, "authentication", fmt.Errorf("authentication failed"))
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.metrics.RecordError(h.config.SourceID, "read_body", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Transform to typed fact
	fact, err := h.transformer.TransformRequest(r, body)
	if err != nil {
		h.metrics.RecordError(h.config.SourceID, "transform", err)
		http.Error(w, "Processing Error", http.StatusInternalServerError)
		return
	}

	if fact != nil {
		select {
		case h.factChan <- fact:
			h.metrics.RecordFactProcessed(h.config.SourceID, fact.SchemaName)
			h.metrics.RecordLatency(h.config.SourceID, time.Since(start))

			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			response := map[string]interface{}{
				"status":    "received",
				"fact_type": fact.SchemaName,
				"timestamp": fact.Timestamp,
			}
			json.NewEncoder(w).Encode(response)
		default:
			h.metrics.RecordError(h.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		}
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ignored"}`)
	}
}

// authenticateRequest validates the incoming request
func (h *HTTPSource) authenticateRequest(r *http.Request) bool {
	switch h.config.AuthMethod {
	case "none", "":
		return true
	case "bearer_token":
		return h.validateBearerToken(r)
	case "api_key":
		return h.validateAPIKey(r)
	default:
		return false
	}
}

func (h *HTTPSource) validateBearerToken(r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	expectedToken := h.config.AuthConfig["token"]
	return token == expectedToken
}

func (h *HTTPSource) validateAPIKey(r *http.Request) bool {
	header := h.config.AuthConfig["token_header"]
	if header == "" {
		header = "X-API-Key"
	}

	apiKey := r.Header.Get(header)
	expectedKey := h.config.AuthConfig["expected_token"]
	return apiKey == expectedKey
}

// TransformRequest transforms an HTTP request to a TypedFact
func (t *MessageTransformer) TransformRequest(r *http.Request, body []byte) (*adapters.TypedFact, error) {
	// Extract headers
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	// Create a generic HTTP event fact
	return &adapters.TypedFact{
		SchemaName:    "http.webhook.event",
		SchemaVersion: "v1.0.0",
		Data:          nil, // Would need a generic proto message type
		RawData:       body,
		Timestamp:     time.Now(),
		SourceID:      t.config.SourceID,
		TraceID:       headers["X-Trace-Id"],
		Metadata: map[string]string{
			"http.method":      r.Method,
			"http.path":        r.URL.Path,
			"http.remote_addr": r.RemoteAddr,
			"http.user_agent":  r.UserAgent(),
			"content_type":     r.Header.Get("Content-Type"),
		},
	}, nil
}

// Factory for HTTP sources
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	httpConfig := &Config{
		SourceID: config.SourceID,
	}

	if port, ok := config.Config["listen_port"].(float64); ok {
		httpConfig.ListenPort = int(port)
	}

	if path, ok := config.Config["path"].(string); ok {
		httpConfig.Path = path
	}

	if auth, ok := config.Config["auth_method"].(string); ok {
		httpConfig.AuthMethod = auth
	}

	if authConfig, ok := config.Config["auth_config"].(map[string]interface{}); ok {
		httpConfig.AuthConfig = make(map[string]string)
		for k, v := range authConfig {
			if s, ok := v.(string); ok {
				httpConfig.AuthConfig[k] = s
			}
		}
	}

	httpConfig.Transformations = config.Transforms
	return NewHTTPSource(httpConfig)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"listen_port": {
				Type:        "int",
				Description: "Port to listen on for webhooks",
				Default:     8080,
				Examples:    []string{"8080", "3000", "9000"},
			},
			"path": {
				Type:        "string",
				Description: "Webhook endpoint path",
				Default:     "/webhook",
				Examples:    []string{"/webhook", "/api/events", "/hooks"},
			},
			"auth_method": {
				Type:        "string",
				Description: "Authentication method",
				Default:     "none",
				Examples:    []string{"none", "bearer_token", "api_key"},
			},
		},
		Required: []string{},
	}
}

func init() {
	adapters.RegisterSourceType("http", &Factory{})
}
