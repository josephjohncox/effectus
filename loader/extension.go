package loader

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/schema/verb"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExtensionManager provides a unified way to extend Effectus with verbs and schemas
type ExtensionManager struct {
	loaders []Loader
}

// NewExtensionManager creates a new extension manager
func NewExtensionManager() *ExtensionManager {
	return &ExtensionManager{
		loaders: make([]Loader, 0),
	}
}

// LoadExtensions loads all registered extensions into the provided registries
func (em *ExtensionManager) LoadExtensions(ctx context.Context, target LoadTarget) error {
	for _, loader := range em.loaders {
		if err := loader.Load(ctx, target); err != nil {
			return fmt.Errorf("failed to load extension %s: %w", loader.Name(), err)
		}
	}
	return nil
}

// AddLoader registers a loader for static or dynamic extensions
func (em *ExtensionManager) AddLoader(loader Loader) {
	em.loaders = append(em.loaders, loader)
}

// GetLoaders returns all registered loaders
func (em *ExtensionManager) GetLoaders() []Loader {
	return em.loaders
}

// === Core Interfaces ===

// LoadTarget defines what can be loaded into
type LoadTarget interface {
	RegisterVerb(spec VerbSpec, executor VerbExecutor) error
	RegisterFunction(name string, fn interface{}) error
	LoadData(path string, value interface{}) error
	RegisterType(name string, typeDef TypeDefinition) error
}

// Loader defines the interface for extension loaders
type Loader interface {
	Name() string
	Load(ctx context.Context, target LoadTarget) error
}

// VerbSpec defines a verb specification interface
type VerbSpec interface {
	GetName() string
	GetDescription() string
	GetCapabilities() []string
	GetResources() []ResourceSpec
	GetArgTypes() map[string]string
	GetRequiredArgs() []string
	GetReturnType() string
	GetInverseVerb() string
}

// ResourceSpec defines resource requirements
type ResourceSpec interface {
	GetResource() string
	GetCapabilities() []string
}

// VerbExecutor is an alias for the core verb executor interface.
type VerbExecutor = verb.Executor

// TypeDefinition defines a type for the schema system
type TypeDefinition struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // "object", "array", "string", etc.
	Properties  interface{} `json:"properties,omitempty"`
	Description string      `json:"description,omitempty"`
}

// === Static Loaders ===

// StaticVerbLoader loads verbs from code (compile-time registration)
type StaticVerbLoader struct {
	name  string
	verbs []VerbDefinition
}

// VerbDefinition defines a verb that can be registered
type VerbDefinition struct {
	Spec     VerbSpec
	Executor VerbExecutor
}

// NewStaticVerbLoader creates a static verb loader
func NewStaticVerbLoader(name string, verbs []VerbDefinition) *StaticVerbLoader {
	return &StaticVerbLoader{
		name:  name,
		verbs: verbs,
	}
}

func (svl *StaticVerbLoader) Name() string {
	return fmt.Sprintf("StaticVerbs:%s", svl.name)
}

func (svl *StaticVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	for _, verbDef := range svl.verbs {
		if err := target.RegisterVerb(verbDef.Spec, verbDef.Executor); err != nil {
			return fmt.Errorf("registering verb %s: %w", verbDef.Spec.GetName(), err)
		}
	}
	return nil
}

// StaticSchemaLoader loads schemas and functions from code
type StaticSchemaLoader struct {
	name      string
	functions map[string]interface{}
	data      map[string]interface{}
	types     map[string]TypeDefinition
}

// NewStaticSchemaLoader creates a static schema loader
func NewStaticSchemaLoader(name string) *StaticSchemaLoader {
	return &StaticSchemaLoader{
		name:      name,
		functions: make(map[string]interface{}),
		data:      make(map[string]interface{}),
		types:     make(map[string]TypeDefinition),
	}
}

// AddFunction registers a function for expressions
func (ssl *StaticSchemaLoader) AddFunction(name string, fn interface{}) *StaticSchemaLoader {
	ssl.functions[name] = fn
	return ssl
}

