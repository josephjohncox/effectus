package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/internal/schemasources"
	"github.com/effectus/effectus-go/lint"
	"github.com/effectus/effectus-go/schema/verb"
)

func newCheckCommand() *Command {
	checkCmd := &Command{
		Name:        "check",
		Description: "Parse, type-check, and lint rule files",
		FlagSet:     flag.NewFlagSet("check", flag.ExitOnError),
	}

	schemaFiles := checkCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	schemaSources := checkCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	verbSchemas := checkCmd.FlagSet.String("verbschema", "", "Comma-separated list of verb schema files to load")
	format := checkCmd.FlagSet.String("format", "text", "Output format: text or json")
	failOnWarn := checkCmd.FlagSet.Bool("fail-on-warn", false, "Return non-zero exit code when warnings are present")
	unsafeMode := checkCmd.FlagSet.String("unsafe", "warn", "Unsafe expression policy: warn, error, ignore")
	verbMode := checkCmd.FlagSet.String("verbs", "error", "Verb lint policy: error, warn, ignore")
	verbose := checkCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	checkCmd.Run = func() error {
		files := checkCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		mode, err := lint.ParseUnsafeMode(*unsafeMode)
		if err != nil {
			return err
		}
		verbPolicy, err := lint.ParseVerbMode(*verbMode)
		if err != nil {
			return err
		}

		registry, declarationErr := loadVerbRegistry(splitCommaList(*verbSchemas), *verbose)
		issues, hadWarn, hadError, err := runCheck(runCheckOptions{
			files:          files,
			schemaFiles:    *schemaFiles,
			schemaSources:  strings.TrimSpace(*schemaSources),
			registry:       registry,
			declarationErr: declarationErr,
			lintOptions: lint.LintOptions{
				UnsafeMode: mode,
				VerbMode:   verbPolicy,
			},
			verbose: *verbose,
		})
		if err != nil {
			return err
		}

		switch strings.ToLower(*format) {
		case "json":
			encoded, err := json.MarshalIndent(issues, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding issues: %w", err)
			}
			fmt.Println(string(encoded))
		case "text":
			if len(issues) > 0 {
				fmt.Println(formatIssuesText(issues))
			}
		default:
			return fmt.Errorf("unsupported format: %s", *format)
		}

		if hadError || (*failOnWarn && hadWarn) {
			return fmt.Errorf("check failed")
		}

		return nil
	}

	return checkCmd
}

type runCheckOptions struct {
	files          []string
	schemaFiles    string
	schemaSources  string
	registry       *verb.Registry
	declarationErr error
	lintOptions    lint.LintOptions
	verbose        bool
}

func runCheck(opts runCheckOptions) ([]lint.Issue, bool, bool, error) {
	if len(opts.files) == 0 {
		return nil, false, false, nil
	}

	_, typeSystem, schemaErr := createEmptyFacts(opts.schemaFiles, opts.verbose)
	var sourceErr error
	if strings.TrimSpace(opts.schemaSources) != "" {
		declarations, err := schemasources.LoadFromFile(opts.schemaSources)
		if err != nil {
			sourceErr = err
		} else if err := schemasources.Apply(context.Background(), typeSystem, declarations, opts.verbose); err != nil {
			sourceErr = err
		}
	}
	if err := errors.Join(opts.declarationErr, schemaErr, sourceErr); err != nil {
		return nil, false, false, err
	}
	environment, err := compiler.BuildIREnvironment(typeSystem, opts.registry)
	if err != nil {
		return nil, false, false, err
	}
	sources, err := compiler.LoadSources(opts.files)
	if err != nil {
		return nil, false, false, err
	}
	issues := make([]lint.Issue, 0)
	hadWarn := false
	hadError := false
	compileOptions := compiler.CompileOptions{InspectSource: func(path string, parsed *ast.File) {
		if opts.verbose {
			fmt.Printf("Checking %s...\n", path)
		}
		fileIssues := lint.LintFileWithOptions(parsed, path, opts.registry, opts.lintOptions)
		for _, issue := range fileIssues {
			if issue.Severity == "warning" {
				hadWarn = true
			}
			if issue.Severity == "error" {
				hadError = true
			}
		}
		issues = append(issues, fileIssues...)
	}}
	if _, err := compiler.CompileChecked(context.Background(), sources, environment, compileOptions); err != nil {
		issues = append(issues, issueFromError("", err))
		hadError = true
	}

	return issues, hadWarn, hadError, nil
}
