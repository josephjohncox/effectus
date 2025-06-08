package runtime

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/effectus/effectus-go/compiler"
)

// RulesetExecutionServer provides a dynamic gRPC interface for ruleset execution
type RulesetExecutionServer struct {
	runtime       *ExecutionRuntime
	server        *grpc.Server
	listener      net.Listener
	rulesets      map[string]*CompiledRuleset
	services      map[string]*RulesetService
	mu            sync.RWMutex
	hotReload     bool
	reloadChecker *time.Ticker
}

// CompiledRuleset represents a compiled and validated ruleset ready for execution
type CompiledRuleset struct {
	Name          string
	Version       string
	Description   string
	FactSchema    *Schema
	EffectSchemas map[string]*Schema
	Rules         []CompiledRule
	Verbs         map[string]*compiler.CompiledVerbSpec
	Dependencies  []string
	Capabilities  []string
	Metadata      map[string]string
}

// CompiledRule represents a compiled rule within a ruleset
type CompiledRule struct {
	Name        string
	Type        RuleType // List or Flow
	Predicates  []CompiledPredicate
	Effects     []CompiledEffect
	Priority    int
	Description string
}

// RuleType defines the type of rule
type RuleType string

const (
	RuleTypeList RuleType = "list"
	RuleTypeFlow RuleType = "flow"
)

// CompiledPredicate represents a compiled predicate
type CompiledPredicate struct {
	Path     string
	Operator string
	Value    interface{}
}

// CompiledEffect represents a compiled effect
type CompiledEffect struct {
	VerbName string
	Args     map[string]interface{}
}

// Schema represents type information for facts or effects
type Schema struct {
	Name        string
	Fields      map[string]*FieldType
	Required    []string
	Description string
}

// FieldType represents a field type in a schema
type FieldType struct {
	Type        string // "string", "int32", "float64", "bool", "message", "repeated"
	MessageType string // For message types
	Required    bool
	Description string
}

// RulesetService handles gRPC calls for a specific ruleset
type RulesetService struct {
	ruleset *CompiledRuleset
	runtime *ExecutionRuntime
}

// ExecutionRequest represents a typed request to execute rules
type ExecutionRequest struct {
	RulesetName string
	Version     string
	Facts       *anypb.Any
	Options     *ExecutionOptions
	TraceID     string
}

// ExecutionOptions provides options for rule execution
type ExecutionOptions struct {
	DryRun           bool
	MaxEffects       int32
	TimeoutSeconds   int32
	EnableTracing    bool
	CapabilityFilter []string
}

// ExecutionResponse represents the typed response from rule execution
type ExecutionResponse struct {
	Success     bool
	Effects     []*TypedEffect
	ExecutionID string
	Duration    *timestamppb.Timestamp
	Metadata    map[string]string
	Errors      []string
	Warnings    []string
}

// TypedEffect represents an effect with type information
type TypedEffect struct {
	VerbName  string
	Args      *anypb.Any
	Result    *anypb.Any
	Timestamp *timestamppb.Timestamp
	Status    EffectStatus
}

// EffectStatus represents the execution status of an effect
type EffectStatus string

const (
	EffectStatusPending  EffectStatus = "pending"
	EffectStatusExecuted EffectStatus = "executed"
	EffectStatusFailed   EffectStatus = "failed"
	EffectStatusSkipped  EffectStatus = "skipped"
	EffectStatusRetrying EffectStatus = "retrying"
)

// NewRulesetExecutionServer creates a new dynamic gRPC server
func NewRulesetExecutionServer(runtime *ExecutionRuntime, addr string) (*RulesetExecutionServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	server := grpc.NewServer()

	// Enable reflection for dynamic service discovery
	reflection.Register(server)

	rulesetServer := &RulesetExecutionServer{
		runtime:  runtime,
		server:   server,
		listener: listener,
		rulesets: make(map[string]*CompiledRuleset),
		services: make(map[string]*RulesetService),
	}

	return rulesetServer, nil
}

// Start starts the gRPC server
func (res *RulesetExecutionServer) Start() error {
	log.Printf("Starting Effectus gRPC server on %s", res.listener.Addr().String())
	return res.server.Serve(res.listener)
}

