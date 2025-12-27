package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
)

const (
	schemaFile = "examples/fraud_e2e/schema/fraud_facts.json"
	factsFile  = "examples/fraud_e2e/data/facts.json"
	rulesFile  = "examples/fraud_e2e/rules/fraud_rules.eff"
	flowFile   = "examples/fraud_e2e/flows/fraud_flow.effx"
)

// Facts implementation for this example.
type exampleFacts struct {
	data     map[string]interface{}
	provider *pathutil.RegistryFactProvider
	schema   effectus.SchemaInfo
}

func (f *exampleFacts) Get(path string) (interface{}, bool) {
	if path == "" {
		return f.data, true
	}
	return f.provider.Get(path)
}

func (f *exampleFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// Schema adapter backed by a TypeSystem.
type typeSystemSchema struct {
	ts *types.TypeSystem
}

func (s *typeSystemSchema) ValidatePath(path string) bool {
	if path == "" {
		return false
	}
	_, err := s.ts.GetFactType(path)
	return err == nil
}

// Simple verb executor.
type loggingExecutor struct {
	name    string
	result  interface{}
	fail    bool
	traceID string
}

func (e *loggingExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fmt.Printf("▶ %s args=%v\n", e.name, args)
	if e.fail {
		return nil, fmt.Errorf("%s failed", e.name)
	}
	if e.result != nil {
		return e.result, nil
	}
	return true, nil
}

func main() {
	ctx := context.Background()

	facts, typeSystem, err := loadFactsAndSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load facts/schema: %v\n", err)
		os.Exit(1)
	}

	registry := buildVerbRegistry(os.Getenv("FAIL_NOTIFY") != "")
	registerVerbTypes(typeSystem)

	// Compile + execute list rules
	if err := runListRules(ctx, facts, typeSystem, registry); err != nil {
		fmt.Fprintf(os.Stderr, "List rules failed: %v\n", err)
		os.Exit(1)
	}

	// Compile + execute fraud flow with saga enabled
	if err := runFraudFlow(ctx, facts, typeSystem, registry); err != nil {
		fmt.Fprintf(os.Stderr, "Flow failed: %v\n", err)
		os.Exit(1)
	}
}

func loadFactsAndSchema() (*exampleFacts, *types.TypeSystem, error) {
	loader := pathutil.NewJSONLoader(readFileBytes(factsFile))
	provider, err := loader.Load()
	if err != nil {
		return nil, nil, err
	}

	// Get root data map for EvaluatePredicatesWithFacts.
	dataAny, _ := provider.Get("")
	data, _ := dataAny.(map[string]interface{})

	ts := types.NewTypeSystem()
	if err := ts.LoadSchemaFile(schemaFile); err != nil {
		return nil, nil, err
	}

	schemaInfo := &typeSystemSchema{ts: ts}
	return &exampleFacts{data: data, provider: provider, schema: schemaInfo}, ts, nil
}