// AddData registers data for fact access
func (ssl *StaticSchemaLoader) AddData(path string, value interface{}) *StaticSchemaLoader {
	ssl.data[path] = value
	return ssl
}

// AddType registers a type definition
func (ssl *StaticSchemaLoader) AddType(name string, typeDef TypeDefinition) *StaticSchemaLoader {
	ssl.types[name] = typeDef
	return ssl
}

func (ssl *StaticSchemaLoader) Name() string {
	return fmt.Sprintf("StaticSchema:%s", ssl.name)
}

func (ssl *StaticSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	// Load functions
	for name, fn := range ssl.functions {
		if err := target.RegisterFunction(name, fn); err != nil {
			return fmt.Errorf("registering function %s: %w", name, err)
		}
	}

	// Load data
	for path, value := range ssl.data {
		if err := target.LoadData(path, value); err != nil {
			return fmt.Errorf("loading data %s: %w", path, err)
		}
	}

	// Load types
	for name, typeDef := range ssl.types {
		if err := target.RegisterType(name, typeDef); err != nil {
			return fmt.Errorf("registering type %s: %w", name, err)
		}
	}

	return nil
}

// === Dynamic Loaders ===

// JSONVerbLoader loads verbs from JSON files
type JSONVerbLoader struct {
	name     string
	filePath string
}

// VerbManifest defines the structure for dynamic verb loading
type VerbManifest struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Verbs       []JSONVerbSpec         `json:"verbs"`
	Executors   map[string]interface{} `json:"executors,omitempty"`
}

