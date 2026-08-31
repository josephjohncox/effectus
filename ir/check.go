package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	// ErrInvalidArtifact identifies a structural or semantic IR failure.
	ErrInvalidArtifact = errors.New("invalid checked IR artifact")
	// ErrLimitExceeded identifies a configured structural or value limit.
	ErrLimitExceeded = errors.New("checked IR limit exceeded")
)

// Limits bounds untrusted artifacts. A zero field uses DefaultLimits.
type Limits struct {
	MaxArtifactBytes    int
	MaxPlans            int
	MaxSteps            int
	MaxStepsPerPlan     int
	MaxArgumentsPerStep int
	MaxPredicateNodes   int
	MaxLiteralNodes     int
	MaxDepth            int
	MaxStringBytes      int
	MaxBytesValue       int
	MaxCollectionItems  int
	MaxObjectFields     int
	MaxTotalStringBytes int
}

// DefaultLimits are conservative production defaults.
var DefaultLimits = Limits{
	MaxArtifactBytes:    4 << 20,
	MaxPlans:            1_000,
	MaxSteps:            10_000,
	MaxStepsPerPlan:     1_000,
	MaxArgumentsPerStep: 128,
	MaxPredicateNodes:   1_024,
	MaxLiteralNodes:     10_000,
	MaxDepth:            64,
	MaxStringBytes:      1 << 20,
	MaxBytesValue:       1 << 20,
	MaxCollectionItems:  10_000,
	MaxObjectFields:     1_024,
	MaxTotalStringBytes: 4 << 20,
}

// Checked is an opaque, immutable execution plan. It contains no callbacks.
type Checked struct {
	wire        []byte
	digest      string
	planCount   int
	stepCount   int
	artifactLen int
}

// Parse decodes untrusted protobuf bytes and rechecks every reference.
func Parse(data []byte, environment Environment, limits Limits) (*Checked, error) {
	limits = limits.withDefaults()
	if len(data) > limits.MaxArtifactBytes {
		return nil, limitError("artifact bytes", len(data), limits.MaxArtifactBytes)
	}
	artifact := new(effectusv1.RuleArtifact)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, artifact); err != nil {
		return nil, invalid("decode protobuf: %v", err)
	}
	return Check(artifact, environment, limits)
}

// Check validates an artifact and stores only its deterministic protobuf form.
// The input and environment may be mutated after this call without changing
// the returned Checked value.
func Check(artifact *effectusv1.RuleArtifact, environment Environment, limits Limits) (*Checked, error) {
	limits = limits.withDefaults()
	if artifact == nil {
		return nil, invalid("artifact is nil")
	}
	if size := proto.Size(artifact); size > limits.MaxArtifactBytes {
		return nil, limitError("artifact bytes", size, limits.MaxArtifactBytes)
	}
	if err := rejectUnknownFields(artifact.ProtoReflect()); err != nil {
		return nil, err
	}
	normalizedEnvironment, err := normalizeEnvironment(environment)
	if err != nil {
		return nil, invalid("environment: %v", err)
	}
	clone := proto.Clone(artifact).(*effectusv1.RuleArtifact)
	checker := artifactChecker{
		artifact:    clone,
		environment: normalizedEnvironment,
		types:       typeChecker{environment: normalizedEnvironment},
		limits:      limits,
		planIDs:     make(map[string]struct{}),
		stepIDs:     make(map[string]struct{}),
		sourceOrders: map[effectusv1.SourceDialect]map[uint32]struct{}{
			effectusv1.SourceDialect_SOURCE_DIALECT_LIST: make(map[uint32]struct{}),
			effectusv1.SourceDialect_SOURCE_DIALECT_FLOW: make(map[uint32]struct{}),
		},
	}
	if err := checker.check(); err != nil {
		return nil, err
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(clone)
	if err != nil {
		return nil, invalid("encode canonical protobuf: %v", err)
	}
	if len(wire) > limits.MaxArtifactBytes {
		return nil, limitError("artifact bytes", len(wire), limits.MaxArtifactBytes)
	}
	digest := sha256.Sum256(wire)
	return &Checked{
		wire:        wire,
		digest:      hex.EncodeToString(digest[:]),
		planCount:   len(clone.Plans),
		stepCount:   checker.stepCount,
		artifactLen: len(wire),
	}, nil
}

// Marshal returns a copy of the deterministic protobuf representation.
func (c *Checked) Marshal() []byte {
	if c == nil {
		return nil
	}
	return append([]byte(nil), c.wire...)
}

// CloneArtifact returns a mutable copy for compatibility and inspection. The
// returned value is not checked state and must be passed through Check again.
func (c *Checked) CloneArtifact() *effectusv1.RuleArtifact {
	if c == nil {
		return nil
	}
	artifact := new(effectusv1.RuleArtifact)
	if err := proto.Unmarshal(c.wire, artifact); err != nil {
		panic("ir.Checked contains invalid internal protobuf: " + err.Error())
	}
	return artifact
}

// Digest returns the SHA-256 digest of Marshal.
func (c *Checked) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *Checked) PlanCount() int {
	if c == nil {
		return 0
	}
	return c.planCount
}

