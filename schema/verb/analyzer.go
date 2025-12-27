package verb

import (
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/ast"
)

// AnalyzerVerbRegistry defines the interface needed by the analyzer.
type AnalyzerVerbRegistry interface {
	GetVerb(name string) (*Spec, bool)
}

// CapabilityAnalyzer analyzes effects for capability conflicts
type CapabilityAnalyzer struct {
	registry AnalyzerVerbRegistry
}

// NewCapabilityAnalyzer creates a new capability analyzer
func NewCapabilityAnalyzer(registry AnalyzerVerbRegistry) *CapabilityAnalyzer {
	return &CapabilityAnalyzer{
		registry: registry,
	}
}

// AnalysisResult represents the result of a static analysis
type AnalysisResult struct {
	Conflicts   []Conflict
	Resources   map[string]Capability
	VerbsCalled []string
}

// Conflict represents a conflict between effects
type Conflict struct {
	Resource string
	Effects  []string
	Reason   string
}

// Analyze performs static analysis on a rule file
func (a *CapabilityAnalyzer) Analyze(file *ast.File) (*AnalysisResult, error) {
	result := &AnalysisResult{
		Resources:   make(map[string]Capability),
		VerbsCalled: []string{},
	}

	// Analyze rules
	for _, rule := range file.Rules {
		for _, block := range rule.Blocks {
			if block.Then != nil {
				for _, effect := range block.Then.Effects {
					a.analyzeEffect(effect, result)
				}
			}
		}
	}

	// Analyze flows
	for _, flow := range file.Flows {
		if flow.Steps != nil {
			for _, step := range flow.Steps.Steps {
				a.analyzeStep(step, result)
			}
		}
	}

	// Detect conflicts
	a.detectConflicts(result)

	return result, nil
}

// analyzeEffect analyzes a single effect for capability usage
func (a *CapabilityAnalyzer) analyzeEffect(effect *ast.Effect, result *AnalysisResult) {
	verbName := effect.Verb

	// Record verb call
	result.VerbsCalled = append(result.VerbsCalled, verbName)

	// Check if verb exists in registry
	verbSpec, exists := a.registry.GetVerb(verbName)
	if !exists {
		// Skip analysis for unknown verbs
		return
	}

	// Track resource usage
	for _, resource := range verbSpec.Resources {
		// If resource already tracked, combine capabilities
		if existingCap, exists := result.Resources[resource.Resource]; exists {
			result.Resources[resource.Resource] = existingCap | resource.Cap
		} else {
			result.Resources[resource.Resource] = resource.Cap
		}
	}
}

// analyzeStep analyzes a single step for capability usage (similar to analyzeEffect)
func (a *CapabilityAnalyzer) analyzeStep(step *ast.Step, result *AnalysisResult) {
	verbName := step.Verb

	// Record verb call
	result.VerbsCalled = append(result.VerbsCalled, verbName)

	// Check if verb exists in registry
	verbSpec, exists := a.registry.GetVerb(verbName)
	if !exists {
		// Skip analysis for unknown verbs
		return
	}

	// Track resource usage
	for _, resource := range verbSpec.Resources {
		// If resource already tracked, combine capabilities
		if existingCap, exists := result.Resources[resource.Resource]; exists {
			result.Resources[resource.Resource] = existingCap | resource.Cap
		} else {
			result.Resources[resource.Resource] = resource.Cap
		}
	}
}

// detectConflicts detects conflicts in the analyzed effects
func (a *CapabilityAnalyzer) detectConflicts(result *AnalysisResult) {
	// Maps to track verb usage per resource
	resourceVerbs := make(map[string][]string)

	// Group verbs by resource
	for _, verbName := range result.VerbsCalled {
		verbSpec, exists := a.registry.GetVerb(verbName)
		if !exists {
			continue
		}

		for _, resource := range verbSpec.Resources {
			resourceVerbs[resource.Resource] = append(resourceVerbs[resource.Resource], verbName)
		}
	}

	// Check each resource for conflicts
	for resource, verbs := range resourceVerbs {
		// Skip if only one verb touches the resource
		if len(verbs) <= 1 {
			continue
		}

		// Check for write conflicts (multiple non-commutative writes)
		writeVerbs := []string{}
		hasConflict := false

		for _, verbName := range verbs {
			verbSpec, _ := a.registry.GetVerb(verbName)

			// Find resource capabilities
			var resourceCap Capability
			for _, res := range verbSpec.Resources {
				if res.Resource == resource {
					resourceCap = res.Cap
					break
				}
			}

			// If it's a write operation
			if resourceCap&CapWrite != 0 {
				// Check if it's commutative
				if resourceCap&CapCommutative == 0 {
					writeVerbs = append(writeVerbs, verbName)

					// If we have multiple non-commutative writes, it's a conflict
					if len(writeVerbs) > 1 {
						hasConflict = true
					}
				}
			}
		}

		// Record conflict if found
		if hasConflict {
			result.Conflicts = append(result.Conflicts, Conflict{
				Resource: resource,
				Effects:  writeVerbs,
				Reason:   "Multiple non-commutative write operations on same resource",
			})
		}

		// Check for exclusive conflicts (exclusive verb with any other verb)
		for _, verbName := range verbs {
			verbSpec, _ := a.registry.GetVerb(verbName)

			if verbSpec.Capability.IsExclusive() {
				// If exclusive verb is used with others, it's a conflict
				if len(verbs) > 1 {
					result.Conflicts = append(result.Conflicts, Conflict{
						Resource: resource,
						Effects:  verbs,
						Reason:   fmt.Sprintf("Exclusive verb %s used with other verbs on same resource", verbName),
					})
					break
				}
			}
		}
	}
}

// FormatAnalysisResult formats the analysis result as a string
func FormatAnalysisResult(result *AnalysisResult) string {
	var sb strings.Builder

	// Report conflicts
	if len(result.Conflicts) > 0 {
		sb.WriteString(fmt.Sprintf("Found %d potential conflicts:\n", len(result.Conflicts)))
		for i, conflict := range result.Conflicts {
			sb.WriteString(fmt.Sprintf("  %d. Resource: %s\n", i+1, conflict.Resource))
			sb.WriteString(fmt.Sprintf("     Effects: %s\n", strings.Join(conflict.Effects, ", ")))
			sb.WriteString(fmt.Sprintf("     Reason: %s\n", conflict.Reason))
		}
	} else {
		sb.WriteString("No capability conflicts detected.\n")
	}

	// Report resource usage
	sb.WriteString("\nResource capabilities used:\n")
	for resource, cap := range result.Resources {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", resource, cap.String()))
	}

	return sb.String()
}
