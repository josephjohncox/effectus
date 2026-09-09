package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/internal/demo/orderreview"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type clientOptions struct {
	address, token, ruleset, version, namespace, key, orderID, caFile string
	allowInsecure                                                     bool
	timeout                                                           time.Duration
}

type clientResult struct {
	ExecutionID      string `json:"execution_id"`
	State            string `json:"state"`
	GenerationDigest string `json:"generation_digest"`
	DurablyAccepted  bool   `json:"durably_accepted"`
	Completed        bool   `json:"completed"`
	Success          bool   `json:"success"`
}

func clientCredentials(options clientOptions) (credentials.TransportCredentials, error) {
	if options.allowInsecure {
		if options.caFile != "" {
			return nil, fmt.Errorf("choose TLS with --ca-file or explicit --allow-insecure, not both")
		}
		return insecure.NewCredentials(), nil
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if options.caFile != "" {
		data, err := os.ReadFile(options.caFile)
		if err != nil {
			return nil, fmt.Errorf("read trusted CA file: %w", err)
		}
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("CA file contains no certificates")
		}
	}
	return credentials.NewTLS(config), nil
}

func executeClient(ctx context.Context, options clientOptions) (result clientResult, err error) {
	options.token = strings.TrimSpace(options.token)
	if options.token == "" {
		return result, fmt.Errorf("set EFFECTUS_API_TOKEN or --token")
	}
	if options.timeout <= 0 || options.timeout > 5*time.Minute {
		return result, fmt.Errorf("timeout must be positive and at most 5m")
	}
	transport, err := clientCredentials(options)
	if err != nil {
		return result, err
	}
	scenario, err := orderreview.CanonicalScenario()
	if err != nil {
		return result, err
	}
	facts := scenario.Facts()
	if options.orderID != "" {
		facts["order"].(map[string]any)["id"] = options.orderID
	}
	typedFacts, err := structpb.NewStruct(facts)
	if err != nil {
		return result, err
	}
	if options.namespace == "" {
		options.namespace = scenario.Request.Namespace
	}
	if options.key == "" {
		options.key = scenario.IdempotencyKey
	}
	connection, err := grpc.NewClient(options.address, grpc.WithTransportCredentials(transport))
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, connection.Close()) }()
	ctx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+options.token))
	response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{
		RulesetName: options.ruleset, Version: options.version, Namespace: options.namespace, IdempotencyKey: options.key,
		TypedFacts: typedFacts, WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL,
	})
	if err != nil {
		// Do not print remote status details or wrapped destination errors.
		return result, fmt.Errorf("RPC failed: %s", status.Code(err))
	}
	return clientResult{
		ExecutionID: response.ExecutionId, State: response.State.String(), GenerationDigest: response.GenerationDigest,
		DurablyAccepted: response.DurablyAccepted, Completed: response.Completed, Success: response.Success,
	}, nil
}

func runClientCLI(args []string, stdout, stderr io.Writer) int {
	options := clientOptions{}
	flags := flag.NewFlagSet("grpc_execution", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.address, "address", "127.0.0.1:9091", "gRPC service address")
	flags.StringVar(&options.token, "token", "", "Bearer token. Prefer EFFECTUS_API_TOKEN to avoid process arguments")
	flags.StringVar(&options.ruleset, "ruleset", "order-review", "Ruleset name")
	flags.StringVar(&options.version, "version", "1.0.0", "Ruleset version")
	flags.StringVar(&options.namespace, "namespace", "", "Namespace. Default: shared scenario")
	flags.StringVar(&options.key, "idempotency-key", "", "Logical request key. Default: shared scenario")
	flags.StringVar(&options.orderID, "order-id", "", "Override the scenario order ID to exercise identity conflicts")
	flags.StringVar(&options.caFile, "ca-file", "", "Trusted PEM CA file. Default: system roots")
	flags.BoolVar(&options.allowInsecure, "allow-insecure", false, "Explicit local-development plaintext override")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Second, "Positive client deadline")
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
	// Do not put the environment secret in a flag default: help prints defaults.
	if options.token == "" {
		options.token = os.Getenv("EFFECTUS_API_TOKEN")
	}
	result, err := executeClient(context.Background(), options)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(result)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(runClientCLI(os.Args[1:], os.Stdout, os.Stderr)) }