// VerbTarget defines how a verb should be executed.
type VerbTarget struct {
	Type   string                 `json:"type"`
	Ref    string                 `json:"ref,omitempty"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// JSONVerbSpec defines a verb specification in JSON
type JSONVerbSpec struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Capabilities []string           `json:"capabilities"`
	Resources    []JSONResourceSpec `json:"resources"`
	ArgTypes     map[string]string  `json:"argTypes"`
	RequiredArgs []string           `json:"requiredArgs"`
	ReturnType   string             `json:"returnType"`
	InverseVerb  string             `json:"inverseVerb,omitempty"`
	Target       *VerbTarget        `json:"target,omitempty"`
}

func (jvs *JSONVerbSpec) GetName() string           { return jvs.Name }
func (jvs *JSONVerbSpec) GetDescription() string    { return jvs.Description }
func (jvs *JSONVerbSpec) GetCapabilities() []string { return jvs.Capabilities }
func (jvs *JSONVerbSpec) GetResources() []ResourceSpec {
	specs := make([]ResourceSpec, len(jvs.Resources))
	for i, r := range jvs.Resources {
		specs[i] = &r
	}
	return specs
}
func (jvs *JSONVerbSpec) GetArgTypes() map[string]string { return jvs.ArgTypes }
func (jvs *JSONVerbSpec) GetRequiredArgs() []string      { return jvs.RequiredArgs }
func (jvs *JSONVerbSpec) GetReturnType() string          { return jvs.ReturnType }
func (jvs *JSONVerbSpec) GetInverseVerb() string         { return jvs.InverseVerb }

// JSONResourceSpec defines resource requirements in JSON
type JSONResourceSpec struct {
	Resource     string   `json:"resource"`
	Capabilities []string `json:"capabilities"`
}

func (jrs *JSONResourceSpec) GetResource() string       { return jrs.Resource }
func (jrs *JSONResourceSpec) GetCapabilities() []string { return jrs.Capabilities }

// NewJSONVerbLoader creates a JSON verb loader from file
func NewJSONVerbLoader(name, filePath string) *JSONVerbLoader {
	return &JSONVerbLoader{
		name:     name,
		filePath: filePath,
	}
}

func (jvl *JSONVerbLoader) Name() string {
	return fmt.Sprintf("JSONVerbs:%s", jvl.name)
}

func (jvl *JSONVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	data, err := os.ReadFile(jvl.filePath)
	if err != nil {
		return fmt.Errorf("reading verb manifest: %w", err)
	}

	var manifest VerbManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing verb manifest: %w", err)
	}

	for _, verbSpec := range manifest.Verbs {
		targetType, targetConfig := resolveVerbTarget(&verbSpec)
		executor, err := jvl.createExecutor(targetType, targetConfig, verbSpec.Name)
		if err != nil {
			return fmt.Errorf("creating executor for %s: %w", verbSpec.Name, err)
		}

		if err := target.RegisterVerb(&verbSpec, executor); err != nil {
			return fmt.Errorf("registering verb %s: %w", verbSpec.Name, err)
		}
	}

	return nil
}

func (jvl *JSONVerbLoader) createExecutor(targetType string, config map[string]interface{}, verbName string) (VerbExecutor, error) {
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "mock":
		return &MockExecutor{Name: fmt.Sprintf("Mock:%s", jvl.name)}, nil
	case "noop":
		return &NoOpExecutor{}, nil
	case "http":
		return NewHTTPExecutor(config)
	case "grpc":
		return NewGRPCExecutor(config)
	case "stream", "message":
		return NewStreamExecutor(config)
	case "local":
		return nil, fmt.Errorf("local target requires in-process executor; use static loader for %s", verbName)
	case "oci":
		return NewOCIExecutor(verbName, config)
	default:
		return nil, fmt.Errorf("unsupported executor target: %s", targetType)
	}
}

func resolveVerbTarget(spec *JSONVerbSpec) (string, map[string]interface{}) {
	if spec == nil || spec.Target == nil {
		return "stream", map[string]interface{}{"publisher": "stdout"}
	}

	targetType := strings.TrimSpace(spec.Target.Type)
	config := map[string]interface{}{}
	for key, value := range spec.Target.Config {
		config[key] = value
	}
	if spec.Target.Ref != "" {
		config["ref"] = spec.Target.Ref
	}

	if targetType == "" {
		targetType = "stream"
	}
	if targetType == "stream" {
		if _, ok := config["publisher"]; !ok && len(config) == 0 {
			config["publisher"] = "stdout"
		}
	}

	return targetType, config
}

// JSONSchemaLoader loads schemas from JSON Schema files
type JSONSchemaLoader struct {
	name     string
	filePath string
}

// SchemaManifest defines the structure for dynamic schema loading
type SchemaManifest struct {
	Name        string                    `json:"name"`
	Version     string                    `json:"version"`
	Description string                    `json:"description"`
	Types       map[string]TypeDefinition `json:"types"`
	Functions   map[string]FunctionDef    `json:"functions"`
	InitialData map[string]interface{}    `json:"initialData,omitempty"`
}

// FunctionDef defines a function for dynamic loading
type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"` // "builtin", "expression", etc.
	Config      map[string]interface{} `json:"config,omitempty"`
}

// NewJSONSchemaLoader creates a JSON schema loader from file
func NewJSONSchemaLoader(name, filePath string) *JSONSchemaLoader {
	return &JSONSchemaLoader{
		name:     name,
		filePath: filePath,
	}
}

func (jsl *JSONSchemaLoader) Name() string {
	return fmt.Sprintf("JSONSchema:%s", jsl.name)
}

func (jsl *JSONSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	data, err := os.ReadFile(jsl.filePath)
	if err != nil {
		return fmt.Errorf("reading schema manifest: %w", err)
	}

	var manifest SchemaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing schema manifest: %w", err)
	}

	// Load initial data
	for path, value := range manifest.InitialData {
		if err := target.LoadData(path, value); err != nil {
			return fmt.Errorf("loading initial data %s: %w", path, err)
		}
	}

	// Load functions
	for _, funcDef := range manifest.Functions {
		if err := jsl.loadFunction(funcDef, target); err != nil {
			return fmt.Errorf("loading function %s: %w", funcDef.Name, err)
		}
	}

	// Load types
	for name, typeDef := range manifest.Types {
		if err := target.RegisterType(name, typeDef); err != nil {
			return fmt.Errorf("registering type %s: %w", name, err)
		}
	}

	return nil
}

