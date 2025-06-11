package unified

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"github.com/effectus/effectus-go/schema"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// DynamicSystem orchestrates the entire Effectus runtime with proto-first architecture
type DynamicSystem struct {
	// Schema management
	schemaRegistry *schema.BufSchemaRegistry
	factRegistry   *schema.FactSchemaRegistry
	verbRegistry   *schema.VerbSchemaRegistry

	// Fact sourcing
	factSources    map[string]FactSource
	factNormalizer *FactNormalizer

	// Dynamic compilation
	ruleCompiler *RuleCompiler
	ruleRegistry *atomic.Value // *RuleRegistry

	// Execution
	coreEvaluator  *CoreEvaluator
	effectExecutor *EffectExecutor

	// Hot reload
	hotLoader *HotLoader

	// Metrics and observability
	metrics *SystemMetrics
}

// FactSource represents any source that can provide typed facts
type FactSource interface {
	// Subscribe to facts with schema validation
	Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error)

	// Get schema information for this source
	GetSourceSchema() *effectusv1.Schema

	// Health check
	HealthCheck() error

	// Source metadata
	GetMetadata() SourceMetadata
}

// TypedFact represents a fact with full schema information
type TypedFact struct {
	SchemaName    string
	SchemaVersion string
	Data          proto.Message
	Timestamp     time.Time
	SourceID      string
	TraceID       string
	Metadata      map[string]string
}

// SourceMetadata provides information about fact sources
type SourceMetadata struct {
	SourceType    string   // "kafka", "http", "database", "file"
	Capabilities  []string // ["streaming", "batch", "realtime"]
	SchemaFormats []string // ["protobuf", "json", "avro"]
	Config        map[string]string
}

// FactNormalizer converts from various formats to typed proto messages
type FactNormalizer struct {
	schemaRegistry *schema.BufSchemaRegistry
	converters     map[string]FormatConverter
}

// FormatConverter handles conversion from external formats to proto
type FormatConverter interface {
	Convert(rawData []byte, targetSchema *effectusv1.Schema) (proto.Message, error)
	CanConvert(sourceFormat, targetFormat string) bool
}

// RuleCompiler dynamically compiles rules against current schemas
type RuleCompiler struct {
	schemaRegistry *schema.BufSchemaRegistry
	verbRegistry   *schema.VerbSchemaRegistry
	factRegistry   *schema.FactSchemaRegistry

	// Compilation cache
	compilationCache *CompilationCache
}

// CompiledRuleset represents a fully compiled and validated ruleset
type CompiledRuleset struct {
	Name           string
	Version        string
	SchemaVersions map[string]string // fact_type -> schema_version
	Rules          []*CompiledRule
	Dependencies   []string
	Capabilities   []string
	CreatedAt      time.Time
}

// CompiledRule represents a single compiled rule
type CompiledRule struct {
	Name       string
	Type       RuleType
	Predicates []*TypedPredicate
	Effects    []*TypedEffect
	Priority   int32
	SchemaHash string // For cache invalidation
}

// TypedPredicate represents a predicate with schema validation
type TypedPredicate struct {
	FactType      string
	SchemaVersion string
	Path          string
	Operator      string
	Value         *anypb.Any
	Validator     PredicateValidator
}

// TypedEffect represents an effect with schema validation
type TypedEffect struct {
	VerbName         string
	InterfaceVersion string
	Args             map[string]*anypb.Any
	Validator        EffectValidator
	Capability       string
}

// PredicateValidator validates predicate execution
type PredicateValidator interface {
	Validate(fact proto.Message) (bool, error)
	GetRequiredSchema() *effectusv1.Schema
}

// EffectValidator validates effect execution
type EffectValidator interface {
	ValidateArgs(args map[string]*anypb.Any) error
	GetRequiredSchema() *effectusv1.Schema
}

// CoreEvaluator handles pure rule evaluation (no I/O)
type CoreEvaluator struct {
	ruleRegistry *atomic.Value // *RuleRegistry
	metrics      *EvaluationMetrics
}

// EffectExecutor handles I/O and external interactions
type EffectExecutor struct {
	verbExecutors map[string]VerbExecutor
	capabilities  map[string]bool
	lockManager   LockManager
	sagaManager   SagaManager
}

// VerbExecutor handles execution of specific verbs
type VerbExecutor interface {
	Execute(ctx context.Context, effect *TypedEffect) (*VerbResult, error)
	GetInterface() *effectusv1.VerbInterface
	HealthCheck() error
}

// HotLoader manages dynamic updates
type HotLoader struct {
	schemaRegistry *schema.BufSchemaRegistry
	ruleCompiler   *RuleCompiler
	updateChan     chan *UpdateEvent
}

// UpdateEvent represents a schema or rule update
type UpdateEvent struct {
	Type       UpdateType
	SchemaName string
	Version    string
	Data       proto.Message
	Timestamp  time.Time
}

type UpdateType int

