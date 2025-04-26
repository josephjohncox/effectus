package unified

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/eval"
)

// Spec implements the effectus.Spec interface for both list and flow style rules
type Spec struct {
	ListSpec *list.Spec
	FlowSpec *flow.Spec
	Name     string
}

// RequiredFacts returns all fact paths required by the unified spec
func (s *Spec) RequiredFacts() []string {
	factPathSet := make(map[string]struct{})

	// Add list spec fact paths
	if s.ListSpec != nil {
		for _, path := range s.ListSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Add flow spec fact paths
	if s.FlowSpec != nil {
		for _, path := range s.FlowSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	factPaths := make([]string, 0, len(factPathSet))
	for path := range factPathSet {
		factPaths = append(factPaths, path)
	}

	return factPaths
}

// GetName returns the name of the spec
func (s *Spec) GetName() string {
	return s.Name
}

// Rule represents either a list rule or flow in a combined execution model
type Rule struct {
	Name       string
	Priority   int
	SourceFile string
	RuleType   string // "list" or "flow"
	ListRule   *list.CompiledRule
	FlowRule   *flow.CompiledFlow
}

// Execute executes the unified spec against a context
// This implements the effectus.Spec interface
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Create a unified list of rules
	unifiedRules := make([]*Rule, 0)

	// Add list rules if any
	if s.ListSpec != nil && len(s.ListSpec.Rules) > 0 {
		for _, rule := range s.ListSpec.Rules {
			unifiedRules = append(unifiedRules, &Rule{
				Name:     rule.Name,
				Priority: rule.Priority,
				RuleType: "list",
				ListRule: rule,
			})
		}
	}

	// Add flow rules if any
	if s.FlowSpec != nil && len(s.FlowSpec.Flows) > 0 {
		for _, flow := range s.FlowSpec.Flows {
			unifiedRules = append(unifiedRules, &Rule{
				Name:       flow.Name,
				Priority:   flow.Priority,
				SourceFile: flow.SourceFile,
				RuleType:   "flow",
				FlowRule:   flow,
			})
		}
	}

	// Sort unified rules by priority (highest first)
	sortRulesByPriority(unifiedRules)

	// Create an executor wrapper for flow rules
	flowExecutor := &contextExecutor{
		ctx:      ctx,
		executor: ex,
	}

	// Execute each rule in priority order
	for _, rule := range unifiedRules {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch rule.RuleType {
		case "list":
			// Evaluate predicates manually
			if !eval.EvaluatePredicates(rule.ListRule.Predicates, facts) {
				continue
			}

			// Execute the list rule effects
			for _, effect := range rule.ListRule.Effects {
				_, err := ex.Do(effect)
				if err != nil {
					return fmt.Errorf("error executing list rule %s effect %s: %w",
						rule.Name, effect.Verb, err)
				}
			}

		case "flow":
			// Evaluate predicates manually
			if !eval.EvaluatePredicates(rule.FlowRule.Predicates, facts) {
				continue
			}

			// Execute the flow program
			_, err := flow.Run(rule.FlowRule.Program, flowExecutor)
			if err != nil {
				return fmt.Errorf("error executing flow %s: %w", rule.Name, err)
			}
		}
	}

	return nil
}

// sortRulesByPriority sorts unified rules by priority (highest first)
func sortRulesByPriority(rules []*Rule) {
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[i].Priority < rules[j].Priority {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
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