func (jsl *JSONSchemaLoader) loadFunction(funcDef FunctionDef, target LoadTarget) error {
	switch funcDef.Type {
	case "builtin":
		return jsl.loadBuiltinFunction(funcDef, target)
	default:
		return fmt.Errorf("unsupported function type: %s", funcDef.Type)
	}
}

func (jsl *JSONSchemaLoader) loadBuiltinFunction(funcDef FunctionDef, target LoadTarget) error {
	switch funcDef.Name {
	case "length":
		return target.RegisterFunction("length", func(s string) int { return len(s) })
	case "upper":
		return target.RegisterFunction("upper", strings.ToUpper)
	case "lower":
		return target.RegisterFunction("lower", strings.ToLower)
	default:
		return fmt.Errorf("unknown builtin function: %s", funcDef.Name)
	}
}

// === Protocol Buffer Loaders ===

// ProtoVerbLoader loads verbs from Protocol Buffer messages
type ProtoVerbLoader struct {
	name    string
	message proto.Message
}

// NewProtoVerbLoader creates a protobuf verb loader
func NewProtoVerbLoader(name string, message proto.Message) *ProtoVerbLoader {
	return &ProtoVerbLoader{
		name:    name,
		message: message,
	}
}

func (pvl *ProtoVerbLoader) Name() string {
	return fmt.Sprintf("ProtoVerbs:%s", pvl.name)
}

