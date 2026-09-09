package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
)

func prepareAdmission(input *Admission) (*Admission, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: admission is nil", ErrInvalidExecuteRequest)
	}
	owned := *input
	owned.ExecutionID = strings.TrimSpace(owned.ExecutionID)
	owned.AdmissionID = strings.TrimSpace(owned.AdmissionID)
	owned.TenantNamespace = strings.TrimSpace(owned.TenantNamespace)
	owned.Ruleset = strings.TrimSpace(owned.Ruleset)
	owned.Version = strings.TrimSpace(owned.Version)
	owned.MergePolicy = strings.TrimSpace(owned.MergePolicy)
	if owned.ExecutionID == "" || owned.TenantNamespace == "" {
		return nil, fmt.Errorf("%w: stable execution ID and tenant namespace are required", ErrInvalidExecuteRequest)
	}
	if owned.MergePolicy == "" {
		owned.MergePolicy = "merge"
	}
	if owned.MergePolicy != "merge" && owned.MergePolicy != "replace" {
		return nil, fmt.Errorf("%w: unsupported fact merge policy %q", ErrInvalidExecuteRequest, owned.MergePolicy)
	}
	return &owned, nil
}

// Keep admissionHash itself unchanged: persisted legacy hash goldens remain a
// compatibility contract. New identities hash the effective declared values.
func semanticAdmissionHash(admission *Admission, environment ir.Environment) (string, error) {
	facts, err := normalizedWorkflowFacts(environment, admission.Facts)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidExecuteRequest, err)
	}
	// Flattening retains aggregate objects for lookup. Undeclared intermediate
	// objects are redundant in the identity view; declared objects remain
	// meaningful values. Unknown leaves remain significant, including numeric
	// spelling where no declared type permits normalization.
	for path, value := range facts {
		if _, declared := environment.Facts[path]; !declared {
			if object, ok := value.(map[string]any); ok && len(object) != 0 {
				delete(facts, path)
			}
		}
	}
	copy := *admission
	copy.Facts = facts
	return admissionHash(&copy)
}

func admissionIdentity(admission *Admission) string {
	if admission.AdmissionID != "" {
		return admission.AdmissionID
	}
	return admission.ExecutionID
}

func validateArtifactIdentity(record schema.ExecutionRecord, artifact schema.ExecutionArtifact) error {
	var identity struct {
		Ruleset     string            `json:"ruleset"`
		Version     string            `json:"version"`
		FunctionIDs map[string]string `json:"function_ids"`
	}
	if err := strictArtifactJSON(artifact.FunctionManifest, &identity); err != nil {
		return fmt.Errorf("%w: invalid pinned identity: %v", ErrGenerationMismatch, err)
	}
	if artifact.GenerationDigest != record.GenerationDigest || identity.Ruleset != record.Ruleset || identity.Version != record.Version {
		return fmt.Errorf("%w: durable record labels do not match its pinned artifact", ErrGenerationMismatch)
	}
	return nil
}

func (engine *Engine) matchReplay(ctx context.Context, admission *Admission, record schema.ExecutionRecord) error {
	if admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != record.GenerationDigest {
		return ErrGenerationMismatch
	}
	if record.ExecutionID != admission.ExecutionID || record.AdmissionIdentity != admissionIdentity(admission) || record.TenantNamespace != admission.TenantNamespace || record.Ruleset != admission.Ruleset || record.Version != admission.Version {
		return fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, admissionIdentity(admission))
	}
	policy := record.MergePolicy
	if policy == "" {
		policy = "merge"
	}
	if policy != admission.MergePolicy {
		return ErrIdentityConflict
	}
	artifact, err := engine.ledger.GetArtifact(ctx, record.GenerationDigest)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: read pinned artifact: %v", ErrBlockedDependency, err)
	}
	if err := validateArtifactIdentity(record, artifact); err != nil {
		return err
	}
	environment, err := decodeArtifactEnvironment(artifact)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlockedDependency, err)
	}
	hash, err := semanticAdmissionHash(admission, environment)
	if err != nil {
		return err
	}
	if record.RequestHash == hash {
		return nil
	}
	// No legacy row is rewritten. Prove semantic equivalence against frozen
	// effective facts using the original pinned environment, never the active
	// generation. Raw lexical hashes alone cannot establish that equivalence.
	frozen, err := decodeExecutionFacts(record.EffectiveFacts)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlockedDependency, err)
	}
	old := *admission
	old.Facts = frozen
	oldHash, err := semanticAdmissionHash(&old, environment)
	if err != nil {
		return fmt.Errorf("%w: invalid frozen facts: %v", ErrBlockedDependency, err)
	}
	if hash != oldHash {
		return fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, admissionIdentity(admission))
	}
	return nil
}