func (c *Checked) StepCount() int {
	if c == nil {
		return 0
	}
	return c.stepCount
}

func (c *Checked) Size() int {
	if c == nil {
		return 0
	}
	return c.artifactLen
}

func (limits Limits) withDefaults() Limits {
	result := limits
	defaults := DefaultLimits
	fields := []*int{
		&result.MaxArtifactBytes, &result.MaxPlans, &result.MaxSteps,
		&result.MaxStepsPerPlan, &result.MaxArgumentsPerStep,
		&result.MaxPredicateNodes, &result.MaxLiteralNodes, &result.MaxDepth,
		&result.MaxStringBytes, &result.MaxBytesValue,
		&result.MaxCollectionItems, &result.MaxObjectFields,
		&result.MaxTotalStringBytes,
	}
	defaultFields := []int{
		defaults.MaxArtifactBytes, defaults.MaxPlans, defaults.MaxSteps,
		defaults.MaxStepsPerPlan, defaults.MaxArgumentsPerStep,
		defaults.MaxPredicateNodes, defaults.MaxLiteralNodes, defaults.MaxDepth,
		defaults.MaxStringBytes, defaults.MaxBytesValue,
		defaults.MaxCollectionItems, defaults.MaxObjectFields,
		defaults.MaxTotalStringBytes,
	}
	for i, field := range fields {
		if *field <= 0 {
			*field = defaultFields[i]
		}
	}
	return result
}

type artifactChecker struct {
	artifact     *effectusv1.RuleArtifact
	environment  Environment
	types        typeChecker
	limits       Limits
	planIDs      map[string]struct{}
	stepIDs      map[string]struct{}
	sourceOrders map[effectusv1.SourceDialect]map[uint32]struct{}
	stepCount    int
	literalNodes int
	stringBytes  int
}

func (c *artifactChecker) check() error {
	if c.artifact.FormatVersion != FormatVersion {
		return invalid("format_version is %d, want %d", c.artifact.FormatVersion, FormatVersion)
	}
	if c.artifact.Compiler == nil {
		return invalid("compiler metadata is required")
	}
	if err := c.text("compiler.name", c.artifact.Compiler.Name, true); err != nil {
		return err
	}
	if err := c.text("compiler.version", c.artifact.Compiler.Version, true); err != nil {
		return err
	}
	if err := checkDigest("compiler.build_digest", c.artifact.Compiler.BuildDigest); err != nil {
		return err
	}
	if err := c.text("compiler.build_digest", c.artifact.Compiler.BuildDigest, true); err != nil {
		return err
	}
	expectedEnvironmentDigest, err := EnvironmentDigest(c.environment)
	if err != nil {
		return invalid("environment digest: %v", err)
	}
	if c.artifact.EnvironmentDigest != expectedEnvironmentDigest {
		return invalid("environment_digest does not match the supplied environment")
	}
	if err := c.text("environment_digest", c.artifact.EnvironmentDigest, true); err != nil {
		return err
	}
	if len(c.artifact.Plans) > c.limits.MaxPlans {
		return limitError("plans", len(c.artifact.Plans), c.limits.MaxPlans)
	}
	for index, plan := range c.artifact.Plans {
		if plan == nil {
			return invalid("plan %d is nil", index)
		}
		if index > 0 && comparePlans(c.artifact.Plans[index-1], plan) >= 0 {
			return invalid("plans are not in canonical dialect, priority, and source order at index %d", index)
		}
		if err := c.checkPlan(index, plan); err != nil {
			return err
		}
	}
	return nil
}