func (pvl *ProtoVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	// Convert protobuf to JSON and delegate to JSON loader
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	data, err := marshaler.Marshal(pvl.message)
	if err != nil {
		return fmt.Errorf("marshaling proto message: %w", err)
	}

	// Create temporary JSON loader
	tempFile, err := os.CreateTemp("", "verbs-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	jsonLoader := NewJSONVerbLoader(pvl.name, tempFile.Name())
	return jsonLoader.Load(ctx, target)
}

// ProtoSchemaLoader loads schemas from Protocol Buffer messages
type ProtoSchemaLoader struct {
	name    string
	message proto.Message
}

// NewProtoSchemaLoader creates a protobuf schema loader
func NewProtoSchemaLoader(name string, message proto.Message) *ProtoSchemaLoader {
	return &ProtoSchemaLoader{
		name:    name,
		message: message,
	}
}

func (psl *ProtoSchemaLoader) Name() string {
	return fmt.Sprintf("ProtoSchema:%s", psl.name)
}

func (psl *ProtoSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	// Convert protobuf to JSON and delegate to JSON loader
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	data, err := marshaler.Marshal(psl.message)
	if err != nil {
		return fmt.Errorf("marshaling proto message: %w", err)
	}

	// Create temporary JSON loader
	tempFile, err := os.CreateTemp("", "schema-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	jsonLoader := NewJSONSchemaLoader(psl.name, tempFile.Name())
	return jsonLoader.Load(ctx, target)
}

// === OCI Bundle Loader ===

// OCIBundleLoader loads extensions from OCI registry bundles
type OCIBundleLoader struct {
	name string
	ref  string // OCI reference like "registry.io/my-effectus-bundle:v1.0.0"
}

// NewOCIBundleLoader creates an OCI bundle loader
func NewOCIBundleLoader(name, ref string) *OCIBundleLoader {
	return &OCIBundleLoader{
		name: name,
		ref:  ref,
	}
}

func (obl *OCIBundleLoader) Name() string {
	return fmt.Sprintf("OCIBundle:%s", obl.name)
}

func (obl *OCIBundleLoader) Load(ctx context.Context, target LoadTarget) error {
	loaders, cleanup, err := loadOCIBundleLoaders(obl.ref)
	if err != nil {
		return err
	}
	defer cleanup()

	for _, loader := range loaders {
		if err := loader.Load(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func loadOCIBundleLoaders(ref string) ([]Loader, func(), error) {
	if strings.TrimSpace(ref) == "" {
		return nil, func() {}, fmt.Errorf("oci ref is required")
	}
	dir, err := os.MkdirTemp("", "effectus-oci-*")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	if err := pullOCIExtensionBundle(ref, dir); err != nil {
		cleanup()
		return nil, func() {}, err
	}

	loaders, err := LoadFromDirectory(dir)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return loaders, cleanup, nil
}

func pullOCIExtensionBundle(ref string, outputDir string) error {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing oci ref: %w", err)
	}

	image, err := remote.Image(parsed, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	layers, err := image.Layers()
	if err != nil {
		return fmt.Errorf("getting layers: %w", err)
	}
	if len(layers) == 0 {
		return fmt.Errorf("image has no layers")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	// Extract all layers except the last one (bundle metadata).
	for i, layer := range layers {
		if i == len(layers)-1 {
			break
		}
		rc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("getting layer %d: %w", i, err)
		}
		if err := extractTarLayer(rc, outputDir); err != nil {
			rc.Close()
			return fmt.Errorf("extracting layer %d: %w", i, err)
		}
		rc.Close()
	}
	return nil
}

type captureTarget struct {
	verbs map[string]VerbExecutor
}

func newCaptureTarget() *captureTarget {
	return &captureTarget{verbs: make(map[string]VerbExecutor)}
}

func (ct *captureTarget) RegisterVerb(spec VerbSpec, executor VerbExecutor) error {
	if spec == nil || executor == nil {
		return nil
	}
	ct.verbs[spec.GetName()] = executor
	return nil
}

func (ct *captureTarget) RegisterFunction(name string, fn interface{}) error {
	return nil
}

func (ct *captureTarget) LoadData(path string, value interface{}) error {
	return nil
}

func (ct *captureTarget) RegisterType(name string, typeDef TypeDefinition) error {
	return nil
}

// === Utility Executors ===

// MockExecutor provides a simple mock executor for testing
type MockExecutor struct {
	Name string
}

func (me *MockExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"executor": me.Name,
		"args":     args,
		"result":   "mock_success",
	}, nil
}

// NoOpExecutor provides a no-operation executor
type NoOpExecutor struct{}

func (noe *NoOpExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{"status": "noop"}, nil
}

// HTTPExecutor executes verbs via HTTP calls
type HTTPExecutor struct {
	URL     string
	Method  string
	Headers map[string]string
	Timeout time.Duration
	Client  *http.Client
}

func NewHTTPExecutor(config map[string]interface{}) (*HTTPExecutor, error) {
	url, ok := config["url"].(string)
	if !ok {
		return nil, fmt.Errorf("http executor requires 'url' config")
	}

	method := "POST"
	if m, ok := config["method"].(string); ok {
		method = m
	}

	headers := make(map[string]string)
	if h, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}

	timeout := 5 * time.Second
	if raw, ok := config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}

	return &HTTPExecutor{
		URL:     url,
		Method:  method,
		Headers: headers,
		Timeout: timeout,
	}, nil
}

func (he *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, he.Method, he.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range he.Headers {
		req.Header.Set(key, value)
	}

	client := he.Client
	if client == nil {
		client = &http.Client{Timeout: he.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if len(body) == 0 {
		return true, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err == nil {
		return decoded, nil
	}

	return strings.TrimSpace(string(body)), nil
}

// GRPCExecutor executes verbs via gRPC calls.
type GRPCExecutor struct {
	Address  string
	Method   string
	Timeout  time.Duration
	Metadata map[string]string
	UseTLS   bool
	conn     *grpc.ClientConn
}

func NewGRPCExecutor(config map[string]interface{}) (*GRPCExecutor, error) {
	address, _ := config["address"].(string)
	if address == "" {
		return nil, fmt.Errorf("grpc executor requires 'address' config")
	}
	method, _ := config["method"].(string)
	if method == "" {
		return nil, fmt.Errorf("grpc executor requires 'method' config")
	}
	timeout := 10 * time.Second
	if raw, ok := config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	metadata := make(map[string]string)
	if raw, ok := config["metadata"].(map[string]interface{}); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok {
				metadata[k] = s
			}
		}
	}
	useTLS := false
	if raw, ok := config["useTLS"].(bool); ok {
		useTLS = raw
	}

	return &GRPCExecutor{
		Address:  address,
		Method:   method,
		Timeout:  timeout,
		Metadata: metadata,
		UseTLS:   useTLS,
	}, nil
}

func (ge *GRPCExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if ge.conn == nil {
		var opts []grpc.DialOption
		if ge.UseTLS {
			opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
		} else {
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}
		conn, err := grpc.Dial(ge.Address, opts...)
		if err != nil {
			return nil, fmt.Errorf("grpc dial: %w", err)
		}
		ge.conn = conn
	}

	req, err := structpb.NewStruct(args)
	if err != nil {
		return nil, fmt.Errorf("grpc encode args: %w", err)
	}

	callCtx := ctx
	if ge.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, ge.Timeout)
		defer cancel()
	}

	if len(ge.Metadata) > 0 {
		md := metadataFromMap(ge.Metadata)
		callCtx = metadata.NewOutgoingContext(callCtx, md)
	}

	var resp structpb.Struct
	if err := grpc.Invoke(callCtx, ge.Method, req, &resp, ge.conn); err != nil {
		return nil, err
	}
	return resp.AsMap(), nil
}

func metadataFromMap(values map[string]string) metadata.MD {
	md := metadata.MD{}
	for key, value := range values {
		md[key] = []string{value}
	}
	return md
}

// StreamExecutor emits verbs to a stream publisher.
type StreamExecutor struct {
	publisher streamPublisher
}

type streamPublisher interface {
	Publish(ctx context.Context, payload []byte) error
}

type stdoutPublisher struct{}

func (sp *stdoutPublisher) Publish(ctx context.Context, payload []byte) error {
	_, _ = fmt.Printf("stream.emit %s\n", string(payload))
	return nil
}

type httpStreamPublisher struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func (hp *httpStreamPublisher) Publish(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hp.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range hp.headers {
		req.Header.Set(key, value)
	}
	resp, err := hp.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stream http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type kafkaStreamPublisher struct {
	writer *kafka.Writer
}

func (kp *kafkaStreamPublisher) Publish(ctx context.Context, payload []byte) error {
	return kp.writer.WriteMessages(ctx, kafka.Message{
		Value: payload,
		Time:  time.Now(),
	})
}

func NewStreamExecutor(config map[string]interface{}) (*StreamExecutor, error) {
	publisher := "stdout"
	if raw, ok := config["publisher"].(string); ok && raw != "" {
		publisher = raw
	}
	switch strings.ToLower(publisher) {
	case "stdout":
		return &StreamExecutor{publisher: &stdoutPublisher{}}, nil
	case "http":
		url, _ := config["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("stream http publisher requires url")
		}
		headers := map[string]string{}
		if raw, ok := config["headers"].(map[string]interface{}); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
		timeout := 5 * time.Second
		if raw, ok := config["timeout"].(string); ok {
			if parsed, err := time.ParseDuration(raw); err == nil {
				timeout = parsed
			}
		}
		client := &http.Client{Timeout: timeout}
		return &StreamExecutor{publisher: &httpStreamPublisher{url: url, headers: headers, client: client}}, nil
	case "kafka":
		var brokers []string
		if raw, ok := config["brokers"].([]interface{}); ok {
			for _, entry := range raw {
				if s, ok := entry.(string); ok {
					brokers = append(brokers, s)
				}
			}
		}
		if raw, ok := config["brokers"].([]string); ok {
			brokers = append(brokers, raw...)
		}
		if len(brokers) == 0 {
			return nil, fmt.Errorf("stream kafka publisher requires brokers")
		}
		topic, _ := config["topic"].(string)
		if topic == "" {
			return nil, fmt.Errorf("stream kafka publisher requires topic")
		}
		writer := &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		}
		return &StreamExecutor{publisher: &kafkaStreamPublisher{writer: writer}}, nil
	default:
		return nil, fmt.Errorf("unsupported stream publisher: %s", publisher)
	}
}

func (se *StreamExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}
	if err := se.publisher.Publish(ctx, payload); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "queued"}, nil
}

