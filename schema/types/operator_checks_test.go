package types

import (
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/stretchr/testify/require"
)

func TestTypeCheckArgValueWithBindings(t *testing.T) {
	typeSystem := NewTypeSystem()
	bindings := map[string]*Type{"result": NewStringType()}

	require.NoError(t, typeSystem.TypeCheckArgValueWithBindings(
		&ast.ArgValue{VarRef: "$result"}, NewStringType(), nil, bindings,
	))

	err := typeSystem.TypeCheckArgValueWithBindings(
		&ast.ArgValue{VarRef: "$result"}, NewIntType(), nil, bindings,
	)
	require.ErrorContains(t, err, "not compatible")

	err = typeSystem.TypeCheckArgValueWithBindings(
		&ast.ArgValue{VarRef: "$missing"}, NewStringType(), nil, bindings,
	)
	require.ErrorContains(t, err, "unknown variable reference")
}

func TestTypeCheckArgumentValueUsesCanonicalLiteralChecker(t *testing.T) {
	typeSystem := NewTypeSystem()
	require.NoError(t, typeSystem.TypeCheckArgumentValue(
		&ast.ArgValue{Literal: &ast.Literal{String: stringPointer("value")}},
		NewStringType(),
		nil,
	))
}

func stringPointer(value string) *string {
	return &value
}