func comparePlans(left, right *effectusv1.Plan) int {
	if left.SourceDialect != right.SourceDialect {
		return int(left.SourceDialect) - int(right.SourceDialect)
	}
	if left.Priority != right.Priority {
		if left.Priority > right.Priority {
			return -1
		}
		return 1
	}
	if left.SourceOrder != right.SourceOrder {
		if left.SourceOrder < right.SourceOrder {
			return -1
		}
		return 1
	}
	return strings.Compare(left.Id, right.Id)
}

func (c *artifactChecker) checkPlan(index int, plan *effectusv1.Plan) error {
	location := fmt.Sprintf("plan[%d]", index)
	if err := c.text(location+".id", plan.Id, true); err != nil {
		return err
	}
	if _, duplicate := c.planIDs[plan.Id]; duplicate {
		return invalid("%s has duplicate id %q", location, plan.Id)
	}
	c.planIDs[plan.Id] = struct{}{}
	switch plan.SourceDialect {
	case effectusv1.SourceDialect_SOURCE_DIALECT_LIST, effectusv1.SourceDialect_SOURCE_DIALECT_FLOW:
	default:
		return invalid("%s has invalid source_dialect %d", location, plan.SourceDialect)
	}
	orders := c.sourceOrders[plan.SourceDialect]
	if _, duplicate := orders[plan.SourceOrder]; duplicate {
		return invalid("%s duplicates source_order %d for %s", location, plan.SourceOrder, plan.SourceDialect)
	}
	orders[plan.SourceOrder] = struct{}{}
	switch plan.ExecutionPolicy {
	case effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST,
		effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING:
	default:
		return invalid("%s has invalid execution_policy %d", location, plan.ExecutionPolicy)
	}
	if plan.Predicate == nil || plan.Predicate.Expression == nil {
		return invalid("%s predicate is required", location)
	}
	predicateNodes := 0
	predicateType, err := c.checkExpression(plan.Predicate.Expression, 1, &predicateNodes)
	if err != nil {
		return contextualInvalid(location+" predicate", err)
	}
	if predicateType.kind != typeBool {
		return invalid("%s predicate returns %s, want bool", location, c.types.describe(predicateType))
	}
	if len(plan.Steps) > c.limits.MaxStepsPerPlan {
		return limitError(location+" steps", len(plan.Steps), c.limits.MaxStepsPerPlan)
	}
	c.stepCount += len(plan.Steps)
	if c.stepCount > c.limits.MaxSteps {
		return limitError("steps", c.stepCount, c.limits.MaxSteps)
	}
	slots := make([]*typeRef, 0, len(plan.Steps))
	for stepIndex, step := range plan.Steps {
		if err := c.checkStep(location, stepIndex, step, &slots); err != nil {
			return err
		}
	}
	return nil
}

