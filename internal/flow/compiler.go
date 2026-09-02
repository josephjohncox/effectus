package flow

import (
	"fmt"
	"path/filepath"

	"github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/ast"
	"github.com/josephjohncox/effectus/common"
	"github.com/josephjohncox/effectus/schema"
)

// Compiler implements the Compiler interface for flow-style rules
type Compiler struct{}

// CompileFile compiles a rule file into a flow-style spec
func (c *Compiler) CompileFile(path string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	// Ensure the file has the correct extension
	ext := filepath.Ext(path)
	if ext != ".effx" {
		return nil, fmt.Errorf("flow compiler can only compile .effx files, got: %s", path)
	}

	// For now, return an error indicating that file parsing needs to be implemented
	// This breaks the dependency cycle while maintaining the interface
	return nil, fmt.Errorf("flow compiler file parsing not yet implemented for %s", path)
}

// CompileParsedFile compiles a parsed file into a flow-style spec
func (c *Compiler) CompileParsedFile(file *ast.File, path string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	// Ensure the file contains flows
	if len(file.Flows) == 0 {
		return nil, fmt.Errorf("no flows found in file %s", path)
	}

	// Compile each flow
	spec := &Spec{
		Flows: make([]*CompiledFlow, 0, len(file.Flows)),
	}

	factPaths := make(map[string]struct{})

	for _, flow := range file.Flows {
		compiledFlow, err := compileFlow(flow, schema)
		if err != nil {
			return nil, fmt.Errorf("failed to compile flow %s in %s: %w", flow.Name, path, err)
		}

		// Store source file information in compiled flow
		compiledFlow.SourceFile = path

		spec.Flows = append(spec.Flows, compiledFlow)

		// Collect fact paths
		for _, path := range compiledFlow.FactPaths {
			factPaths[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	spec.FactPaths = make([]string, 0, len(factPaths))
	for path := range factPaths {
		spec.FactPaths = append(spec.FactPaths, path)
	}

	return spec, nil
}

// CompileFiles compiles multiple rule files into a single flow-style spec
func (c *Compiler) CompileFiles(paths []string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	// Create merged spec
	mergedSpec := &Spec{
		Flows:     make([]*CompiledFlow, 0),
		FactPaths: make([]string, 0),
		Name:      "merged",
	}

	factPathSet := make(map[string]struct{})

	// Compile each file and merge results
	for _, path := range paths {
		spec, err := c.CompileFile(path, schema)
		if err != nil {
			return nil, fmt.Errorf("error compiling %s: %w", path, err)
		}

		// Merge the compiled spec
		flowSpec, ok := spec.(*Spec)
		if !ok {
			return nil, fmt.Errorf("unexpected spec type for %s", path)
		}

		// Add flows to merged spec
		mergedSpec.Flows = append(mergedSpec.Flows, flowSpec.Flows...)

		// Collect fact paths
		for _, factPath := range flowSpec.FactPaths {
			factPathSet[factPath] = struct{}{}
		}
	}

	// Extract unique fact paths
	for path := range factPathSet {
		mergedSpec.FactPaths = append(mergedSpec.FactPaths, path)
	}

	return mergedSpec, nil
}

// compileFlow compiles a single flow into a CompiledFlow
func compileFlow(flow *ast.Flow, schemaInfo effectus.SchemaInfo) (*CompiledFlow, error) {
	compiledFlow := &CompiledFlow{
		Name:     flow.Name,
		Priority: flow.Priority,
	}

	// Compile predicates using schema registry
	if flow.When != nil && flow.When.Expression != "" {
		// Create a registry for compilation
		registry := schema.NewRegistry()
		predicates, factPaths, err := registry.CompileLogicalExpression(flow.When.Expression, schemaInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to compile predicates: %w", err)
		}

		compiledFlow.Predicates = predicates

		// Extract unique fact paths
		compiledFlow.FactPaths = make([]string, 0, len(factPaths))
		for path := range factPaths {
			compiledFlow.FactPaths = append(compiledFlow.FactPaths, path)
		}
	}

	// Compile steps
	if flow.Steps != nil && flow.Steps.Steps != nil {
		// Create bindings map for variable resolution
		bindings := make(map[string]interface{})

		// Compile the steps into a Program
		program, err := compileSteps(flow.Steps.Steps, bindings, schemaInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to compile steps: %w", err)
		}

		compiledFlow.Program = program
	} else {
		// Empty program
		compiledFlow.Program = Pure(nil)
	}

	return compiledFlow, nil
}

type compiledStep struct {
	verb string
	args map[string]interface{}
	bind string
}

// compileSteps checks and lowers every step before it creates an executable program.
func compileSteps(steps []*ast.Step, bindings map[string]interface{}, _ effectus.SchemaInfo) (*Program, error) {
	checkedBindings := copyBindings(bindings)
	compiled := make([]compiledStep, 0, len(steps))
	for index, step := range steps {
		if step == nil {
			return nil, fmt.Errorf("step %d is nil", index+1)
		}
		args, err := common.CompileArgs(step.Args, checkedBindings)
		if err != nil {
			return nil, fmt.Errorf("compile step %d (%s): %w", index+1, step.Verb, err)
		}
		compiled = append(compiled, compiledStep{verb: step.Verb, args: args, bind: step.BindName})
		if step.BindName != "" {
			if _, exists := checkedBindings[step.BindName]; exists {
				return nil, fmt.Errorf("step %d redefines result binding %q", index+1, step.BindName)
			}
			checkedBindings[step.BindName] = common.ResultRef(step.BindName)
		}
	}
	return buildCompiledSteps(compiled, 0, copyBindings(bindings)), nil
}

func buildCompiledSteps(steps []compiledStep, index int, bindings map[string]interface{}) *Program {
	if index == len(steps) {
		return Pure(nil)
	}
	step := steps[index]
	args := make(map[string]interface{}, len(step.args))
	for name, value := range step.args {
		if ref, ok := value.(common.ResultRef); ok {
			resolved, exists := bindings[string(ref)]
			if !exists {
				return Error(fmt.Errorf("result binding %q is unavailable", ref))
			}
			args[name] = resolved
			continue
		}
		args[name] = value
	}
	effect := effectus.Effect{Verb: step.verb, Payload: args}
	return Do(effect, func(result interface{}) *Program {
		nextBindings := copyBindings(bindings)
		if step.bind != "" {
			nextBindings[step.bind] = result
		}
		return buildCompiledSteps(steps, index+1, nextBindings)
	})
}

func copyBindings(bindings map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(bindings))
	for name, value := range bindings {
		copy[name] = value
	}
	return copy
}

// Error creates a program that immediately returns an error
func Error(err error) *Program {
	// Create a program that contains the error as its Pure value
	// This will be detected and treated as an error during execution
	return &Program{
		Tag:  PureProgramTag, // This is declared in program.go
		Pure: err,            // Store the error in the Pure field
	}
}
