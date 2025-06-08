package list

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/eval"
	"github.com/effectus/effectus-go/schema/verb"
)

// Effect represents a verb to be executed along with its arguments
// This will be phased out in favor of effectus.Effect
type Effect struct {
	Verb string                 // Name of the verb
	Args map[string]interface{} // Arguments to the verb
}

// Spec implements the effectus.Spec interface for list rules
type Spec struct {
	Rules      []*CompiledRule
	FactPaths  []string
	Name       string
	VerbSystem *verb.UnifiedVerbSystem // Use unified verb system
}

// GetName returns the name of this spec
func (s *Spec) GetName() string {
	return s.Name
}

// RequiredFacts returns the list of fact paths required by this spec
func (s *Spec) RequiredFacts() []string {
	return s.FactPaths
}

// Execute runs all rules in the spec
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Sort rules by priority using common utility
	rules := common.SortByPriorityFunc(s.Rules, func(rule *CompiledRule) int {
		return rule.Priority
	})

	// Run each rule
	for _, rule := range rules {
		// Check if the context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Evaluate rule predicates
		if !eval.EvaluatePredicates(rule.Predicates, facts) {
			continue
		}

		fmt.Printf("Rule %s matches, executing effects\n", rule.Name)

		// Execute effects
		for _, effect := range rule.Effects {
			// Use unified verb system if available
			if s.VerbSystem != nil {
				// Create properly validated effect
				execEffect, err := s.VerbSystem.CreateEffect(effect.Verb, effect.Args)
				if err != nil {
					return fmt.Errorf("error creating effect %s: %w", effect.Verb, err)
				}

				// Execute using unified system
				result, err := s.VerbSystem.Execute(ctx, execEffect)
				if err != nil {
					return fmt.Errorf("error executing effect %s: %w", effect.Verb, err)
				}

				fmt.Printf("Effect %s result: %v\n", effect.Verb, result)
			} else {
				// Fallback to legacy executor for backward compatibility
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
		}
	}

	return nil
}

// CompiledRule represents a rule after compilation
type CompiledRule struct {
	Name       string
	Priority   int
	Predicates []*eval.Predicate
	Effects    []*Effect
	FactPaths  []string
}

// GetPriority implements the common.Prioritized interface
func (cr *CompiledRule) GetPriority() int {
	return cr.Priority
}

// sortRulesByPriority is now replaced by common.SortByPriorityFunc
// This eliminates code duplication across different spec types