func (c *artifactChecker) checkStep(planLocation string, index int, step *effectusv1.Step, slots *[]*typeRef) error {
	location := fmt.Sprintf("%s.step[%d]", planLocation, index)
	if step == nil {
		return invalid("%s is nil", location)
	}
	if step.Ordinal != uint32(index) {
		return invalid("%s ordinal is %d, want %d", location, step.Ordinal, index)
	}
	if err := c.text(location+".id", step.Id, true); err != nil {
		return err
	}
	if _, duplicate := c.stepIDs[step.Id]; duplicate {
		return invalid("%s has duplicate id %q", location, step.Id)
	}
	c.stepIDs[step.Id] = struct{}{}
	if err := c.text(location+".verb", step.Verb, true); err != nil {
		return err
	}
	contract, ok := c.environment.Verbs[step.Verb]
	if !ok {
		return invalid("%s references unknown verb %q", location, step.Verb)
	}
	expectedHash, err := ContractHash(contract)
	if err != nil {
		return invalid("%s contract: %v", location, err)
	}
	if step.ContractHash != expectedHash {
		return invalid("%s contract_hash does not match verb %q", location, step.Verb)
	}
	if err := c.text(location+".contract_hash", step.ContractHash, true); err != nil {
		return err
	}
	if err := c.checkStepPolicies(location, step, contract); err != nil {
		return err
	}
	if len(step.Arguments) > c.limits.MaxArgumentsPerStep {
		return limitError(location+" arguments", len(step.Arguments), c.limits.MaxArgumentsPerStep)
	}
	provided := make(map[string]struct{}, len(step.Arguments))
	previousName := ""
	for argumentIndex, argument := range step.Arguments {
		argumentLocation := fmt.Sprintf("%s.argument[%d]", location, argumentIndex)
		if argument == nil {
			return invalid("%s is nil", argumentLocation)
		}
		if err := c.text(argumentLocation+".name", argument.Name, true); err != nil {
			return err
		}
		if argumentIndex > 0 && argument.Name <= previousName {
			return invalid("%s arguments are not uniquely ordered by name", location)
		}
		previousName = argument.Name
		expectedTypeName, exists := contract.Arguments[argument.Name]
		if !exists {
			return invalid("%s is not declared by verb %q", argumentLocation, step.Verb)
		}
		expectedType, err := c.types.parse(expectedTypeName, false)
		if err != nil {
			return invalid("%s type: %v", argumentLocation, err)
		}
		if err := c.checkValue(argument.Value, expectedType, *slots, argumentLocation); err != nil {
			return err
		}
		provided[argument.Name] = struct{}{}
	}
	for _, required := range contract.RequiredArgs {
		if _, ok := provided[required]; !ok {
			return invalid("%s is missing required argument %q", location, required)
		}
	}
	resultType, err := c.types.parse(contract.ResultType, true)
	if err != nil {
		return invalid("%s result type: %v", location, err)
	}
	if step.ResultSlot != nil {
		if resultType.kind == typeVoid {
			return invalid("%s binds a void result", location)
		}
		if *step.ResultSlot != uint32(len(*slots)) {
			return invalid("%s result_slot is %d, want dense slot %d", location, *step.ResultSlot, len(*slots))
		}
		*slots = append(*slots, resultType)
	}
	return nil
}