func buildVerbRegistry(failNotify bool) *verb.Registry {
	registry := verb.NewRegistry(nil)

	mustRegister(registry, &verb.Spec{
		Name:         "FlagFraud",
		Description:  "Flags a transaction for review",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "fraud_case", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
		RequiredArgs: []string{"orderId", "reason"},
		ReturnType:   "bool",
		Inverse:      "UnflagFraud",
		Executor:     &loggingExecutor{name: "FlagFraud"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "UnflagFraud",
		Description:  "Removes a fraud flag",
		Capability:   verb.CapDelete,
		Resources:    verb.ResourceSet{{Resource: "fraud_case", Cap: verb.CapDelete}},
		ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
		RequiredArgs: []string{"orderId"},
		ReturnType:   "bool",
		Executor:     &loggingExecutor{name: "UnflagFraud"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "FreezeAccount",
		Description:  "Freezes a customer account",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "account", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"accountId": "string", "reason": "string"},
		RequiredArgs: []string{"accountId", "reason"},
		ReturnType:   "bool",
		Inverse:      "UnfreezeAccount",
		Executor:     &loggingExecutor{name: "FreezeAccount"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "UnfreezeAccount",
		Description:  "Unfreezes a customer account",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "account", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"accountId": "string", "reason": "string"},
		RequiredArgs: []string{"accountId"},
		ReturnType:   "bool",
		Executor:     &loggingExecutor{name: "UnfreezeAccount"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "OpenCase",
		Description:  "Creates a fraud case",
		Capability:   verb.CapCreate,
		Resources:    verb.ResourceSet{{Resource: "fraud_case", Cap: verb.CapCreate}},
		ArgTypes:     map[string]string{"orderId": "string", "risk": "int"},
		RequiredArgs: []string{"orderId", "risk"},
		ReturnType:   "string",
		Executor:     &loggingExecutor{name: "OpenCase", result: "case-001"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "UpdateCase",
		Description:  "Updates a fraud case",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "fraud_case", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"caseId": "string", "status": "string"},
		RequiredArgs: []string{"caseId", "status"},
		ReturnType:   "bool",
		Executor:     &loggingExecutor{name: "UpdateCase"},
	})

	mustRegister(registry, &verb.Spec{
		Name:         "NotifyRisk",
		Description:  "Notifies the risk team",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "notification", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"orderId": "string", "channel": "string"},
		RequiredArgs: []string{"orderId", "channel"},
		ReturnType:   "bool",
		Executor:     &loggingExecutor{name: "NotifyRisk", fail: failNotify},
	})

	return registry
}

func registerVerbTypes(ts *types.TypeSystem) {
	register := func(name string, args map[string]*types.Type, ret *types.Type) {
		if err := ts.RegisterVerbType(name, args, ret); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	}

	register("FlagFraud", map[string]*types.Type{
		"orderId": types.NewStringType(),
		"reason":  types.NewStringType(),
	}, types.NewBoolType())

	register("UnflagFraud", map[string]*types.Type{
		"orderId": types.NewStringType(),
		"reason":  types.NewStringType(),
	}, types.NewBoolType())

	register("FreezeAccount", map[string]*types.Type{
		"accountId": types.NewStringType(),
		"reason":    types.NewStringType(),
	}, types.NewBoolType())

	register("UnfreezeAccount", map[string]*types.Type{
		"accountId": types.NewStringType(),
		"reason":    types.NewStringType(),
	}, types.NewBoolType())

	register("OpenCase", map[string]*types.Type{
		"orderId": types.NewStringType(),
		"risk":    types.NewIntType(),
	}, types.NewStringType())

	register("UpdateCase", map[string]*types.Type{
		"caseId": types.NewStringType(),
		"status": types.NewStringType(),
	}, types.NewBoolType())

	register("NotifyRisk", map[string]*types.Type{
		"orderId": types.NewStringType(),
		"channel": types.NewStringType(),
	}, types.NewBoolType())
}

func runListRules(ctx context.Context, facts *exampleFacts, ts *types.TypeSystem, registry *verb.Registry) error {
	comp := compiler.NewCompiler()
	copyTypes(ts, comp.GetTypeSystem())

	file, err := comp.ParseAndTypeCheck(rulesFile, facts)
	if err != nil {
		return err
	}

	listCompiler := &list.Compiler{}
	specAny, err := listCompiler.CompileParsedFile(file, rulesFile, facts.Schema())
	if err != nil {
		return err
	}

	spec := specAny.(*list.Spec)
	spec.VerbRegistry = registry

	fmt.Println("\n== Running list rules ==")
	return spec.Execute(ctx, facts, nil)
}

func runFraudFlow(ctx context.Context, facts *exampleFacts, ts *types.TypeSystem, registry *verb.Registry) error {
	comp := compiler.NewCompiler()
	copyTypes(ts, comp.GetTypeSystem())

	file, err := comp.ParseAndTypeCheck(flowFile, facts)
	if err != nil {
		return err
	}

	flowCompiler := &flow.Compiler{}
	specAny, err := flowCompiler.CompileParsedFile(file, flowFile, facts.Schema())
	if err != nil {
		return err
	}

	spec := specAny.(*flow.Spec)
	spec.VerbRegistry = registry
	spec.SagaEnabled = true
	spec.SagaStore = schema.NewInMemorySagaStore()
	spec.CapSystem = capability.NewCapabilitySystem()

	fmt.Println("\n== Running fraud flow (saga enabled) ==")
	return spec.Execute(ctx, facts, nil)
}

func copyTypes(from *types.TypeSystem, to *types.TypeSystem) {
	for _, path := range from.GetAllFactPaths() {
		factType, _ := from.GetFactType(path)
		to.RegisterFactType(path, factType)
	}

	registerVerbTypes(to)
}

func mustRegister(registry *verb.Registry, spec *verb.Spec) {
	if err := registry.RegisterVerb(spec); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register %s: %v\n", spec.Name, err)
		os.Exit(1)
	}
}

func readFileBytes(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read %s: %v\n", path, err)
		os.Exit(1)
	}
	return data
}

func trimPath(path string) string {
	return strings.TrimPrefix(path, filepath.Clean("./"))
}
