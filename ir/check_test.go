package ir_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func testEnvironment(t *testing.T) ir.Environment {
	t.Helper()
	return ir.Environment{
		Facts: map[string]string{
			"customer.active": "bool",
			"customer.email":  "string",
		},
		Verbs: map[string]ir.VerbContract{
			"lookup": {
				Arguments:  map[string]string{"email": "string"},
				ResultType: "string",
			},
			"send": {
				Arguments:    map[string]string{"message": "string", "tag": "string"},
				RequiredArgs: []string{"message"},
				ResultType:   "string",
			},
		},
		Functions: map[string]ir.FunctionContract{
			"isNonEmpty": {ArgumentTypes: []string{"string"}, ReturnType: "bool", Pure: true, Total: true},
			"clock":      {ReturnType: "string", Pure: true, Total: false},
		},
	}
}

func validArtifact(t *testing.T, environment ir.Environment) *effectusv1.RuleArtifact {
	t.Helper()
	environmentDigest, err := ir.EnvironmentDigest(environment)
	require.NoError(t, err)
	lookupHash, err := ir.ContractHash(environment.Verbs["lookup"])
	require.NoError(t, err)
	sendHash, err := ir.ContractHash(environment.Verbs["send"])
	require.NoError(t, err)
	build := sha256.Sum256([]byte("effectusc/test"))
	return &effectusv1.RuleArtifact{
		FormatVersion:     ir.FormatVersion,
		EnvironmentDigest: environmentDigest,
		Compiler: &effectusv1.CompilerMetadata{
			Name:        "effectusc",
			Version:     "test",
			BuildDigest: hex.EncodeToString(build[:]),
		},
		Plans: []*effectusv1.Plan{
			{
				Id:              "list/active-customer",
				SourceDialect:   effectusv1.SourceDialect_SOURCE_DIALECT_LIST,
				SourceOrder:     0,
				Priority:        10,
				ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST,
				Predicate: &effectusv1.Predicate{Expression: &effectusv1.Expression{Kind: &effectusv1.Expression_FactPath{
					FactPath: "customer.active",
				}}},
				Steps: []*effectusv1.Step{
					{
						Id:           "list/active-customer/lookup",
						Ordinal:      0,
						Verb:         "lookup",
						ContractHash: lookupHash,
						Arguments: []*effectusv1.Argument{{
							Name:  "email",
							Value: &effectusv1.Value{Kind: &effectusv1.Value_FactPath{FactPath: "customer.email"}},
						}},
						ResultSlot: uint32Pointer(0),
					},
					{
						Id:           "list/active-customer/send",
						Ordinal:      1,
						Verb:         "send",
						ContractHash: sendHash,
						Arguments: []*effectusv1.Argument{{
							Name:  "message",
							Value: &effectusv1.Value{Kind: &effectusv1.Value_ResultSlot{ResultSlot: 0}},
						}},
					},
				},
			},
			{
				Id:              "flow/literal-message",
				SourceDialect:   effectusv1.SourceDialect_SOURCE_DIALECT_FLOW,
				SourceOrder:     0,
				Priority:        100,
				ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING,
				Predicate: &effectusv1.Predicate{Expression: &effectusv1.Expression{Kind: &effectusv1.Expression_Call{Call: &effectusv1.FunctionCall{
					Function: "isNonEmpty",
					Arguments: []*effectusv1.Expression{{Kind: &effectusv1.Expression_FactPath{
						FactPath: "customer.email",
					}}},
				}}}},
				Steps: []*effectusv1.Step{{
					Id:           "flow/literal-message/send",
					Ordinal:      0,
					Verb:         "send",
					ContractHash: sendHash,
					Arguments: []*effectusv1.Argument{{
						Name:  "message",
						Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: stringLiteral("customer.email")}},
					}},
				}},
			},
		},
	}
}