func (c *artifactChecker) checkStepPolicies(location string, step *effectusv1.Step, contract VerbContract) error {
	expectedRetry := &effectusv1.CheckedRetryPolicy{
		MaxAttempts:          contract.RetryPolicy.MaxAttempts,
		InitialBackoffMillis: contract.RetryPolicy.InitialBackoffMillis,
		MaxBackoffMillis:     contract.RetryPolicy.MaxBackoffMillis,
	}
	if step.RetryPolicy == nil {
		step.RetryPolicy = expectedRetry
	} else if !proto.Equal(step.RetryPolicy, expectedRetry) {
		return invalid("%s retry_policy does not match verb %q", location, step.Verb)
	}
	expectedIdempotency := effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_NONE
	switch contract.IdempotencyPolicy {
	case IdempotencyKeyRequired:
		expectedIdempotency = effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_KEY_REQUIRED
	case IdempotencySinkGuaranteed:
		expectedIdempotency = effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED
	}
	if step.IdempotencyPolicy == effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_UNSPECIFIED {
		step.IdempotencyPolicy = expectedIdempotency
	} else if step.IdempotencyPolicy != expectedIdempotency {
		return invalid("%s idempotency_policy does not match verb %q", location, step.Verb)
	}
	expectedFencing := effectusv1.FencingRequirement_FENCING_REQUIREMENT_NONE
	if contract.FencingRequired {
		expectedFencing = effectusv1.FencingRequirement_FENCING_REQUIREMENT_REQUIRED
	}
	if step.FencingRequirement == effectusv1.FencingRequirement_FENCING_REQUIREMENT_UNSPECIFIED {
		step.FencingRequirement = expectedFencing
	} else if step.FencingRequirement != expectedFencing {
		return invalid("%s fencing_requirement does not match verb %q", location, step.Verb)
	}
	if contract.InverseVerb == "" {
		if step.Compensation != nil {
			return invalid("%s has compensation but verb %q has no inverse", location, step.Verb)
		}
		return nil
	}
	inverse, ok := c.environment.Verbs[contract.InverseVerb]
	if !ok {
		return invalid("%s verb %q references unknown inverse %q", location, step.Verb, contract.InverseVerb)
	}
	inverseHash, err := ContractHash(inverse)
	if err != nil {
		return invalid("%s inverse contract: %v", location, err)
	}
	if step.Compensation == nil {
		step.Compensation = &effectusv1.CompensationContract{InverseVerb: contract.InverseVerb, InverseContractHash: inverseHash}
	} else if step.Compensation.InverseVerb != contract.InverseVerb || step.Compensation.InverseContractHash != inverseHash {
		return invalid("%s compensation does not match verb %q", location, step.Verb)
	}
	return nil
}

func (c *artifactChecker) checkValue(value *effectusv1.Value, expected *typeRef, slots []*typeRef, location string) error {
	if value == nil {
		return invalid("%s value is nil", location)
	}
	switch kind := value.Kind.(type) {
	case *effectusv1.Value_Literal:
		if err := c.checkLiteralStructure(kind.Literal, 1); err != nil {
			return contextualInvalid(location+" literal", err)
		}
		if err := c.types.literalAssignable(kind.Literal, expected); err != nil {
			return invalid("%s: %v", location, err)
		}
	case *effectusv1.Value_FactPath:
		if err := c.text(location+" fact_path", kind.FactPath, true); err != nil {
			return err
		}
		typeName, ok := c.environment.Facts[kind.FactPath]
		if !ok {
			return invalid("%s references unknown fact %q", location, kind.FactPath)
		}
		actual, err := c.types.parse(typeName, false)
		if err != nil || !c.types.assignable(actual, expected) {
			return invalid("%s fact %q type is incompatible with %s", location, kind.FactPath, c.types.describe(expected))
		}
	case *effectusv1.Value_ResultSlot:
		if uint64(kind.ResultSlot) >= uint64(len(slots)) {
			return invalid("%s references non-preceding result slot %d", location, kind.ResultSlot)
		}
		if !c.types.assignable(slots[kind.ResultSlot], expected) {
			return invalid("%s result slot %d type is incompatible with %s", location, kind.ResultSlot, c.types.describe(expected))
		}
	default:
		return invalid("%s value kind is not set", location)
	}
	return nil
}