// OCIExecutor resolves a verb executor from an OCI extension bundle.
type OCIExecutor struct {
	ref      string
	verbName string
	once     sync.Once
	executor VerbExecutor
	err      error
}

func NewOCIExecutor(verbName string, config map[string]interface{}) (*OCIExecutor, error) {
	ref, _ := config["ref"].(string)
	if ref == "" {
		return nil, fmt.Errorf("oci target requires ref")
	}
	if raw, ok := config["verb"].(string); ok && raw != "" {
		verbName = raw
	}
	return &OCIExecutor{ref: ref, verbName: verbName}, nil
}

func (oe *OCIExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	oe.once.Do(func() {
		executors, err := loadOCIBundleExecutors(ctx, oe.ref)
		if err != nil {
			oe.err = err
			return
		}
		executor, ok := executors[oe.verbName]
		if !ok {
			oe.err = fmt.Errorf("verb %s not found in oci bundle", oe.verbName)
			return
		}
		oe.executor = executor
	})
	if oe.err != nil {
		return nil, oe.err
	}
	if oe.executor == nil {
		return nil, fmt.Errorf("oci executor not initialized")
	}
	return oe.executor.Execute(ctx, args)
}

func loadOCIBundleExecutors(ctx context.Context, ref string) (map[string]VerbExecutor, error) {
	loaders, cleanup, err := loadOCIBundleLoaders(ref)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	target := newCaptureTarget()
	for _, loader := range loaders {
		if err := loader.Load(ctx, target); err != nil {
			return nil, err
		}
	}
	return target.verbs, nil
}

