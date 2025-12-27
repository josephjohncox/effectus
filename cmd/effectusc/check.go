package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/effectus/effectus-go/compiler"
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

		issues, hadWarn, hadError, err := runCheck(runCheckOptions{
			files:       files,
			schemaFiles: *schemaFiles,
			verbSchemas: splitCommaList(*verbSchemas),
			registry:    loadVerbRegistry(splitCommaList(*verbSchemas), *verbose),
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
	files       []string
	schemaFiles string
	verbSchemas []string
	registry    *verb.Registry
	lintOptions lint.LintOptions
	verbose     bool
}

func runCheck(opts runCheckOptions) ([]lint.Issue, bool, bool, error) {
	if len(opts.files) == 0 {
		return nil, false, false, nil
	}

	comp := compiler.NewCompiler()

	for _, file := range opts.verbSchemas {
		if opts.verbose {
			fmt.Printf("Loading verb schemas from %s...\n", file)
		}
		if err := comp.LoadVerbSpecs(file); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading verb schema file %s: %v\n", file, err)
		}
	}

	facts, typeSystem := createEmptyFacts(opts.schemaFiles, opts.verbose)
	compTS := comp.GetTypeSystem()
	compTS.MergeTypeSystem(typeSystem)

	issues := make([]lint.Issue, 0)
	hadWarn := false
	hadError := false

	for _, filename := range opts.files {
		if opts.verbose {
			fmt.Printf("Checking %s...\n", filename)
		}

		parsed, err := comp.ParseAndTypeCheck(filename, facts)
		if err != nil {
			hadError = true
			issues = append(issues, issueFromError(filename, err))
			continue
		}

		fileIssues := lint.LintFileWithOptions(parsed, filename, opts.registry, opts.lintOptions)
		for _, issue := range fileIssues {
			if issue.Severity == "warning" {
				hadWarn = true
			}
			if issue.Severity == "error" {
				hadError = true
			}
		}
		issues = append(issues, fileIssues...)
	}

	return issues, hadWarn, hadError, nil
}