func TestCheckedRoundTripPreservesValueKindsAndIsImmutable(t *testing.T) {
	environment := testEnvironment(t)
	artifact := validArtifact(t, environment)
	checked, err := ir.Check(artifact, environment, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, 2, checked.PlanCount())
	require.Equal(t, 3, checked.StepCount())

	artifact.Plans[0].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: stringLiteral("mutated")}}
	roundTrip, err := ir.Parse(checked.Marshal(), environment, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, checked.Digest(), roundTrip.Digest())
	require.Equal(t, checked.Marshal(), roundTrip.Marshal())

	clone := roundTrip.CloneArtifact()
	_, fact := clone.Plans[0].Steps[0].Arguments[0].Value.Kind.(*effectusv1.Value_FactPath)
	_, slot := clone.Plans[0].Steps[1].Arguments[0].Value.Kind.(*effectusv1.Value_ResultSlot)
	literal, literalOK := clone.Plans[1].Steps[0].Arguments[0].Value.Kind.(*effectusv1.Value_Literal)
	require.True(t, fact)
	require.True(t, slot)
	require.True(t, literalOK)
	require.Equal(t, "customer.email", literal.Literal.GetStringValue())
}

func TestCheckAcceptsOmittedOptionalArgument(t *testing.T) {
	environment := testEnvironment(t)
	_, err := ir.Check(validArtifact(t, environment), environment, ir.Limits{})
	require.NoError(t, err)
}

func TestCheckFailsClosedOnMalformedArtifact(t *testing.T) {
	tests := map[string]func(*effectusv1.RuleArtifact, ir.Environment){
		"wrong format": func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.FormatVersion++ },
		"plan order": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0], a.Plans[1] = a.Plans[1], a.Plans[0]
		},
		"duplicate plan identity": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[1].Id = a.Plans[0].Id
		},
		"step ordinal": func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.Plans[0].Steps[1].Ordinal = 8 },
		"duplicate step identity": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0].Steps[1].Id = a.Plans[0].Steps[0].Id
		},
		"forward slot": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_ResultSlot{ResultSlot: 0}}
		},
		"sparse slot":      func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.Plans[0].Steps[0].ResultSlot = uint32Pointer(1) },
		"contract changed": func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.Plans[0].Steps[0].ContractHash = digest("other") },
		"unknown verb":     func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.Plans[0].Steps[0].Verb = "missing" },
		"unknown fact": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_FactPath{FactPath: "missing"}}
		},
		"missing required argument": func(a *effectusv1.RuleArtifact, _ ir.Environment) { a.Plans[0].Steps[0].Arguments = nil },
		"duplicate argument": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0].Steps[1].Arguments = append(a.Plans[0].Steps[1].Arguments, proto.Clone(a.Plans[0].Steps[1].Arguments[0]).(*effectusv1.Argument))
		},
		"argument type mismatch": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[1].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: boolLiteral(true)}}
		},
		"non boolean predicate": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[0].Predicate.Expression = &effectusv1.Expression{Kind: &effectusv1.Expression_FactPath{FactPath: "customer.email"}}
		},
		"non total function": func(a *effectusv1.RuleArtifact, _ ir.Environment) {
			a.Plans[1].Predicate.Expression = &effectusv1.Expression{Kind: &effectusv1.Expression_Call{Call: &effectusv1.FunctionCall{Function: "clock"}}}
		},
		"environment mismatch": func(_ *effectusv1.RuleArtifact, e ir.Environment) { e.Facts["new"] = "string" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			environment := testEnvironment(t)
			artifact := validArtifact(t, environment)
			mutate(artifact, environment)
			_, err := ir.Check(artifact, environment, ir.Limits{})
			require.Error(t, err)
			require.True(t, errors.Is(err, ir.ErrInvalidArtifact) || errors.Is(err, ir.ErrLimitExceeded), err)
		})
	}
}