// Stop gracefully stops the gRPC server
func (res *RulesetExecutionServer) Stop() {
	log.Println("Stopping Effectus gRPC server...")
	res.server.GracefulStop()

	if res.hotReload && res.reloadChecker != nil {
		res.reloadChecker.Stop()
	}
}

// RegisterRuleset dynamically registers a compiled ruleset as a gRPC service
func (res *RulesetExecutionServer) RegisterRuleset(ruleset *CompiledRuleset) error {
	res.mu.Lock()
	defer res.mu.Unlock()

	// Validate ruleset
	if err := res.validateRuleset(ruleset); err != nil {
		return fmt.Errorf("ruleset validation failed: %w", err)
	}

	// Create service for the ruleset
	service := &RulesetService{
		ruleset: ruleset,
		runtime: res.runtime,
	}

	// Register the service with gRPC server
	if err := res.registerGRPCService(ruleset.Name, service); err != nil {
		return fmt.Errorf("failed to register gRPC service: %w", err)
	}

	res.rulesets[ruleset.Name] = ruleset
	res.services[ruleset.Name] = service

	log.Printf("Registered ruleset '%s' (version %s) with %d rules, %d verbs",
		ruleset.Name, ruleset.Version, len(ruleset.Rules), len(ruleset.Verbs))

	return nil
}

// UnregisterRuleset removes a ruleset and its gRPC service
func (res *RulesetExecutionServer) UnregisterRuleset(rulesetName string) error {
	res.mu.Lock()
	defer res.mu.Unlock()

	if _, exists := res.rulesets[rulesetName]; !exists {
		return fmt.Errorf("ruleset '%s' not registered", rulesetName)
	}

	// Unregister gRPC service
	if err := res.unregisterGRPCService(rulesetName); err != nil {
		return fmt.Errorf("failed to unregister gRPC service: %w", err)
	}

	delete(res.rulesets, rulesetName)
	delete(res.services, rulesetName)

	log.Printf("Unregistered ruleset '%s'", rulesetName)
	return nil
}

// ExecuteRuleset executes rules for a specific ruleset
func (res *RulesetExecutionServer) ExecuteRuleset(ctx context.Context, req *ExecutionRequest) (*ExecutionResponse, error) {
	res.mu.RLock()
	service, exists := res.services[req.RulesetName]
	res.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("ruleset '%s' not found", req.RulesetName)
	}

	return service.Execute(ctx, req)
}

// GetRulesetInfo returns information about a registered ruleset
func (res *RulesetExecutionServer) GetRulesetInfo(ctx context.Context, rulesetName string) (*RulesetInfo, error) {
	res.mu.RLock()
	ruleset, exists := res.rulesets[rulesetName]
	res.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("ruleset '%s' not found", rulesetName)
	}

	return &RulesetInfo{
		Name:         ruleset.Name,
		Version:      ruleset.Version,
		Description:  ruleset.Description,
		RuleCount:    len(ruleset.Rules),
		VerbCount:    len(ruleset.Verbs),
		Dependencies: ruleset.Dependencies,
		Capabilities: ruleset.Capabilities,
		FactSchema:   ruleset.FactSchema,
		Metadata:     ruleset.Metadata,
	}, nil
}

// ListRulesets returns all registered rulesets
func (res *RulesetExecutionServer) ListRulesets(ctx context.Context) ([]*RulesetInfo, error) {
	res.mu.RLock()
	defer res.mu.RUnlock()

	rulesets := make([]*RulesetInfo, 0, len(res.rulesets))
	for _, ruleset := range res.rulesets {
		info := &RulesetInfo{
			Name:         ruleset.Name,
			Version:      ruleset.Version,
			Description:  ruleset.Description,
			RuleCount:    len(ruleset.Rules),
			VerbCount:    len(ruleset.Verbs),
			Dependencies: ruleset.Dependencies,
			Capabilities: ruleset.Capabilities,
			FactSchema:   ruleset.FactSchema,
			Metadata:     ruleset.Metadata,
		}
		rulesets = append(rulesets, info)
	}

	return rulesets, nil
}

