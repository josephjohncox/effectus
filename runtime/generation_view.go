package runtime

import (
	"context"
	"fmt"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
)

type GenerationView struct {
	Ruleset          string
	Version          string
	GenerationDigest string
	IRDigest         string
	SourceDigest     string
	Environment      ir.Environment
	Plans            []PlanView
}
type PlanView struct {
	ID        string
	Dialect   effectusv1.SourceDialect
	Priority  int32
	Predicate string
	Verbs     []string
}

func (engine *Engine) GenerationView() *GenerationView {
	if engine == nil {
		return nil
	}
	return generationView(engine.Generation())
}
func generationView(generation *Generation) *GenerationView {
	if generation == nil || generation.Checked() == nil {
		return nil
	}
	artifact := generation.Checked().CloneArtifact()
	view := &GenerationView{Ruleset: generation.Ruleset(), Version: generation.Version(), GenerationDigest: generation.Digest(), IRDigest: generation.Checked().Digest(), SourceDigest: generation.SourceDigest(), Environment: generation.Environment(), Plans: make([]PlanView, 0, len(artifact.Plans))}
	for _, plan := range artifact.Plans {
		if plan == nil {
			continue
		}
		item := PlanView{ID: plan.Id, Dialect: plan.SourceDialect, Priority: plan.Priority, Verbs: planVerbNames(plan)}
		if plan.Predicate != nil && plan.Predicate.Expression != nil {
			item.Predicate = plan.Predicate.Expression.String()
		}
		view.Plans = append(view.Plans, item)
	}
	return view
}
func (engine *Engine) DryRun(ctx context.Context, facts map[string]any) ([]PlanEvaluation, error) {
	if engine == nil || ctx == nil {
		return nil, fmt.Errorf("checked dry-run requires an engine and context")
	}
	generation := engine.Generation()
	if generation == nil || generation.Checked() == nil {
		return nil, fmt.Errorf("checked generation is unavailable")
	}
	effective := cloneWorkflowFacts(facts)
	if err := validateAdmissionFactTypes(generation.Environment(), effective); err != nil {
		return nil, err
	}
	artifact := generation.Checked().CloneArtifact()
	result := make([]PlanEvaluation, 0, len(artifact.Plans))
	for _, plan := range artifact.Plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if plan == nil || plan.Predicate == nil {
			return nil, fmt.Errorf("checked generation has an invalid plan")
		}
		matched, err := evaluateCheckedPredicate(plan.Predicate.Expression, effective, generation)
		if err != nil {
			return nil, fmt.Errorf("evaluate checked plan %q: %w", plan.Id, err)
		}
		result = append(result, PlanEvaluation{Plan: PlanView{ID: plan.Id, Dialect: plan.SourceDialect, Priority: plan.Priority, Predicate: plan.Predicate.Expression.String(), Verbs: planVerbNames(plan)}, Matched: matched})
	}
	return result, nil
}

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
