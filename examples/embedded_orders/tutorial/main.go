package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/josephjohncox/effectus/embedded"
	"github.com/josephjohncox/effectus/invocation"
)

type tutorialOptions struct {
	dialect    string
	diagnostic string
	total      float64
	risk       int
	bundleOnly bool
}

type tutorialSummary struct {
	Dialect          string      `json:"dialect"`
	Completed        bool        `json:"completed"`
	ExecutionID      string      `json:"execution_id"`
	ReplayID         string      `json:"replayed_execution_id"`
	GenerationDigest string      `json:"generation_digest"`
	Operations       []operation `json:"operations"`
}

func runTutorial(ctx context.Context, options tutorialOptions) (summary tutorialSummary, err error) {
	source, err := tutorialBundle(options.dialect, options.diagnostic)
	if err != nil {
		return summary, err
	}
	executor := &tutorialExecutor{}
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: tutorialResolverID,
		Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			return executor, nil, nil
		}),
	}})
	if err != nil {
		return summary, err
	}
	runner, err := embedded.Open(ctx, source, registry)
	if err != nil {
		return summary, err
	}
	defer func() { err = errors.Join(err, runner.Close()) }()
	request := embedded.Request{
		Namespace: "tutorial-" + options.dialect, IdempotencyKey: "order-200-created",
		Facts: map[string]any{"order": map[string]any{"id": "order-200", "total": options.total, "risk_score": options.risk}},
	}
	first, err := runner.Execute(ctx, request)
	if err != nil {
		return summary, err
	}
	replay, err := runner.Execute(ctx, request)
	if err != nil {
		return summary, err
	}
	return tutorialSummary{
		Dialect: options.dialect, Completed: first.Completed, ExecutionID: first.ExecutionID,
		ReplayID: replay.ExecutionID, GenerationDigest: runner.Engine().ActiveGenerationDigest(), Operations: executor.snapshot(),
	}, nil
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	var options tutorialOptions
	flags := flag.NewFlagSet("language_tutorial", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.dialect, "dialect", "eff", "Rule dialect: eff or effx")
	flags.StringVar(&options.diagnostic, "diagnostic", "", "Compile an invalid example: unknown-fact, type-mismatch, future-binding")
	flags.Float64Var(&options.total, "total", 2499, "Order total")
	flags.IntVar(&options.risk, "risk-score", 82, "Order risk score")
	flags.BoolVar(&options.bundleOnly, "bundle", false, "Print the source bundle instead of executing it")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}
	var err error
	if options.bundleOnly {
		source, buildErr := tutorialBundle(options.dialect, options.diagnostic)
		err = buildErr
		if err == nil {
			var data []byte
			data, err = source.Bytes()
			if err == nil {
				_, err = fmt.Fprintln(stdout, string(data))
			}
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var summary tutorialSummary
		summary, err = runTutorial(ctx, options)
		if err == nil {
			err = json.NewEncoder(stdout).Encode(summary)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr)) }