const (
	UpdateTypeSchemaChange UpdateType = iota
	UpdateTypeRuleChange
	UpdateTypeVerbInterface
)

// NewDynamicSystem creates a new dynamic system instance
func NewDynamicSystem(config *SystemConfig) (*DynamicSystem, error) {
	schemaRegistry, err := schema.NewBufSchemaRegistry(config.BufConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create schema registry: %w", err)
	}

	system := &DynamicSystem{
		schemaRegistry: schemaRegistry,
		factRegistry:   schema.NewFactSchemaRegistry(schemaRegistry),
		verbRegistry:   schema.NewVerbSchemaRegistry(schemaRegistry),
		factSources:    make(map[string]FactSource),
		ruleRegistry:   &atomic.Value{},
		metrics:        NewSystemMetrics(),
	}

	// Initialize components
	system.factNormalizer = NewFactNormalizer(schemaRegistry)
	system.ruleCompiler = NewRuleCompiler(schemaRegistry, system.verbRegistry, system.factRegistry)
	system.coreEvaluator = NewCoreEvaluator(system.ruleRegistry)
	system.effectExecutor = NewEffectExecutor(config.Capabilities)
	system.hotLoader = NewHotLoader(schemaRegistry, system.ruleCompiler)

	return system, nil
}

// Start initializes and starts the dynamic system
func (ds *DynamicSystem) Start(ctx context.Context) error {
	// Start schema registry
	if err := ds.schemaRegistry.Start(ctx); err != nil {
		return fmt.Errorf("failed to start schema registry: %w", err)
	}

	// Start fact sources
	for name, source := range ds.factSources {
		if err := ds.startFactSource(ctx, name, source); err != nil {
			return fmt.Errorf("failed to start fact source %s: %w", name, err)
		}
	}

	// Start hot loader
	go ds.hotLoader.Run(ctx)

	// Initial rule compilation
	if err := ds.recompileRules(ctx); err != nil {
		return fmt.Errorf("failed initial rule compilation: %w", err)
	}

	return nil
}

// RegisterFactSource adds a new fact source
func (ds *DynamicSystem) RegisterFactSource(name string, source FactSource) error {
	// Validate source schema compatibility
	sourceSchema := source.GetSourceSchema()
	if err := ds.factRegistry.ValidateSchema(sourceSchema); err != nil {
		return fmt.Errorf("invalid source schema: %w", err)
	}

	ds.factSources[name] = source
	return nil
}

// ExecuteRules processes facts through the rule engine
func (ds *DynamicSystem) ExecuteRules(ctx context.Context, facts []*TypedFact) ([]*EffectResult, error) {
	// Get current rule registry
	ruleRegistry := ds.ruleRegistry.Load().(*RuleRegistry)
	if ruleRegistry == nil {
		return nil, fmt.Errorf("no rules compiled")
	}

	// Core evaluation (pure, no I/O)
	effects, err := ds.coreEvaluator.Evaluate(ctx, facts, ruleRegistry)
	if err != nil {
		return nil, fmt.Errorf("rule evaluation failed: %w", err)
	}

	// Effect execution (I/O, external calls)
	results, err := ds.effectExecutor.ExecuteEffects(ctx, effects)
	if err != nil {
		return nil, fmt.Errorf("effect execution failed: %w", err)
	}

	return results, nil
}

// UpdateSchema handles dynamic schema updates
func (ds *DynamicSystem) UpdateSchema(ctx context.Context, schemaUpdate *SchemaUpdate) error {
	// Validate breaking changes
	if err := ds.schemaRegistry.ValidateUpdate(schemaUpdate); err != nil {
		return fmt.Errorf("schema update validation failed: %w", err)
	}

	// Apply schema update
	if err := ds.schemaRegistry.ApplyUpdate(schemaUpdate); err != nil {
		return fmt.Errorf("failed to apply schema update: %w", err)
	}

	// Trigger rule recompilation
	if err := ds.recompileRules(ctx); err != nil {
		return fmt.Errorf("rule recompilation failed: %w", err)
	}

	return nil
}

// recompileRules recompiles all rules against current schemas
func (ds *DynamicSystem) recompileRules(ctx context.Context) error {
	newRegistry, err := ds.ruleCompiler.CompileAll(ctx)
	if err != nil {
		return err
	}

	// Atomic swap
	ds.ruleRegistry.Store(newRegistry)

	return nil
}

// startFactSource initializes and starts a fact source
func (ds *DynamicSystem) startFactSource(ctx context.Context, name string, source FactSource) error {
	// Get fact types this source provides
	metadata := source.GetMetadata()
	factTypes := ds.getFactTypesForSource(metadata)

	// Subscribe to facts
	factChan, err := source.Subscribe(ctx, factTypes)
	if err != nil {
		return err
	}

	// Start processing facts
	go ds.processFacts(ctx, name, factChan)

	return nil
}

