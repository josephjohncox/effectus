package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
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

type loggingExecutor struct {
	name string
}

func (e *loggingExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fmt.Printf("▶ %s args=%v\n", e.name, args)
	return true, nil
}

func main() {
	manifestPath := flag.String("manifest", "examples/multi_bundle_runtime/manifest.json", "Path to extension manifest")
	factsPath := flag.String("facts", "examples/multi_bundle_runtime/facts.json", "Path to fact payload JSON")
	watch := flag.Bool("watch", false, "Watch manifest for changes and reload")
	interval := flag.Duration("interval", 3*time.Second, "Watch interval")
	flag.Parse()

	ctx := context.Background()
	if err := runOnce(ctx, *manifestPath, *factsPath); err != nil {
		fmt.Fprintf(os.Stderr, "Run failed: %v\n", err)
		os.Exit(1)
	}

	if !*watch {
		return
	}

	fmt.Println("\nWatching for manifest changes...")
	prevHash := hashFile(*manifestPath)
	for {
		time.Sleep(*interval)
		nextHash := hashFile(*manifestPath)
		if nextHash == "" || nextHash == prevHash {
			continue
		}
		prevHash = nextHash
		fmt.Println("\n== Reloading bundles ==")
		if err := runOnce(ctx, *manifestPath, *factsPath); err != nil {
			fmt.Fprintf(os.Stderr, "Reload failed: %v\n", err)
		}
	}
}

func runOnce(ctx context.Context, manifestPath, factsPath string) error {
	facts, typeSystem, err := loadFactsAndSchema(factsPath)
	if err != nil {
		return err
	}

	resolved, err := unified.ResolveManifestFile(manifestPath, unified.ResolverOptions{AllowFile: true})
	if err != nil {
		return err
	}
	if len(resolved) == 0 {
		return fmt.Errorf("no bundles resolved")
	}

	schemaFiles := 0
	verbFiles := 0
	ruleFiles := 0

	verbRegistry := verb.NewRegistry(typeSystem)
	loadedRules := make([]string, 0)

	for _, bundle := range resolved {
		bundleMeta := bundle.Bundle
		if bundleMeta == nil {
			bundleMeta, err = unified.LoadBundle(bundle.BundlePath)
			if err != nil {
				return err
			}
		}

		for _, schemaFile := range bundleMeta.SchemaFiles {
			path := filepath.Join(bundle.RootDir, "schema", schemaFile)
			if err := typeSystem.LoadSchemaFile(path); err != nil {
				return fmt.Errorf("loading schema %s: %w", path, err)
			}
			schemaFiles++
		}

		for _, verbFile := range bundleMeta.VerbFiles {
			path := filepath.Join(bundle.RootDir, "verbs", verbFile)
			if err := verbRegistry.LoadFromJSON(path); err != nil {
				return fmt.Errorf("loading verbs %s: %w", path, err)
			}
			verbFiles++
		}

		for _, ruleFile := range bundleMeta.RuleFiles {
			path := filepath.Join(bundle.RootDir, "rules", ruleFile)
			loadedRules = append(loadedRules, path)
			ruleFiles++
		}
	}

	attachExecutors(verbRegistry)

	comp := compiler.NewCompiler()
	comp.GetTypeSystem().MergeTypeSystem(typeSystem)

	compiledSpec, err := compileListRules(comp, loadedRules, facts)
	if err != nil {
		return err
	}
	compiledSpec.VerbRegistry = verbRegistry

	fmt.Printf("Loaded %d bundles (schemas: %d, verbs: %d, rules: %d)\n", len(resolved), schemaFiles, verbFiles, ruleFiles)
	fmt.Println("== Executing merged list rules ==")
	return compiledSpec.Execute(ctx, facts, nil)
}

func loadFactsAndSchema(factsPath string) (*exampleFacts, *types.TypeSystem, error) {
	dataBytes, err := os.ReadFile(factsPath)
	if err != nil {
		return nil, nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, nil, err
	}

	provider := pathutil.NewRegistryFactProviderFromMap(data)
	ts := types.NewTypeSystem()
	return &exampleFacts{data: data, provider: provider, schema: &typeSystemSchema{ts: ts}}, ts, nil
}

func compileListRules(comp *compiler.Compiler, ruleFiles []string, facts *exampleFacts) (*list.Spec, error) {
	listCompiler := &list.Compiler{}
	specs := make([]*list.Spec, 0)

	for _, file := range ruleFiles {
		parsed, err := comp.ParseAndTypeCheck(file, facts)
		if err != nil {
			return nil, err
		}
		specAny, err := listCompiler.CompileParsedFile(parsed, file, facts.Schema())
		if err != nil {
			return nil, err
		}
		spec, ok := specAny.(*list.Spec)
		if !ok {
			return nil, fmt.Errorf("unexpected spec type for %s", file)
		}
		specs = append(specs, spec)
	}

	merged := &list.Spec{Rules: []*list.CompiledRule{}}
	factPaths := make(map[string]struct{})
	for _, spec := range specs {
		merged.Rules = append(merged.Rules, spec.Rules...)
		for _, path := range spec.FactPaths {
			factPaths[path] = struct{}{}
		}
	}

	merged.FactPaths = make([]string, 0, len(factPaths))
	for path := range factPaths {
		merged.FactPaths = append(merged.FactPaths, path)
	}
	sort.Strings(merged.FactPaths)

	return merged, nil
}

func attachExecutors(registry *verb.Registry) {
	executors := map[string]verb.Executor{
		"NotifyCustomer":  &loggingExecutor{name: "NotifyCustomer"},
		"FreezeAccount":   &loggingExecutor{name: "FreezeAccount"},
		"UnfreezeAccount": &loggingExecutor{name: "UnfreezeAccount"},
	}

	for name, exec := range executors {
		_ = registry.SetExecutor(name, exec)
	}
}

func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