func (c *artifactChecker) checkExpression(expression *effectusv1.Expression, depth int, nodes *int) (*typeRef, error) {
	if expression == nil {
		return nil, fmt.Errorf("expression is nil")
	}
	if depth > c.limits.MaxDepth {
		return nil, limitError("predicate depth", depth, c.limits.MaxDepth)
	}
	*nodes++
	if *nodes > c.limits.MaxPredicateNodes {
		return nil, limitError("predicate nodes", *nodes, c.limits.MaxPredicateNodes)
	}
	switch kind := expression.Kind.(type) {
	case *effectusv1.Expression_Literal:
		if err := c.checkLiteralStructure(kind.Literal, depth); err != nil {
			return nil, err
		}
		return c.types.literalType(kind.Literal, depth)
	case *effectusv1.Expression_FactPath:
		if err := c.text("predicate fact_path", kind.FactPath, true); err != nil {
			return nil, err
		}
		typeName, ok := c.environment.Facts[kind.FactPath]
		if !ok {
			return nil, fmt.Errorf("unknown fact %q", kind.FactPath)
		}
		return c.types.parse(typeName, false)
	case *effectusv1.Expression_Unary:
		if kind.Unary == nil {
			return nil, fmt.Errorf("unary expression is nil")
		}
		operand, err := c.checkExpression(kind.Unary.Operand, depth+1, nodes)
		if err != nil {
			return nil, err
		}
		switch kind.Unary.Operator {
		case effectusv1.UnaryOperator_UNARY_OPERATOR_NOT:
			if operand.kind != typeBool {
				return nil, fmt.Errorf("not operand must be bool")
			}
			return &typeRef{kind: typeBool}, nil
		case effectusv1.UnaryOperator_UNARY_OPERATOR_NEGATE:
			if !isNumeric(operand) {
				return nil, fmt.Errorf("negate operand must be numeric")
			}
			return operand, nil
		default:
			return nil, fmt.Errorf("invalid unary operator %d", kind.Unary.Operator)
		}
	case *effectusv1.Expression_Binary:
		return c.checkBinary(kind.Binary, depth, nodes)
	case *effectusv1.Expression_Call:
		return c.checkCall(kind.Call, depth, nodes)
	default:
		return nil, fmt.Errorf("expression kind is not set")
	}
}

func (c *artifactChecker) checkBinary(binary *effectusv1.BinaryExpression, depth int, nodes *int) (*typeRef, error) {
	if binary == nil {
		return nil, fmt.Errorf("binary expression is nil")
	}
	left, err := c.checkExpression(binary.Left, depth+1, nodes)
	if err != nil {
		return nil, err
	}
	right, err := c.checkExpression(binary.Right, depth+1, nodes)
	if err != nil {
		return nil, err
	}
	compatible := c.types.assignable(left, right) || c.types.assignable(right, left)
	switch binary.Operator {
	case effectusv1.BinaryOperator_BINARY_OPERATOR_EQUAL, effectusv1.BinaryOperator_BINARY_OPERATOR_NOT_EQUAL:
		if !compatible {
			return nil, fmt.Errorf("equality operands are incompatible")
		}
		return &typeRef{kind: typeBool}, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER,
		effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER_EQUAL,
		effectusv1.BinaryOperator_BINARY_OPERATOR_LESS,
		effectusv1.BinaryOperator_BINARY_OPERATOR_LESS_EQUAL:
		if !(compatible && ((isNumeric(left) && isNumeric(right)) || (left.kind == typeString && right.kind == typeString))) {
			return nil, fmt.Errorf("comparison operands must be compatible numbers or strings")
		}
		return &typeRef{kind: typeBool}, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_AND, effectusv1.BinaryOperator_BINARY_OPERATOR_OR:
		if left.kind != typeBool || right.kind != typeBool {
			return nil, fmt.Errorf("logical operands must be bool")
		}
		return &typeRef{kind: typeBool}, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_IN:
		resolved, resolveErr := c.types.resolve(right, make(map[string]struct{}))
		if resolveErr != nil || resolved.kind != typeList || !c.types.assignable(left, resolved.element) {
			return nil, fmt.Errorf("in requires a value and a compatible list")
		}
		return &typeRef{kind: typeBool}, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_CONTAINS:
		resolved, resolveErr := c.types.resolve(left, make(map[string]struct{}))
		if resolveErr != nil {
			return nil, resolveErr
		}
		if (resolved.kind == typeString && right.kind == typeString) || (resolved.kind == typeList && c.types.assignable(right, resolved.element)) {
			return &typeRef{kind: typeBool}, nil
		}
		return nil, fmt.Errorf("contains requires string/string or list/element operands")
	case effectusv1.BinaryOperator_BINARY_OPERATOR_ADD:
		if left.kind == typeString && right.kind == typeString {
			return &typeRef{kind: typeString}, nil
		}
		fallthrough
	case effectusv1.BinaryOperator_BINARY_OPERATOR_SUBTRACT,
		effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY,
		effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE,
		effectusv1.BinaryOperator_BINARY_OPERATOR_MODULO:
		if !isNumeric(left) || !isNumeric(right) {
			return nil, fmt.Errorf("arithmetic operands must be numeric")
		}
		if binary.Operator == effectusv1.BinaryOperator_BINARY_OPERATOR_MODULO && (left.kind != typeInt || right.kind != typeInt) {
			return nil, fmt.Errorf("modulo operands must be int")
		}
		if left.kind == typeFloat || right.kind == typeFloat || binary.Operator == effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE {
			return &typeRef{kind: typeFloat}, nil
		}
		return &typeRef{kind: typeInt}, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_MATCHES,
		effectusv1.BinaryOperator_BINARY_OPERATOR_STARTS_WITH,
		effectusv1.BinaryOperator_BINARY_OPERATOR_ENDS_WITH:
		if left.kind != typeString || right.kind != typeString {
			return nil, fmt.Errorf("string operator operands must be string")
		}
		return &typeRef{kind: typeBool}, nil
	default:
		return nil, fmt.Errorf("invalid binary operator %d", binary.Operator)
	}
}