// processFacts handles incoming facts from a source
func (ds *DynamicSystem) processFacts(ctx context.Context, sourceName string, factChan <-chan *TypedFact) {
	for {
		select {
		case <-ctx.Done():
			return
		case fact := <-factChan:
			if err := ds.processSingleFact(ctx, fact); err != nil {
				ds.metrics.RecordFactProcessingError(sourceName, err)
			}
		}
	}
}

// processSingleFact processes a single fact through the system
func (ds *DynamicSystem) processSingleFact(ctx context.Context, fact *TypedFact) error {
	// Validate fact against schema
	if err := ds.factRegistry.ValidateFact(fact); err != nil {
		return fmt.Errorf("fact validation failed: %w", err)
	}

	// Execute rules
	results, err := ds.ExecuteRules(ctx, []*TypedFact{fact})
	if err != nil {
		return fmt.Errorf("rule execution failed: %w", err)
	}

	// Record metrics
	ds.metrics.RecordFactProcessed(fact.SchemaName, len(results))

	return nil
}

// getFactTypesForSource determines what fact types a source can provide
func (ds *DynamicSystem) getFactTypesForSource(metadata SourceMetadata) []string {
	// This would be implemented based on source capabilities
	// and registered fact schemas
	return []string{"user_profile", "system_event"} // placeholder
}

// System configuration
type SystemConfig struct {
	BufConfig    *schema.BufConfig
	Capabilities map[string]bool
	Sources      map[string]SourceConfig
}

type SourceConfig struct {
	Type   string
	Config map[string]interface{}
}

// Additional types for completeness
type RuleRegistry struct {
	Rulesets map[string]*CompiledRuleset
	Version  string
}

type CompilationCache struct {
	// Implementation details
}

type SchemaUpdate struct {
	// Implementation details
}

type LockManager interface {
	// Implementation details
}

type SagaManager interface {
	// Implementation details
}

type VerbResult struct {
	// Implementation details
}

type EffectResult struct {
	// Implementation details
}

type SystemMetrics struct {
	// Implementation details
}

type EvaluationMetrics struct {
	// Implementation details
}

// Factory functions (placeholders)
func NewFactNormalizer(registry *schema.BufSchemaRegistry) *FactNormalizer {
	return &FactNormalizer{schemaRegistry: registry}
}

func NewRuleCompiler(schemaReg *schema.BufSchemaRegistry, verbReg *schema.VerbSchemaRegistry, factReg *schema.FactSchemaRegistry) *RuleCompiler {
	return &RuleCompiler{
		schemaRegistry: schemaReg,
		verbRegistry:   verbReg,
		factRegistry:   factReg,
	}
}

func NewCoreEvaluator(ruleRegistry *atomic.Value) *CoreEvaluator {
	return &CoreEvaluator{ruleRegistry: ruleRegistry}
}

func NewEffectExecutor(capabilities map[string]bool) *EffectExecutor {
	return &EffectExecutor{capabilities: capabilities}
}

func NewHotLoader(schemaRegistry *schema.BufSchemaRegistry, compiler *RuleCompiler) *HotLoader {
	return &HotLoader{
		schemaRegistry: schemaRegistry,
		ruleCompiler:   compiler,
	}
}

func NewSystemMetrics() *SystemMetrics {
	return &SystemMetrics{}
}

func (fn *FactNormalizer) Normalize(rawFact []byte, sourceFormat string, targetSchema *effectusv1.Schema) (*TypedFact, error) {
	// Implementation details
	return nil, nil
}

func (rc *RuleCompiler) CompileAll(ctx context.Context) (*RuleRegistry, error) {
	// Implementation details
	return nil, nil
}

func (ce *CoreEvaluator) Evaluate(ctx context.Context, facts []*TypedFact, registry *RuleRegistry) ([]*TypedEffect, error) {
	// Implementation details
	return nil, nil
}

func (ee *EffectExecutor) ExecuteEffects(ctx context.Context, effects []*TypedEffect) ([]*EffectResult, error) {
	// Implementation details
	return nil, nil
}

func (hl *HotLoader) Run(ctx context.Context) {
	// Implementation details
}

func (sm *SystemMetrics) RecordFactProcessingError(sourceName string, err error) {
	// Implementation details
}

func (sm *SystemMetrics) RecordFactProcessed(schemaName string, effectCount int) {
	// Implementation details
}

func (fr *schema.FactSchemaRegistry) ValidateSchema(schema *effectusv1.Schema) error {
	// Implementation details
	return nil
}

func (fr *schema.FactSchemaRegistry) ValidateFact(fact *TypedFact) error {
	// Implementation details
	return nil
}

func (sr *schema.BufSchemaRegistry) Start(ctx context.Context) error {
	// Implementation details
	return nil
}

func (sr *schema.BufSchemaRegistry) ValidateUpdate(update *SchemaUpdate) error {
	// Implementation details
	return nil
}

func (sr *schema.BufSchemaRegistry) ApplyUpdate(update *SchemaUpdate) error {
	// Implementation details
	return nil
}

type RuleType int

const (
	RuleTypeList RuleType = iota
	RuleTypeFlow
)
