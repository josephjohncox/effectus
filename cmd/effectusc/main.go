// Command effectusc validates source bundles and emits checked IR.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
)

type command struct {
	name, description string
	flags             *flag.FlagSet
	run               func() error
}

func main() { os.Exit(runCLI(os.Args[1:])) }

func runCLI(arguments []string) int {
	commands := defineCommands()
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h" || arguments[0] == "help") {
		usage(commands)
		return 0
	}
	if len(arguments) == 0 {
		usage(commands)
		return 2
	}
	cmd, ok := commands[arguments[0]]
	if !ok {
		usage(commands)
		return 2
	}
	if err := cmd.flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := cmd.run(); err != nil {
		fmt.Fprintln(os.Stderr, "effectusc:", err)
		return 1
	}
	return 0
}
func usage(commands map[string]command) {
	fmt.Fprintln(os.Stderr, "Usage: effectusc <command> [options]")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "  %s\t%s\n", name, commands[name].description)
	}
}
func defineCommands() map[string]command {
	return map[string]command{"check": checkedCommand(false), "compile": checkedCommand(true), "inspect": inspectCommand()}
}
func loadBundle(path string) (*bundle.SourceBundle, error) {
	if path == "" {
		return nil, fmt.Errorf("--bundle is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source bundle: %w", err)
	}
	value, err := bundle.Parse(data)
	if err != nil {
		return nil, err
	}
	return value, nil
}
func checkedCommand(write bool) command {
	name, description := "check", "compile a source bundle to checked IR without writing output"
	if write {
		name, description = "compile", "compile a source bundle to checked IR"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("bundle", "", "Path to effectus.source-bundle.v1 JSON")
	var output string
	if write {
		flags.StringVar(&output, "output", "", "Output checked IR protobuf (required for compile)")
	}
	return command{name: name, description: description, flags: flags, run: func() error {
		if flags.NArg() != 0 {
			return fmt.Errorf("%s does not accept positional source files; construct a SourceBundle first", name)
		}
		if write && output == "" {
			return fmt.Errorf("--output is required")
		}
		value, err := loadBundle(*source)
		if err != nil {
			return err
		}
		checked, err := compiler.CompileChecked(context.Background(), value, compiler.CompileOptions{})
		if err != nil {
			return err
		}
		if !write {
			fmt.Printf("checked bundle %s@%s: %d plans, %d steps, ir=%s\n", value.Name(), value.Version(), checked.PlanCount(), checked.StepCount(), checked.Digest())
			return nil
		}
		if err := writeCheckedArtifact(*source, output, checked.Marshal()); err != nil {
			return fmt.Errorf("write checked IR: %w", err)
		}
		return nil
	}}
}
func inspectCommand() command {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("bundle", "", "Path to effectus.source-bundle.v1 JSON")
	return command{name: "inspect", description: "show immutable source-bundle and checked-IR identity", flags: flags, run: func() error {
		if flags.NArg() != 0 {
			return fmt.Errorf("inspect does not accept positional source files")
		}
		value, err := loadBundle(*source)
		if err != nil {
			return err
		}
		checked, err := compiler.CompileChecked(context.Background(), value, compiler.CompileOptions{})
		if err != nil {
			return err
		}
		digest, err := value.Digest()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"name": value.Name(), "version": value.Version(), "source_digest": digest, "ir_digest": checked.Digest(), "plans": checked.PlanCount(), "steps": checked.StepCount()})
	}}
}