func TestCheckRejectsUnknownAndOpenTypes(t *testing.T) {
	for _, typeName := range []string{"missing.Named", "any", "unknown", "list<any>"} {
		t.Run(typeName, func(t *testing.T) {
			environment := testEnvironment(t)
			contract := environment.Verbs["send"]
			contract.Arguments["message"] = typeName
			environment.Verbs["send"] = contract
			artifact := validArtifactWithoutDigest(t, environment)
			_, err := ir.Check(artifact, environment, ir.Limits{})
			require.ErrorIs(t, err, ir.ErrInvalidArtifact)
		})
	}
}

func TestParseRejectsUnknownProtobufFields(t *testing.T) {
	environment := testEnvironment(t)
	artifact := validArtifact(t, environment)
	wire, err := proto.Marshal(artifact)
	require.NoError(t, err)
	wire = protowire.AppendTag(wire, 1000, protowire.VarintType)
	wire = protowire.AppendVarint(wire, 1)
	_, err = ir.Parse(wire, environment, ir.Limits{})
	require.ErrorIs(t, err, ir.ErrInvalidArtifact)
}

func TestCheckEnforcesLimitPlusOne(t *testing.T) {
	environment := testEnvironment(t)
	tests := map[string]struct {
		limits ir.Limits
		mutate func(*effectusv1.RuleArtifact)
	}{
		"plans":          {limits: ir.Limits{MaxPlans: 1}},
		"steps":          {limits: ir.Limits{MaxSteps: 2}},
		"steps per plan": {limits: ir.Limits{MaxStepsPerPlan: 1}},
		"arguments": {
			limits: ir.Limits{MaxArgumentsPerStep: 1},
			mutate: func(a *effectusv1.RuleArtifact) {
				a.Plans[0].Steps[1].Arguments = append(a.Plans[0].Steps[1].Arguments, &effectusv1.Argument{
					Name: "tag", Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: stringLiteral("tag")}},
				})
			},
		},
		"predicate nodes": {limits: ir.Limits{MaxPredicateNodes: 1}},
		"string bytes":    {limits: ir.Limits{MaxStringBytes: 4}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			artifact := validArtifact(t, environment)
			if test.mutate != nil {
				test.mutate(artifact)
			}
			_, err := ir.Check(artifact, environment, test.limits)
			require.ErrorIs(t, err, ir.ErrLimitExceeded)
		})
	}
}

func TestEnvironmentDigestAndContractHashAreDeterministic(t *testing.T) {
	left := testEnvironment(t)
	right := testEnvironment(t)
	leftDigest, err := ir.EnvironmentDigest(left)
	require.NoError(t, err)
	rightDigest, err := ir.EnvironmentDigest(right)
	require.NoError(t, err)
	require.Equal(t, leftDigest, rightDigest)

	implicitRequired := ir.VerbContract{Arguments: map[string]string{"b": "string", "a": "int"}, ResultType: "void"}
	explicitRequired := ir.VerbContract{Arguments: map[string]string{"a": "int", "b": "string"}, RequiredArgs: []string{"b", "a"}, ResultType: "void"}
	implicitHash, err := ir.ContractHash(implicitRequired)
	require.NoError(t, err)
	explicitHash, err := ir.ContractHash(explicitRequired)
	require.NoError(t, err)
	require.Equal(t, implicitHash, explicitHash)
}

func validArtifactWithoutDigest(t *testing.T, environment ir.Environment) *effectusv1.RuleArtifact {
	t.Helper()
	// Build against a valid environment first, then replace provenance. This
	// helper is used for environments that the strict checker must reject.
	base := testEnvironment(t)
	artifact := validArtifact(t, base)
	if environmentDigest, err := ir.EnvironmentDigest(environment); err == nil {
		artifact.EnvironmentDigest = environmentDigest
	}
	return artifact
}

func stringLiteral(value string) *effectusv1.Literal {
	return &effectusv1.Literal{Kind: &effectusv1.Literal_StringValue{StringValue: value}}
}

func boolLiteral(value bool) *effectusv1.Literal {
	return &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: value}}
}

func uint32Pointer(value uint32) *uint32 { return &value }

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
