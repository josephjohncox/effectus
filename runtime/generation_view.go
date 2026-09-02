package runtime

import (
	"context"
	"fmt"

	"github.com/josephjohncox/effectus/compiler"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
)

// GenerationView is an immutable presentation snapshot of the generation used
// for new admissions. It contains no executor implementation or mutable state.
type GenerationView struct {
	Ruleset          string
	Version          string
	GenerationDigest string
	IRDigest         string
	SourceDigest     string
	Environment      ir.Environment
	Plans            []PlanView
}

// PlanView is a checked plan shown by status and dry-run APIs.
type PlanView struct {
	ID        string
	Dialect   effectusv1.SourceDialect
	Priority  int32
	Predicate string
	Verbs     []string
}

// GenerationView returns the single checked generation that backs admission,
// transport execution, status, and UI presentation.
func (engine *Engine) GenerationView() *GenerationView {
	if engine == nil || engine.runtime == nil {
		return nil
	}
	engine.runtime.mu.RLock()
	defer engine.runtime.mu.RUnlock()
	generation := engine.runtime.activeGeneration
	if generation == nil || generation.unit == nil || generation.unit.CheckedIR == nil {
		return nil
	}
	return generationView(generation, generation.unit)
}

func generationView(generation *ExecutionGeneration, compiled *compiler.CompiledUnit) *GenerationView {
	artifact := compiled.CheckedIR.CloneArtifact()
	view := &GenerationView{
		Ruleset: generation.Ruleset, Version: generation.Version,
		GenerationDigest: generation.GenerationDigest, IRDigest: generation.IRDigest,
		SourceDigest: compiled.SourceDigest, Environment: cloneGenerationEnvironment(compiled.IREnvironment),
		Plans: make([]PlanView, 0, len(artifact.Plans)),
	}
	for _, plan := range artifact.Plans {
		if plan == nil {
			continue
		}
		item := PlanView{ID: plan.Id, Dialect: plan.SourceDialect, Priority: plan.Priority, Verbs: make([]string, 0, len(plan.Steps))}
		if plan.Predicate != nil && plan.Predicate.Expression != nil {
			item.Predicate = plan.Predicate.Expression.String()
		}
		for _, step := range plan.Steps {
			if step != nil {
				item.Verbs = append(item.Verbs, step.Verb)
			}
		}
		view.Plans = append(view.Plans, item)
	}
	return view
}

// DryRun evaluates the active checked generation without durable admission,
// invocation, retries, or other external effects.
func (engine *Engine) DryRun(ctx context.Context, facts map[string]any) ([]PlanEvaluation, error) {
	if engine == nil || engine.runtime == nil || ctx == nil {
		return nil, fmt.Errorf("checked dry-run requires an engine and context")
	}
	engine.runtime.mu.RLock()
	generation := engine.runtime.activeGeneration
	if engine.runtime.state != StateReady || generation == nil || generation.unit == nil || generation.unit.CheckedIR == nil {
		engine.runtime.mu.RUnlock()
		return nil, fmt.Errorf("checked generation is unavailable")
	}
	unit := generation.unit
	engine.runtime.mu.RUnlock()

	effective := cloneWorkflowFacts(unit.InitialData)
	mergeWorkflowFactOverrides(effective, facts)
	if err := validateAdmissionFactTypes(unit.IREnvironment, effective); err != nil {
		return nil, err
	}
	artifact := unit.CheckedIR.CloneArtifact()
	result := make([]PlanEvaluation, 0, len(artifact.Plans))
	for _, plan := range artifact.Plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if plan == nil || plan.Predicate == nil {
			return nil, fmt.Errorf("checked generation has an invalid plan")
		}
		matched, err := evaluateCheckedPredicate(plan.Predicate.Expression, effective, unit)
		if err != nil {
			return nil, fmt.Errorf("evaluate checked plan %q: %w", plan.Id, err)
		}
		result = append(result, PlanEvaluation{Plan: PlanView{ID: plan.Id, Dialect: plan.SourceDialect, Priority: plan.Priority, Predicate: plan.Predicate.Expression.String(), Verbs: planVerbNames(plan)}, Matched: matched})
	}
	return result, nil
}

// PlanEvaluation is the result of evaluating one checked plan in DryRun.
type PlanEvaluation struct {
	Plan    PlanView
	Matched bool
}

func planVerbNames(plan *effectusv1.Plan) []string {
	verbs := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if step != nil {
			verbs = append(verbs, step.Verb)
		}
	}
	return verbs
}
