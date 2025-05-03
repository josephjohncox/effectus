package flow

import (
	"fmt"
	"path/filepath"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/eval"
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

	// Parse the file
	file, err := effectus.ParseFile(path)
	if err != nil {
		return nil, err
	}

	// Compile the file
	return c.CompileParsedFile(file, path, schema)
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
func compileFlow(flow *ast.Flow, schema effectus.SchemaInfo) (*CompiledFlow, error) {
	compiledFlow := &CompiledFlow{
		Name:     flow.Name,
		Priority: flow.Priority,
	}

	// Compile predicates
	if flow.When != nil && flow.When.Expression != "" {
		predicates, factPaths, err := eval.CompileLogicalExpression(flow.When.Expression, schema)
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
		program, err := compileSteps(flow.Steps.Steps, bindings, schema)
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

// compileSteps compiles a sequence of steps into a Program
func compileSteps(steps []*ast.Step, bindings map[string]interface{}, schema effectus.SchemaInfo) (*Program, error) {
	if len(steps) == 0 {
		return Pure(nil), nil
	}
	step := steps[0]

	// Create the effect
	compiledArgs, err := common.CompileArgs(step.Args, bindings)
	if err != nil {
		return nil, fmt.Errorf("failed to compile args: %w", err)
	}
	effect := effectus.Effect{
		Verb:    step.Verb,
		Payload: compiledArgs,
	}

	// Create the program with the remaining steps
	if step.BindName != "" {
		// If we're binding a name, create a continuation that updates bindings
		return Do(effect, func(result interface{}) *Program {
			// Update bindings with result
			newBindings := make(map[string]interface{})
			for k, v := range bindings {
				newBindings[k] = v
			}
			newBindings[step.BindName] = result

			// Compile remaining steps with updated bindings
			nextProgram, err := compileSteps(steps[1:], newBindings, schema)
			if err != nil {
				// Return a program that immediately returns this error to ensure proper error propagation
				return Error(fmt.Errorf("failed to compile next steps: %w", err))
			}
			return nextProgram
		}), nil
	} else {
		// If not binding, just continue with remaining steps
		return Do(effect, func(_ interface{}) *Program {
			nextProgram, err := compileSteps(steps[1:], bindings, schema)
			if err != nil {
				// Return a program that immediately returns this error to ensure proper error propagation
				return Error(fmt.Errorf("failed to compile next steps: %w", err))
			}
			return nextProgram
		}), nil
	}
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
