package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ErrRuleManagerPipelineUnsupported reports that the legacy rule-manager
// compiler, validator, and deployment controller do not have implementations.
var ErrRuleManagerPipelineUnsupported = errors.New("rule manager compilation and deployment pipeline is not implemented")

// RuleCompiler is a minimal placeholder for the rule compilation pipeline.
type RuleCompiler struct {
	settings *CompilerSettings
}

// NewRuleCompiler creates a rule compiler stub.
func NewRuleCompiler(settings *CompilerSettings) *RuleCompiler {
	return &RuleCompiler{settings: settings}
}

// CompileRuleset compiles a ruleset from rule files.
func (_ *RuleCompiler) CompileRuleset(_ context.Context, rulesetName string, _ []RuleFile) (*CompiledRuleset, error) {
	return nil, fmt.Errorf("compile ruleset %q: %w", rulesetName, ErrRuleManagerPipelineUnsupported)
}

// GetVersion returns the compiler version.
func (_ *RuleCompiler) GetVersion() string {
	return "unsupported"
}

// RuleValidator is a minimal placeholder for ruleset validation.
type RuleValidator struct {
	settings *ValidationSettings
}

// NewRuleValidator creates a rule validator stub.
func NewRuleValidator(settings *ValidationSettings) *RuleValidator {
	return &RuleValidator{settings: settings}
}

// ValidateRuleset validates a compiled ruleset.
func (_ *RuleValidator) ValidateRuleset(_ context.Context, _ *CompiledRuleset) error {
	return ErrRuleManagerPipelineUnsupported
}

// DeploymentController is a minimal placeholder for deployment orchestration.
type DeploymentController struct {
	settings *DeploymentSettings
	storage  RuleStorageBackend
}

// NewDeploymentController creates a deployment controller stub.
func NewDeploymentController(settings *DeploymentSettings, storage RuleStorageBackend) *DeploymentController {
	return &DeploymentController{settings: settings, storage: storage}
}

// Deploy performs a deployment.
func (_ *DeploymentController) Deploy(_ context.Context, _ *StoredRuleset, environment string, _ *DeploymentOptions) (*DeploymentResult, error) {
	return nil, fmt.Errorf("deploy to %q: %w", environment, ErrRuleManagerPipelineUnsupported)
}
