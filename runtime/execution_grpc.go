package runtime

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type EngineExecutionServiceOptions struct {
	RulesetName string
	Version     string
}

// EngineExecutionService is the sole generated inbound gRPC facade. It has no
// mutable method registry and admits work only through Engine.Execute.
type EngineExecutionService struct {
	effectusv1.UnimplementedRulesetExecutionServiceServer
	Engine  *Engine
	options EngineExecutionServiceOptions
}

func RegisterEngineExecutionServiceWithOptions(registrar grpc.ServiceRegistrar, engine *Engine, options EngineExecutionServiceOptions) error {
	if registrar == nil || engine == nil {
		return fmt.Errorf("gRPC registrar and runtime engine are required")
	}
	if strings.TrimSpace(options.RulesetName) == "" || strings.TrimSpace(options.Version) == "" {
		return fmt.Errorf("gRPC ruleset name and version are required")
	}
	effectusv1.RegisterRulesetExecutionServiceServer(registrar, &EngineExecutionService{Engine: engine, options: options})
	return nil
}

func (service *EngineExecutionService) ExecuteRuleset(ctx context.Context, request *effectusv1.ExecutionRequest) (*effectusv1.ExecutionResponse, error) {
	if service == nil || service.Engine == nil {
		return nil, status.Error(codes.Unavailable, "execution engine is unavailable")
	}
	if request == nil || ctx == nil {
		return nil, status.Error(codes.InvalidArgument, "execution request and context are required")
	}
	if request.RulesetName != service.options.RulesetName || request.Version != service.options.Version {
		return nil, status.Error(codes.NotFound, "requested ruleset version is unavailable")
	}
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	typedFacts := request.TypedFacts
	if typedFacts == nil && request.Facts != nil {
		legacy := new(structpb.Struct)
		if err := request.Facts.UnmarshalTo(legacy); err != nil {
			return nil, status.Error(codes.InvalidArgument, "legacy facts must contain google.protobuf.Struct")
		}
		typedFacts = legacy
	}
	if typedFacts == nil {
		return nil, status.Error(codes.InvalidArgument, "typed_facts are required")
	}
	facts := typedFacts.AsMap()
	if _, err := canonicalJSONValue(facts); err != nil {
		return nil, status.Error(codes.InvalidArgument, "facts contain invalid values")
	}
	if request.SchemaValidation != nil {
		return nil, status.Error(codes.InvalidArgument, "schema_validation is not supported")
	}
	if err := validateExecutionOptions(request.Options); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	waitMode, err := grpcWaitMode(request.WaitMode)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid wait_mode")
	}

	if _, err := service.activeGenerationDigest(); err != nil {
		return nil, status.Error(codes.Unavailable, "execution generation is unavailable")
	}
	// The engine checks an explicit digest against the pinned replay artifact
	// or, for a new identity, against the active generation.
	generationDigest := strings.TrimSpace(request.GenerationDigest)
	namespace := strings.TrimSpace(request.Namespace)
	if namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}
	executionID := schema.StableExecutionID(namespace, idempotencyKey, request.RulesetName, request.Version)
	admissionID := schema.StableAdmissionID(namespace, idempotencyKey, request.RulesetName, request.Version)
	callContext := ctx
	if request.Options != nil && request.Options.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		callContext, cancel = context.WithTimeout(ctx, time.Duration(request.Options.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	started := time.Now().UTC()
	result, executeErr := service.Engine.Execute(callContext, ExecuteRequest{Admission: &Admission{
		ExecutionID: executionID, AdmissionID: admissionID, TenantNamespace: namespace,
		Ruleset: request.RulesetName, Version: request.Version, Facts: facts,
		ExpectedGenerationDigest: generationDigest,
	}, WaitMode: waitMode})
	var terminal *TerminalExecutionError
	if executeErr != nil && !errors.As(executeErr, &terminal) {
		return nil, grpcEngineError(executeErr)
	}
	ended := time.Now().UTC()
	response := &effectusv1.ExecutionResponse{
		Success: result.Completed, ExecutionId: result.ExecutionID,
		DurablyAccepted: result.DurablyAccepted, Completed: result.Completed,
		State: grpcExecutionState(result.State), GenerationDigest: result.GenerationDigest,
		StartTime: timestamppb.New(started), EndTime: timestamppb.New(ended),
		Metadata: map[string]string{"state": result.State, "generation_digest": result.GenerationDigest, "ruleset": request.RulesetName, "version": request.Version},
	}
	if terminal != nil {
		disposition := status.Convert(grpcEngineError(executeErr))
		response.Errors = []string{disposition.Message()}
		withDetails, err := disposition.WithDetails(response)
		if err != nil {
			return nil, status.Error(codes.Internal, "encode execution disposition")
		}
		return nil, withDetails.Err()
	}
	return response, nil
}

func (service *EngineExecutionService) activeGenerationDigest() (string, error) {
	if service == nil || service.Engine == nil || service.Engine.Generation() == nil || service.Engine.Generation().Closed() {
		return "", fmt.Errorf("no checked generation")
	}
	return service.Engine.Generation().Digest(), nil
}

func validateExecutionOptions(options *effectusv1.ExecutionOptions) error {
	if options == nil {
		return nil
	}
	if options.DryRun {
		return fmt.Errorf("options.dry_run is not supported")
	}
	if options.MaxEffects != 0 {
		return fmt.Errorf("options.max_effects is not supported")
	}
	if options.EnableTracing {
		return fmt.Errorf("options.enable_tracing is not supported")
	}
	if len(options.CapabilityFilter) != 0 {
		return fmt.Errorf("options.capability_filter is not supported")
	}
	if options.MinSchemaVersion != "" {
		return fmt.Errorf("options.min_schema_version is not supported")
	}
	if options.MaxSchemaVersion != "" {
		return fmt.Errorf("options.max_schema_version is not supported")
	}
	if options.TimeoutSeconds < 0 {
		return fmt.Errorf("options.timeout_seconds must not be negative")
	}
	return nil
}

func grpcWaitMode(mode effectusv1.ExecutionWaitMode) (WaitMode, error) {
	switch mode {
	case effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_UNSPECIFIED, effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL:
		return WaitTerminal, nil
	case effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED:
		return WaitAccepted, nil
	default:
		return "", ErrInvalidExecuteRequest
	}
}

func grpcEngineError(err error) error {
	var terminal *TerminalExecutionError
	if errors.As(err, &terminal) {
		message := "execution is blocked"
		if terminal.State == schema.ExecutionFailed {
			message = "execution failed"
		}
		return status.Error(codes.FailedPrecondition, message)
	}
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "execution deadline exceeded")
	case errors.Is(err, ErrIdentityConflict):
		return status.Error(codes.AlreadyExists, "idempotency identity conflicts with an existing request")
	case errors.Is(err, ErrGenerationMismatch):
		return status.Error(codes.FailedPrecondition, "requested generation is not active")
	case errors.Is(err, ErrInvalidExecuteRequest):
		return status.Error(codes.InvalidArgument, "invalid execution request")
	case errors.Is(err, ErrExecutionNotFound):
		return status.Error(codes.NotFound, "execution is not available")
	case errors.Is(err, ErrBlockedDependency), errors.Is(err, ErrExecutionBusy), errors.Is(err, ErrGRPCUnavailable),
		errors.Is(err, sql.ErrConnDone), errors.Is(err, driver.ErrBadConn), errors.As(err, &network):
		return status.Error(codes.Unavailable, "execution dependency is unavailable")
	case errors.Is(err, schema.ErrOptimisticConflict):
		return status.Error(codes.Aborted, "execution state changed; retry the same identity")
	default:
		return status.Error(codes.Internal, "execution failed")
	}
}