// EnableHotReload enables automatic reloading of rulesets
func (res *RulesetExecutionServer) EnableHotReload(interval time.Duration) {
	res.mu.Lock()
	defer res.mu.Unlock()

	if res.hotReload {
		return // Already enabled
	}

	res.hotReload = true
	res.reloadChecker = time.NewTicker(interval)

	go res.hotReloadLoop()
	log.Printf("Hot reload enabled with interval: %v", interval)
}

// Execute implements the execution logic for a specific ruleset
func (rs *RulesetService) Execute(ctx context.Context, req *ExecutionRequest) (*ExecutionResponse, error) {
	// Unmarshal facts from Any type
	facts, err := rs.unmarshalFacts(req.Facts)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal facts: %w", err)
	}

	// Validate facts against schema
	if err := rs.validateFacts(facts); err != nil {
		return nil, fmt.Errorf("fact validation failed: %w", err)
	}

	// Execute rules using the runtime
	effects, err := rs.executeRules(ctx, facts, req.Options)
	if err != nil {
		return &ExecutionResponse{
			Success:     false,
			ExecutionID: generateExecutionID(),
			Duration:    timestamppb.New(time.Now()),
			Errors:      []string{err.Error()},
		}, nil
	}

	// Convert effects to typed effects
	typedEffects, err := rs.marshalEffects(effects)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal effects: %w", err)
	}

	return &ExecutionResponse{
		Success:     true,
		Effects:     typedEffects,
		ExecutionID: generateExecutionID(),
		Duration:    timestamppb.New(time.Now()),
		Metadata: map[string]string{
			"ruleset":    rs.ruleset.Name,
			"version":    rs.ruleset.Version,
			"rule_count": fmt.Sprintf("%d", len(rs.ruleset.Rules)),
		},
	}, nil
}

// RulesetInfo provides metadata about a ruleset
type RulesetInfo struct {
	Name         string
	Version      string
	Description  string
	RuleCount    int
	VerbCount    int
	Dependencies []string
	Capabilities []string
	FactSchema   *Schema
	Metadata     map[string]string
}

// Helper methods and private functions

func (res *RulesetExecutionServer) validateRuleset(ruleset *CompiledRuleset) error {
	if ruleset.Name == "" {
		return fmt.Errorf("ruleset name cannot be empty")
	}
	if ruleset.Version == "" {
		return fmt.Errorf("ruleset version cannot be empty")
	}
	if ruleset.FactSchema == nil {
		return fmt.Errorf("ruleset must have a fact schema")
	}
	return nil
}

func (res *RulesetExecutionServer) registerGRPCService(name string, service *RulesetService) error {
	// In a full implementation, this would dynamically generate and register
	// protobuf service definitions based on the ruleset's schema
	// For now, we'll use a generic service interface
	return nil
}

func (res *RulesetExecutionServer) unregisterGRPCService(name string) error {
	// Implementation would remove the dynamically registered service
	return nil
}

func (res *RulesetExecutionServer) hotReloadLoop() {
	for range res.reloadChecker.C {
		if err := res.checkForUpdates(); err != nil {
			log.Printf("Hot reload check failed: %v", err)
		}
	}
}

func (res *RulesetExecutionServer) checkForUpdates() error {
	// Implementation would check for ruleset updates and reload as needed
	return nil
}

func (rs *RulesetService) unmarshalFacts(anyFacts *anypb.Any) (map[string]interface{}, error) {
	// Implementation would unmarshal the Any type to facts based on the schema
	return make(map[string]interface{}), nil
}

func (rs *RulesetService) validateFacts(facts map[string]interface{}) error {
	// Implementation would validate facts against the ruleset's fact schema
	return nil
}

func (rs *RulesetService) executeRules(ctx context.Context, facts map[string]interface{}, options *ExecutionOptions) ([]interface{}, error) {
	// Implementation would execute rules using the runtime
	return []interface{}{}, nil
}

func (rs *RulesetService) marshalEffects(effects []interface{}) ([]*TypedEffect, error) {
	// Implementation would marshal effects to typed protobuf messages
	return []*TypedEffect{}, nil
}

func generateExecutionID() string {
	// Generate a unique execution ID
	return fmt.Sprintf("exec-%d", time.Now().UnixNano())
}
