package main

import (
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/schema/types"
	"github.com/stretchr/testify/require"
)

func TestFactsTypeReturnsRegisteredType(t *testing.T) {
	typeSystem := types.NewTypeSystem()
	expected := types.NewStringType()
	typeSystem.RegisterFactType("customer.id", expected)
	facts := &testFacts{schema: &testSchema{typeSystem: typeSystem}}

	require.Equal(t, expected, facts.Type("customer.id"))
	require.Nil(t, facts.Type("customer.missing"))
}

func TestValidationCommandsReturnErrorsForInvalidFiles(t *testing.T) {
	for _, commandName := range []string{"typecheck", "parse", "capabilities"} {
		t.Run(commandName, func(t *testing.T) {
			commands = make(map[string]*Command)
			defineCommands()

			command := commands[commandName]
			require.NotNil(t, command)
			missingFile := filepath.Join(t.TempDir(), "missing.eff")
			require.NoError(t, command.FlagSet.Parse([]string{missingFile}))
			require.Error(t, command.Run())
		})
	}
}
