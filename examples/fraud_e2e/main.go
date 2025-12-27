package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

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
	name   string
	result interface{}
	fail   bool
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

type httpExecutor struct {
	name      string
	url       string
	method    string
	resultKey string
	fail      bool
	client    *http.Client
}

func (e *httpExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("%s marshal args: %w", e.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, e.method, e.url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s build request: %w", e.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.fail {
		req.Header.Set("X-Fail", "true")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", e.name, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s failed: status %d: %s", e.name, resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return true, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err == nil {
		if e.resultKey != "" {
			if obj, ok := decoded.(map[string]interface{}); ok {
				if val, exists := obj[e.resultKey]; exists {
					return val, nil
				}
			}
		}
		return decoded, nil
	}

	return string(body), nil
}

func main() {
	ctx := context.Background()

	facts, typeSystem, err := loadFactsAndSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load facts/schema: %v\n", err)
		os.Exit(1)
	}

	verbConfig := verbConfig{
		riskURL:    os.Getenv("RISK_URL"),
		notifyURL:  os.Getenv("NOTIFY_URL"),
		failNotify: os.Getenv("FAIL_NOTIFY") != "",
	}
	registry := buildVerbRegistry(verbConfig)
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
	rawFacts := readFileBytes(factsFile)
	var data map[string]interface{}
	if err := json.Unmarshal(rawFacts, &data); err != nil {
		return nil, nil, err
	}

	provider := pathutil.NewRegistryFactProviderFromMap(data)

	ts := types.NewTypeSystem()
	if err := ts.LoadSchemaFile(schemaFile); err != nil {
		return nil, nil, err
	}

	schemaInfo := &typeSystemSchema{ts: ts}
	return &exampleFacts{data: data, provider: provider, schema: schemaInfo}, ts, nil
}

type verbConfig struct {
	riskURL    string
	notifyURL  string
	failNotify bool
}

func buildVerbRegistry(cfg verbConfig) *verb.Registry {
	registry := verb.NewRegistry(nil)

	openCaseExecutor := verb.Executor(&loggingExecutor{name: "OpenCase", result: "case-001"})
	if cfg.riskURL != "" {
		openCaseExecutor = &httpExecutor{
			name:      "OpenCase",
			url:       cfg.riskURL,
			method:    http.MethodPost,
			resultKey: "caseId",
			client:    &http.Client{Timeout: 5 * time.Second},
		}
	}

	notifyExecutor := verb.Executor(&loggingExecutor{name: "NotifyRisk", fail: cfg.failNotify})
	if cfg.notifyURL != "" {
		notifyExecutor = &httpExecutor{
			name:   "NotifyRisk",
			url:    cfg.notifyURL,
			method: http.MethodPost,
			fail:   cfg.failNotify,
			client: &http.Client{Timeout: 5 * time.Second},
		}
	}

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
		Executor:     openCaseExecutor,
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
		Executor:     notifyExecutor,
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
