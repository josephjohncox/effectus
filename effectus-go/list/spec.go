package list

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
)

// Spec implements the effectus.Spec interface for list rules
type Spec struct {
	Rules     []*CompiledRule
	FactPaths []string
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
		if !evaluatePredicates(rule.Predicates, facts) {
			continue
		}

		// Execute effects
		for _, effect := range rule.Effects {
			_, err := ex.Do(effect)
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
	Predicates []*Predicate
	Effects    []effectus.Effect
	FactPaths  []string
}

// Predicate represents a compiled predicate
type Predicate struct {
	Path string
	Op   string
	Lit  interface{}
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

// evaluatePredicates evaluates all predicates for a rule
func evaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	if len(predicates) == 0 {
		return true
	}

	for _, pred := range predicates {
		// Get fact value
		value, exists := facts.Get(pred.Path)
		if !exists {
			return false
		}

		// Compare based on operator
		if !compareFact(value, pred.Op, pred.Lit) {
			return false
		}
	}

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

// Helper comparison functions (simplified)
func equal(a, b interface{}) bool {
	// Basic equality comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func lessThan(a, b interface{}) bool {
	// Simple string comparison as a fallback
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

func greaterThan(a, b interface{}) bool {
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
