package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/ir"
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

func TestCompileCommandEmitsCheckedIR(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "flow.effx")
	verbs := filepath.Join(dir, "verbs.json")
	output := filepath.Join(dir, "rules.effir")
	require.NoError(t, os.WriteFile(source, []byte(`flow "ok" priority 1 { when { true } steps { record(receipt: "ok") } }`), 0o600))
	require.NoError(t, os.WriteFile(verbs, []byte(`{"record":{"arg_types":{"receipt":"string"},"required_args":["receipt"],"return_type":"void","capability":2}}`), 0o600))

	commands = make(map[string]*Command)
	defineCommands()
	command := commands["compile"]
	require.NoError(t, command.FlagSet.Parse([]string{"--verbschema", verbs, "--output", output, source}))
	require.NoError(t, command.Run())

	registry, err := loadVerbRegistry([]string{verbs}, false)
	require.NoError(t, err)
	environment, err := compiler.BuildIREnvironment(types.NewTypeSystem(), registry)
	require.NoError(t, err)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	_, err = ir.Parse(data, environment, ir.Limits{})
	require.NoError(t, err)
}

func TestCheckedCommandParity(t *testing.T) {
	dir := t.TempDir()
	verbs := filepath.Join(dir, "verbs.json")
	require.NoError(t, os.WriteFile(verbs, []byte(`{"record":{"arg_types":{"receipt":"string"},"required_args":["receipt"],"return_type":"void","capability":2}}`), 0o600))
	for _, test := range []struct {
		name    string
		source  string
		wantErr bool
	}{
		{name: "valid", source: `flow "ok" priority 1 { when { true } steps { record(receipt: "ok") } }`},
		{name: "invalid contract", source: `flow "bad" priority 1 { when { true } steps { record(receipt: 42) } }`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(dir, test.name+".effx")
			require.NoError(t, os.WriteFile(source, []byte(test.source), 0o600))
			results := make(map[string]bool)
			for _, name := range []string{"check", "compile"} {
				commands = make(map[string]*Command)
				defineCommands()
				args := []string{"--verbschema", verbs}
				if name == "compile" {
					args = append(args, "--output", filepath.Join(dir, test.name+".effir"))
				}
				args = append(args, source)
				require.NoError(t, commands[name].FlagSet.Parse(args))
				results[name] = commands[name].Run() != nil
			}
			require.Equal(t, test.wantErr, results["check"])
			require.Equal(t, results["check"], results["compile"])
		})
	}
}

func TestFormatCheckIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unformatted.eff")
	original := []byte(`rule "x" priority 1 { when { true } then { } }`)
	require.NoError(t, os.WriteFile(path, original, 0o600))

	command := newFormatCommand()
	require.NoError(t, command.FlagSet.Parse([]string{"--check", path}))
	require.Error(t, command.Run())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestCheckFailsRequestedDeclarationLoad(t *testing.T) {
	testExplicitDeclarationFailure(t, "schema")
}

func TestCompileFailsRequestedDeclarationLoad(t *testing.T) {
	testExplicitDeclarationFailure(t, "verbschema")
}

func testExplicitDeclarationFailure(t *testing.T, flagName string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "valid.eff")
	require.NoError(t, os.WriteFile(source, []byte(`rule "ok" priority 1 { when { true } then {} }`), 0o600))
	missing := filepath.Join(dir, "missing.json")
	malformed := filepath.Join(dir, "malformed.json")
	require.NoError(t, os.WriteFile(malformed, []byte(`{"broken":`), 0o600))
	unreadable := filepath.Join(dir, "unreadable")
	require.NoError(t, os.Mkdir(unreadable, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(unreadable, "broken.json"), []byte(`{"broken":`), 0o600))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	for kind, declaration := range map[string]string{"missing": missing, "malformed": malformed, "unreadable": unreadable} {
		t.Run(kind, func(t *testing.T) {
			for _, name := range []string{"typecheck", "check", "compile"} {
				t.Run(name, func(t *testing.T) {
					commands = make(map[string]*Command)
					defineCommands()
					output := filepath.Join(dir, kind+"-"+name+".effir")
					args := []string{"--" + flagName, declaration}
					if name == "compile" {
						args = append(args, "--output", output)
					}
					args = append(args, source)
					require.NoError(t, commands[name].FlagSet.Parse(args))
					require.Error(t, commands[name].Run())
					if name == "compile" {
						_, err := os.Stat(output)
						require.ErrorIs(t, err, os.ErrNotExist)
					}
				})
			}
		})
	}
}

func TestDocumentedCommands(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "rules.eff")
	require.NoError(t, os.WriteFile(source, []byte(`rule "ok" priority 1 { when { true } then {} }`), 0o600))
	commands = make(map[string]*Command)
	defineCommands()
	require.Contains(t, commands, "migrate-workflows")
	require.IsIncreasing(t, sortedCommandNames())

	parse := commands["parse"]
	require.NoError(t, parse.FlagSet.Parse([]string{"--verbose", source}))
	require.NoError(t, parse.Run())
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
