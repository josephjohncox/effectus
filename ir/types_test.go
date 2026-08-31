package ir_test

import (
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCheckValidatesClosedStructuralLiterals(t *testing.T) {
	environment := ir.Environment{
		Verbs: map[string]ir.VerbContract{
			"store": {
				Arguments: map[string]string{
					"labels":  "map<string>",
					"profile": "Profile",
					"roles":   "list<string>",
				},
				ResultType: "void",
			},
		},
		Types: map[string]ir.TypeDefinition{
			"Profile": {
				Kind:           ir.TypeKindObject,
				Fields:         map[string]string{"age": "int", "name": "string", "note": "string"},
				RequiredFields: []string{"age", "name"},
			},
		},
	}
	artifact := oneStepArtifact(t, environment, []*effectusv1.Argument{
		{
			Name: "labels",
			Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_ObjectValue{
				ObjectValue: &effectusv1.LiteralObject{Fields: []*effectusv1.LiteralField{{Name: "team", Value: stringLiteral("risk")}}},
			}}}},
		},
		{
			Name: "profile",
			Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_ObjectValue{
				ObjectValue: &effectusv1.LiteralObject{Fields: []*effectusv1.LiteralField{
					{Name: "age", Value: &effectusv1.Literal{Kind: &effectusv1.Literal_IntValue{IntValue: 42}}},
					{Name: "name", Value: stringLiteral("Ada")},
				}},
			}}}},
		},
		{
			Name: "roles",
			Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_ListValue{
				ListValue: &effectusv1.LiteralList{},
			}}}},
		},
	})
	_, err := ir.Check(artifact, environment, ir.Limits{})
	require.NoError(t, err)

	t.Run("missing required field", func(t *testing.T) {
		copy := protoClone(artifact)
		profile := copy.Plans[0].Steps[0].Arguments[1].Value.GetLiteral().GetObjectValue()
		profile.Fields = profile.Fields[1:]
		_, err := ir.Check(copy, environment, ir.Limits{})
		require.ErrorIs(t, err, ir.ErrInvalidArtifact)
	})

	t.Run("unknown field", func(t *testing.T) {
		copy := protoClone(artifact)
		profile := copy.Plans[0].Steps[0].Arguments[1].Value.GetLiteral().GetObjectValue()
		profile.Fields = append(profile.Fields, &effectusv1.LiteralField{Name: "unknown", Value: stringLiteral("x")})
		_, err := ir.Check(copy, environment, ir.Limits{})
		require.ErrorIs(t, err, ir.ErrInvalidArtifact)
	})
}

func oneStepArtifact(t *testing.T, environment ir.Environment, arguments []*effectusv1.Argument) *effectusv1.RuleArtifact {
	t.Helper()
	environmentDigest, err := ir.EnvironmentDigest(environment)
	require.NoError(t, err)
	contractHash, err := ir.ContractHash(environment.Verbs["store"])
	require.NoError(t, err)
	return &effectusv1.RuleArtifact{
		FormatVersion:     ir.FormatVersion,
		EnvironmentDigest: environmentDigest,
		Compiler: &effectusv1.CompilerMetadata{
			Name: "effectusc", Version: "test", BuildDigest: digest("compiler"),
		},
		Plans: []*effectusv1.Plan{{
			Id:              "list/store",
			SourceDialect:   effectusv1.SourceDialect_SOURCE_DIALECT_LIST,
			ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST,
			Predicate: &effectusv1.Predicate{Expression: &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{
				Literal: boolLiteral(true),
			}}},
			Steps: []*effectusv1.Step{{
				Id: "list/store/store", Verb: "store", ContractHash: contractHash, Arguments: arguments,
			}},
		}},
	}
}

func protoClone(artifact *effectusv1.RuleArtifact) *effectusv1.RuleArtifact {
	return proto.Clone(artifact).(*effectusv1.RuleArtifact)
}