func (c *artifactChecker) checkCall(call *effectusv1.FunctionCall, depth int, nodes *int) (*typeRef, error) {
	if call == nil {
		return nil, fmt.Errorf("function call is nil")
	}
	if err := c.text("predicate function", call.Function, true); err != nil {
		return nil, err
	}
	contract, ok := c.environment.Functions[call.Function]
	if !ok {
		return nil, fmt.Errorf("unknown function %q", call.Function)
	}
	if !contract.Pure || !contract.Total {
		return nil, fmt.Errorf("function %q is not declared pure and total", call.Function)
	}
	if len(call.Arguments) != len(contract.ArgumentTypes) {
		return nil, fmt.Errorf("function %q has %d arguments, want %d", call.Function, len(call.Arguments), len(contract.ArgumentTypes))
	}
	for index, argument := range call.Arguments {
		actual, err := c.checkExpression(argument, depth+1, nodes)
		if err != nil {
			return nil, err
		}
		expected, err := c.types.parse(contract.ArgumentTypes[index], false)
		if err != nil || !c.types.assignable(actual, expected) {
			return nil, fmt.Errorf("function %q argument %d is incompatible with %s", call.Function, index, contract.ArgumentTypes[index])
		}
	}
	return c.types.parse(contract.ReturnType, false)
}

