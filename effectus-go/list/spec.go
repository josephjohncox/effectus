package list

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
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
		if !rule.Matches(facts) {
			continue
		}

		// Execute effects
		for _, effect := range rule.Effects {
			// Convert to effectus.Effect
			execEffect := effectus.Effect{
				Verb:    effect.Verb,
				Payload: effect.Args,
			}
			_, err := ex.Do(execEffect)
			if err != nil {
				return fmt.Errorf("error executing effect %s: %w", effect.Verb, err)
			}
		}
	}

	return nil
}

// CompiledRule represents a rule after compilation
type CompiledRule struct {
	Name       string
	Priority   int
	Predicates []*common.Predicate
	Effects    []*Effect
	FactPaths  []string
}

// Matches checks if a rule matches the given facts
func (r *CompiledRule) Matches(facts effectus.Facts) bool {
	// All predicates must match
	for _, pred := range r.Predicates {
		// Get the fact value
		factValue, exists := facts.Get(pred.Path)
		if !exists {
			return false
		}

		// Compare using the operator
		switch pred.Op {
		case "==":
			if factValue != pred.Lit {
				return false
			}
		case "!=":
			if factValue == pred.Lit {
				return false
			}
		case ">":
			// Handle numeric comparisons
			if !compareGreaterThan(factValue, pred.Lit) {
				return false
			}
		case ">=":
			// Handle numeric comparisons
			if !compareGreaterOrEqual(factValue, pred.Lit) {
				return false
			}
		case "<":
			// Handle numeric comparisons
			if !compareLessThan(factValue, pred.Lit) {
				return false
			}
		case "<=":
			// Handle numeric comparisons
			if !compareLessOrEqual(factValue, pred.Lit) {
				return false
			}
		case "in":
			// Check if fact value is in a collection
			if !isInCollection(factValue, pred.Lit) {
				return false
			}
		case "contains":
			// Check if fact value contains an item
			if !contains(factValue, pred.Lit) {
				return false
			}
		default:
			// Unknown operator
			return false
		}
	}

	// All predicates matched
	return true
}

// Helper comparison functions - these would be implemented
// to handle different numeric types and conversions
func compareGreaterThan(a, b interface{}) bool {
	// Implementation would handle different numeric types
	return false
}

func compareGreaterOrEqual(a, b interface{}) bool {
	// Implementation would handle different numeric types
	return false
}

func compareLessThan(a, b interface{}) bool {
	// Implementation would handle different numeric types
	return false
}

func compareLessOrEqual(a, b interface{}) bool {
	// Implementation would handle different numeric types
	return false
}

func isInCollection(item, collection interface{}) bool {
	// Implementation would handle different collection types
	return false
}

func contains(collection, item interface{}) bool {
	// Implementation would handle different collection types
	return false
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