// === Directory Loaders ===

// LoadFromDirectory scans a directory for extension files and creates loaders
func LoadFromDirectory(dirPath string) ([]Loader, error) {
	var loaders []Loader

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		switch {
		case strings.HasSuffix(path, ".verbs.json"):
			name := filepath.Base(path[:len(path)-11]) // Remove .verbs.json
			loaders = append(loaders, NewJSONVerbLoader(name, path))
		case strings.HasSuffix(path, ".schema.json"):
			name := filepath.Base(path[:len(path)-12]) // Remove .schema.json
			loaders = append(loaders, NewJSONSchemaLoader(name, path))
		}

		return nil
	})

	return loaders, err
}

// === Helper Functions ===

// LoadExtensionsFromReader loads extensions from an io.Reader (for testing)
func LoadExtensionsFromReader(r io.Reader, extension string) (Loader, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Write to temp file and create loader
	tempFile, err := os.CreateTemp("", "*"+extension)
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return nil, err
	}

	switch extension {
	case ".verbs.json":
		return NewJSONVerbLoader("temp", tempFile.Name()), nil
	case ".schema.json":
		return NewJSONSchemaLoader("temp", tempFile.Name()), nil
	default:
		return nil, fmt.Errorf("unsupported extension: %s", extension)
	}
}

// extractTarLayer extracts files from a tar stream to the target directory.
func extractTarLayer(r io.Reader, targetDir string) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		if header.FileInfo().IsDir() {
			continue
		}

		targetPath := filepath.Join(targetDir, header.Name)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", targetPath, err)
		}

		file, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("creating file %s: %w", targetPath, err)
		}

		if _, err := io.Copy(file, tr); err != nil {
			file.Close()
			return fmt.Errorf("writing to %s: %w", targetPath, err)
		}

		if err := file.Close(); err != nil {
			return fmt.Errorf("closing file %s: %w", targetPath, err)
		}
	}

	return nil
}
