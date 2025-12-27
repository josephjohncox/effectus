package list

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
)

// Effect represents a verb to be executed along with its arguments
// This will be phased out in favor of effectus.Effect
type Effect struct {
	Verb string                 // Name of the verb
	Args map[string]interface{} // Arguments to the verb
}

// Spec implements the effectus.Spec interface for list rules
type Spec struct {
	Rules        []*CompiledRule
	FactPaths    []string
	Name         string
	SagaEnabled  bool                         // Whether to use saga execution
	SagaStore    schema.SagaStore             // Saga store for transaction management
	CapSystem    *capability.CapabilitySystem // Capability system for locking
	VerbRegistry common.VerbRegistry          // Verb registry for execution
}

// GetName returns the name of this spec
func (s *Spec) GetName() string {
	return s.Name
}

// RequiredFacts returns the list of fact paths required by this spec
func (s *Spec) RequiredFacts() []string {
	return s.FactPaths
}

// Execute runs all rules in the spec with saga and capability support
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Sort rules by priority using common utility (make a copy first)
	rules := make([]*CompiledRule, len(s.Rules))
	copy(rules, s.Rules)
	common.SortByPriority(rules)

	// Create executor with saga and capability support if available
	var executor *Executor
	if s.VerbRegistry != nil {
		var options []ExecutorOption

		if s.SagaEnabled && s.SagaStore != nil {
			options = append(options, WithSaga(s.SagaStore))
		}

		if s.CapSystem != nil {
			options = append(options, WithCapabilitySystem(s.CapSystem))
		}

		executor = NewExecutor(s.VerbRegistry, options...)
	}

	// Run each rule
	for _, rule := range rules {
		// Check if the context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Evaluate rule predicates using facts directly
		if !schema.EvaluatePredicatesWithFacts(rule.Predicates, facts) {
			continue
		}

		fmt.Printf("Rule %s matches, executing effects\n", rule.Name)

		// Execute effects based on available execution modes
		if executor != nil {
			// Use the enhanced executor with saga/capability support
			if err := s.executeRuleWithEnhancedExecutor(ctx, rule, facts, executor); err != nil {
				return fmt.Errorf("error executing rule %s: %w", rule.Name, err)
			}
		} else {
			// Fallback to simple execution
			if err := s.executeRuleSimple(ctx, rule, facts, ex); err != nil {
				return fmt.Errorf("error executing rule %s: %w", rule.Name, err)
			}
		}
	}

	return nil
}

// executeRuleWithEnhancedExecutor executes a rule using the enhanced executor with saga/capability support
func (s *Spec) executeRuleWithEnhancedExecutor(ctx context.Context, rule *CompiledRule, facts effectus.Facts, executor *Executor) error {
	// Convert facts to common.Facts for the executor
	// This is a simplified adapter - in practice you'd have a more sophisticated conversion
	commonFacts := &factsAdapter{facts: facts}

	// Execute the rule
	_, err := executor.ExecuteRule(ctx, rule, commonFacts)
	return err
}

// executeRuleSimple executes a rule using simple execution without saga/capability support
func (s *Spec) executeRuleSimple(ctx context.Context, rule *CompiledRule, facts effectus.Facts, ex effectus.Executor) error {
	// Execute effects using standard executor
	for _, effect := range rule.Effects {
		execEffect := effectus.Effect{
			Verb:    effect.Verb,
			Payload: effect.Args,
		}

		fmt.Printf("Executing effect: %s\n", effect.Verb)
		result, err := ex.Do(execEffect)
		if err != nil {
			return fmt.Errorf("error executing effect %s: %w", effect.Verb, err)
		}

		fmt.Printf("Effect %s result: %v\n", effect.Verb, result)
	}

	return nil
}

// CompiledRule represents a rule after compilation
type CompiledRule struct {
	Name       string
	Priority   int
	Predicates []*schema.Predicate
	Effects    []*Effect
	FactPaths  []string
}

// GetPriority implements the common.Prioritized interface
func (cr *CompiledRule) GetPriority() int {
	return cr.Priority
}

// factsAdapter adapts effectus.Facts to common.Facts for the executor
type factsAdapter struct {
	facts effectus.Facts
}

// Get implements common.Facts
func (fa *factsAdapter) Get(path string) (interface{}, bool) {
	return fa.facts.Get(path)
}

// GetWithContext implements common.Facts (required by the interface)
func (fa *factsAdapter) GetWithContext(path string) (interface{}, *common.ResolutionResult) {
	value, exists := fa.facts.Get(path)
	return value, &common.ResolutionResult{
		Exists: exists,
		Path:   path,
		Value:  value,
	}
}

// HasPath implements common.Facts (required by the interface)
func (fa *factsAdapter) HasPath(path string) bool {
	_, exists := fa.facts.Get(path)
	return exists
}

// Schema implements common.Facts
func (fa *factsAdapter) Schema() common.SchemaInfo {
	return &schemaAdapter{schema: fa.facts.Schema()}
}

// schemaAdapter adapts effectus.SchemaInfo to common.SchemaInfo
type schemaAdapter struct {
	schema effectus.SchemaInfo
}

// ValidatePath implements common.SchemaInfo
func (sa *schemaAdapter) ValidatePath(path string) bool {
	return sa.schema.ValidatePath(path)
}

// GetPathType implements common.SchemaInfo (required by the interface)
func (sa *schemaAdapter) GetPathType(path string) *types.Type {
	// Simple implementation - in practice this would be more sophisticated
	if sa.schema.ValidatePath(path) {
		// Return a generic type since we don't have type information from effectus.SchemaInfo
		return &types.Type{Name: "unknown"}
	}
	return nil
}

// RegisterPathType implements common.SchemaInfo (required by the interface)
func (sa *schemaAdapter) RegisterPathType(path string, typ *types.Type) {
	// No-op for now since effectus.SchemaInfo doesn't support registration
}