func (c *artifactChecker) checkLiteralStructure(literal *effectusv1.Literal, depth int) error {
	if literal == nil {
		return fmt.Errorf("literal is nil")
	}
	if depth > c.limits.MaxDepth {
		return limitError("literal depth", depth, c.limits.MaxDepth)
	}
	c.literalNodes++
	if c.literalNodes > c.limits.MaxLiteralNodes {
		return limitError("literal nodes", c.literalNodes, c.limits.MaxLiteralNodes)
	}
	switch kind := literal.Kind.(type) {
	case *effectusv1.Literal_Null:
		if kind.Null != effectusv1.NullValue_NULL_VALUE_NULL {
			return fmt.Errorf("null enum is unspecified")
		}
	case *effectusv1.Literal_BoolValue, *effectusv1.Literal_IntValue:
	case *effectusv1.Literal_DoubleValue:
		if math.IsNaN(kind.DoubleValue) || math.IsInf(kind.DoubleValue, 0) {
			return fmt.Errorf("double must be finite")
		}
	case *effectusv1.Literal_StringValue:
		if err := c.text("literal string", kind.StringValue, false); err != nil {
			return err
		}
	case *effectusv1.Literal_BytesValue:
		if len(kind.BytesValue) > c.limits.MaxBytesValue {
			return limitError("literal bytes", len(kind.BytesValue), c.limits.MaxBytesValue)
		}
	case *effectusv1.Literal_ListValue:
		if kind.ListValue == nil {
			return fmt.Errorf("list is nil")
		}
		if len(kind.ListValue.Values) > c.limits.MaxCollectionItems {
			return limitError("list items", len(kind.ListValue.Values), c.limits.MaxCollectionItems)
		}
		for _, value := range kind.ListValue.Values {
			if err := c.checkLiteralStructure(value, depth+1); err != nil {
				return err
			}
		}
	case *effectusv1.Literal_ObjectValue:
		if kind.ObjectValue == nil {
			return fmt.Errorf("object is nil")
		}
		if len(kind.ObjectValue.Fields) > c.limits.MaxObjectFields {
			return limitError("object fields", len(kind.ObjectValue.Fields), c.limits.MaxObjectFields)
		}
		previous := ""
		for index, field := range kind.ObjectValue.Fields {
			if field == nil {
				return fmt.Errorf("object field %d is nil", index)
			}
			if err := c.text("object field name", field.Name, true); err != nil {
				return err
			}
			if index > 0 && field.Name <= previous {
				return fmt.Errorf("object fields are not uniquely ordered by name")
			}
			previous = field.Name
			if err := c.checkLiteralStructure(field.Value, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("literal kind is not set")
	}
	return nil
}

func (c *artifactChecker) text(location, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return invalid("%s is empty", location)
	}
	if required && value != strings.TrimSpace(value) {
		return invalid("%s has leading or trailing whitespace", location)
	}
	if !utf8.ValidString(value) {
		return invalid("%s is not valid UTF-8", location)
	}
	if len(value) > c.limits.MaxStringBytes {
		return limitError(location+" bytes", len(value), c.limits.MaxStringBytes)
	}
	c.stringBytes += len(value)
	if c.stringBytes > c.limits.MaxTotalStringBytes {
		return limitError("total string bytes", c.stringBytes, c.limits.MaxTotalStringBytes)
	}
	return nil
}

func checkDigest(location, digest string) error {
	if len(digest) != sha256.Size*2 {
		return invalid("%s must be a lowercase SHA-256 digest", location)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return invalid("%s must be a lowercase SHA-256 digest", location)
	}
	return nil
}

func rejectUnknownFields(message protoreflect.Message) error {
	if len(message.GetUnknown()) != 0 {
		return invalid("protobuf contains unknown fields in %s", message.Descriptor().FullName())
	}
	var nestedErr error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() == protoreflect.MessageKind {
				value.Map().Range(func(_ protoreflect.MapKey, item protoreflect.Value) bool {
					nestedErr = rejectUnknownFields(item.Message())
					return nestedErr == nil
				})
			}
			return nestedErr == nil
		}
		if field.IsList() && field.Kind() == protoreflect.MessageKind {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if nestedErr = rejectUnknownFields(list.Get(i).Message()); nestedErr != nil {
					return false
				}
			}
			return true
		}
		if field.Kind() == protoreflect.MessageKind {
			nestedErr = rejectUnknownFields(value.Message())
		}
		return nestedErr == nil
	})
	return nestedErr
}

func isNumeric(ref *typeRef) bool {
	return ref != nil && (ref.kind == typeInt || ref.kind == typeFloat)
}

func invalid(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidArtifact, fmt.Sprintf(format, args...))
}

func contextualInvalid(location string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrInvalidArtifact, location, err)
}

func limitError(name string, actual, limit int) error {
	return fmt.Errorf("%w: %s is %d, limit is %d", ErrLimitExceeded, name, actual, limit)
}

// CanonicalPlanOrder sorts a mutable artifact into the only accepted plan
// order. Callers must still pass the result to Check.
func CanonicalPlanOrder(plans []*effectusv1.Plan) {
	sort.SliceStable(plans, func(i, j int) bool { return comparePlans(plans[i], plans[j]) < 0 })
}
