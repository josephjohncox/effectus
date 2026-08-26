package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
