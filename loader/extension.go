package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go/schema/verb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

// JSONVerbSpec defines a verb specification in JSON
type JSONVerbSpec struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Capabilities   []string               `json:"capabilities"`
	Resources      []JSONResourceSpec     `json:"resources"`
	ArgTypes       map[string]string      `json:"argTypes"`
	RequiredArgs   []string               `json:"requiredArgs"`
	ReturnType     string                 `json:"returnType"`
	InverseVerb    string                 `json:"inverseVerb,omitempty"`
	ExecutorType   string                 `json:"executorType"`
	ExecutorConfig map[string]interface{} `json:"executorConfig,omitempty"`
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
		executor, err := jvl.createExecutor(verbSpec.ExecutorType, verbSpec.ExecutorConfig)
		if err != nil {
			return fmt.Errorf("creating executor for %s: %w", verbSpec.Name, err)
		}

		if err := target.RegisterVerb(&verbSpec, executor); err != nil {
			return fmt.Errorf("registering verb %s: %w", verbSpec.Name, err)
		}
	}

	return nil
}

func (jvl *JSONVerbLoader) createExecutor(executorType string, config map[string]interface{}) (VerbExecutor, error) {
	switch executorType {
	case "mock":
		return &MockExecutor{Name: fmt.Sprintf("Mock:%s", jvl.name)}, nil
	case "noop":
		return &NoOpExecutor{}, nil
	case "http":
		return NewHTTPExecutor(config)
	default:
		return nil, fmt.Errorf("unsupported executor type: %s", executorType)
	}
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
	// TODO: Implement OCI bundle pulling using oras-go
	// For now, return a placeholder
	return fmt.Errorf("OCI bundle loading not yet implemented for ref: %s", obl.ref)
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

	return &HTTPExecutor{
		URL:     url,
		Method:  method,
		Headers: headers,
	}, nil
}

func (he *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// TODO: Implement actual HTTP execution
	return map[string]interface{}{
		"executor": "HTTP",
		"url":      he.URL,
		"method":   he.Method,
		"args":     args,
		"result":   "http_placeholder",
	}, nil
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
