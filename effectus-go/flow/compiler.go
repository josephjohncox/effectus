package flow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
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
			return nil, fmt.Errorf("failed to compile flow %s: %w", flow.Name, err)
		}
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

// compileFlow compiles a single flow into a CompiledFlow
func compileFlow(flow *ast.Flow, schema effectus.SchemaInfo) (*CompiledFlow, error) {
	compiledFlow := &CompiledFlow{
		Name:     flow.Name,
		Priority: flow.Priority,
	}

	// Compile predicates
	if flow.When != nil && flow.When.Predicates != nil {
		predicates := make([]*Predicate, 0, len(flow.When.Predicates))
		factPaths := make(map[string]struct{})

		for _, pred := range flow.When.Predicates {
			// Validate path against schema
			if !schema.ValidatePath(pred.Path) {
				return nil, fmt.Errorf("invalid path: %s", pred.Path)
			}

			// Save path for later fact requirements
			factPaths[pred.Path] = struct{}{}

			// Create compiled predicate
			compiledPred := &Predicate{
				Path: pred.Path,
				Op:   pred.Op,
				Lit:  compileLiteral(&pred.Lit),
			}
			predicates = append(predicates, compiledPred)
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

	// Compile arguments, resolving any variable references and fact paths
	args := make(map[string]interface{})
	for _, arg := range step.Args {
		var value interface{}

		if arg.Value != nil {
			if arg.Value.VarRef != "" {
				// It's a variable reference like $varName
				varName := strings.TrimPrefix(arg.Value.VarRef, "$")
				if boundValue, exists := bindings[varName]; exists {
					value = boundValue
				} else {
					return nil, fmt.Errorf("undefined variable reference: %s", arg.Value.VarRef)
				}
			} else if arg.Value.FactPath != "" {
				// It's a fact path like customer.email
				// This gets resolved at execution time, so we just pass it as a string
				value = arg.Value.FactPath
			} else if arg.Value.Literal != nil {
				// It's a literal value
				value = compileLiteral(arg.Value.Literal)
			}
		}

		args[arg.Name] = value
	}

	// Create the effect
	effect := effectus.Effect{
		Verb:    step.Verb,
		Payload: args,
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
				// Can't return error here, so we return an "error program"
				// In a real implementation, we'd need a better strategy
				return Pure(fmt.Errorf("failed to compile next steps: %w", err))
			}
			return nextProgram
		}), nil
	} else {
		// If not binding, just continue with remaining steps
		nextProgram, err := compileSteps(steps[1:], bindings, schema)
		if err != nil {
			return nil, err
		}
		return Do(effect, func(_ interface{}) *Program {
			return nextProgram
		}), nil
	}
}

// compileLiteral converts an AST literal to a runtime value
func compileLiteral(lit *ast.Literal) interface{} {
	if lit.String != nil {
		return *lit.String
	}
	if lit.Int != nil {
		return *lit.Int
	}
	if lit.Float != nil {
		return *lit.Float
	}
	if lit.Bool != nil {
		return *lit.Bool
	}
	if lit.List != nil {
		list := make([]interface{}, 0, len(lit.List))
		for _, item := range lit.List {
			list = append(list, compileLiteral(&item))
		}
		return list
	}
	if lit.Map != nil {
		m := make(map[string]interface{}, len(lit.Map))
		for _, entry := range lit.Map {
			m[entry.Key] = compileLiteral(&entry.Value)
		}
		return m
	}
	return nil
}
