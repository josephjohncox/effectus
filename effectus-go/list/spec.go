package list

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/eval"
)

// Effect represents a verb to be executed along with its arguments
type Effect struct {
	Verb string                 // Name of the verb
	Args map[string]interface{} // Arguments to the verb
}

// Spec implements the effectus.Spec interface for list rules
type Spec struct {
	Rules     []*CompiledRule
	FactPaths []string
	Name      string
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
	// Sort rules by priority
	rules := sortRulesByPriority(s.Rules)

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
			// Convert to effectus.Effect
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

// sortRulesByPriority sorts rules by priority (highest first)
func sortRulesByPriority(rules []*CompiledRule) []*CompiledRule {
	// Copy rules to avoid modifying the original
	sortedRules := make([]*CompiledRule, len(rules))
	copy(sortedRules, rules)

	// Sort by priority (highest first)
	for i := 0; i < len(sortedRules); i++ {
		for j := i + 1; j < len(sortedRules); j++ {
			if sortedRules[i].Priority < sortedRules[j].Priority {
				sortedRules[i], sortedRules[j] = sortedRules[j], sortedRules[i]
			}
		}
	}

	return sortedRules
}
