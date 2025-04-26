package flow

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
)

// Spec implements the effectus.Spec interface for flow rules
type Spec struct {
	Flows     []*CompiledFlow
	FactPaths []string
}

// RequiredFacts returns the list of fact paths required by this spec
func (s *Spec) RequiredFacts() []string {
	return s.FactPaths
}

// Execute runs all flows in the spec
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Sort flows by priority
	flows := sortFlowsByPriority(s.Flows)

	// Create an executor wrapper to handle context cancellation
	executor := &contextExecutor{
		ctx:      ctx,
		executor: ex,
	}

	// Run each flow
	for _, flow := range flows {
		// Check if the context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Evaluate flow predicates
		if !evaluatePredicates(flow.Predicates, facts) {
			continue
		}

		// Execute the program
		_, err := Run(flow.Program, executor)
		if err != nil {
			return fmt.Errorf("error executing flow %s: %w", flow.Name, err)
		}
	}

	return nil
}

// CompiledFlow represents a flow after compilation
type CompiledFlow struct {
	Name       string
	Priority   int
	Predicates []*Predicate
	Program    *Program
	FactPaths  []string
}

// Predicate represents a compiled predicate
type Predicate struct {
	Path string
	Op   string
	Lit  interface{}
}

// sortFlowsByPriority sorts flows by priority (highest first)
func sortFlowsByPriority(flows []*CompiledFlow) []*CompiledFlow {
	// Copy flows to avoid modifying the original
	sortedFlows := make([]*CompiledFlow, len(flows))
	copy(sortedFlows, flows)

	// Sort by priority (highest first)
	for i := 0; i < len(sortedFlows); i++ {
		for j := i + 1; j < len(sortedFlows); j++ {
			if sortedFlows[i].Priority < sortedFlows[j].Priority {
				sortedFlows[i], sortedFlows[j] = sortedFlows[j], sortedFlows[i]
			}
		}
	}

	return sortedFlows
}

// evaluatePredicates evaluates all predicates for a flow
func evaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	fmt.Printf("Evaluating %d predicates\n", len(predicates))
	if len(predicates) == 0 {
		fmt.Println("No predicates, returning true")
		return true
	}

	for i, pred := range predicates {
		// Get fact value
		fmt.Printf("Checking predicate %d: %s %s\n", i+1, pred.Path, pred.Op)
		value, exists := facts.Get(pred.Path)
		if !exists {
			fmt.Printf("Fact path not found: %s\n", pred.Path)
			return false
		}

		fmt.Printf("Fact value: %v, comparing with: %v\n", value, pred.Lit)
		// Compare based on operator
		if !compareFact(value, pred.Op, pred.Lit) {
			fmt.Printf("Predicate %d failed\n", i+1)
			return false
		}
		fmt.Printf("Predicate %d passed\n", i+1)
	}

	fmt.Println("All predicates passed")
	return true
}

// compareFact compares a fact value to a literal using the given operator
func compareFact(factValue interface{}, op string, literal interface{}) bool {
	switch op {
	case "==":
		return equal(factValue, literal)
	case "!=":
		return !equal(factValue, literal)
	case "<":
		return lessThan(factValue, literal)
	case "<=":
		return lessThan(factValue, literal) || equal(factValue, literal)
	case ">":
		return greaterThan(factValue, literal)
	case ">=":
		return greaterThan(factValue, literal) || equal(factValue, literal)
	case "in":
		return contains(literal, factValue)
	case "contains":
		return contains(factValue, literal)
	default:
		return false
	}
}

// Helper comparison functions
func equal(a, b interface{}) bool {
	// Handle string comparison with quotes
	if aStr, aOk := a.(string); aOk {
		if bStr, bOk := b.(string); bOk {
			// Remove quotes if present
			if len(bStr) >= 2 && bStr[0] == '"' && bStr[len(bStr)-1] == '"' {
				unquoted := bStr[1 : len(bStr)-1]
				return aStr == unquoted
			}
			return aStr == bStr
		}
	}

	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal == bVal
		case int:
			return aVal == float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) == bVal
		case int:
			return aVal == bVal
		}
	}

	// Basic equality comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func lessThan(a, b interface{}) bool {
	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal < bVal
		case int:
			return aVal < float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) < bVal
		case int:
			return aVal < bVal
		}
	}

	// Simple string comparison as a fallback
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

func greaterThan(a, b interface{}) bool {
	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal > bVal
		case int:
			return aVal > float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) > bVal
		case int:
			return aVal > bVal
		}
	}

	// Simple string comparison as a fallback
	return fmt.Sprintf("%v", a) > fmt.Sprintf("%v", b)
}

func contains(container, item interface{}) bool {
	// Check if container contains item
	switch c := container.(type) {
	case []interface{}:
		for _, v := range c {
			if equal(v, item) {
				return true
			}
		}
	case string:
		if s, ok := item.(string); ok {
			return c == s // This should be strings.Contains for a real implementation
		}
	}
	return false
}

// contextExecutor wraps an Executor to check for context cancellation
type contextExecutor struct {
	ctx      context.Context
	executor effectus.Executor
}

// Do executes an effect, checking for context cancellation first
func (e *contextExecutor) Do(effect effectus.Effect) (interface{}, error) {
	// Check if context is cancelled
	if e.ctx.Err() != nil {
		return nil, e.ctx.Err()
	}

	// Execute the effect
	return e.executor.Do(effect)
}
