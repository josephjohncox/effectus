// Package v03_test verifies the v0.3 compatibility source surface as an external consumer.
package v03_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/compat/v03/embedded"
	"github.com/josephjohncox/effectus/compat/v03/executorhttp"
	"github.com/josephjohncox/effectus/compat/v03/invocation"
)

type v03DescriptorProvider struct{}

func (v03DescriptorProvider) InvocationResolverDescriptor() any {
	return map[string]string{"resolver": "v0.3"}
}

func TestExternalConsumerV03SurfaceCompiles(t *testing.T) {
	// These declarations are copied from the v0.3 contract. This package
	// intentionally imports only compat paths.
	var _ invocation.ResolverDescriptorProvider = v03DescriptorProvider{}
	var _ invocation.Executor = executorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		return invocation.Outcome{Class: invocation.OutcomeSuccess}
	})
	var _ embedded.HandlerFunc = func(context.Context, invocation.Request) invocation.Outcome {
		return embedded.Success(nil)
	}

	metadata := invocation.Context{Saga: invocation.Saga{Direction: invocation.DirectionForward}}
	var _ invocation.Direction = invocation.DirectionCompensation
	var _ invocation.OutcomeClass = invocation.OutcomeSuccess
	var _ invocation.FencingStatus = invocation.FencingNotRequested
	_ = invocation.FencingGrant{Authority: "authority", Resource: "resource", Token: 1}
	_ = invocation.Saga{SagaID: "saga", EffectID: "effect", Attempt: 1, Direction: invocation.DirectionForward, IdempotencyKey: "idempotency"}
	_ = invocation.Request{
		Metadata:     metadata,
		Verb:         "review-order",
		Arguments:    map[string]any{"order": "review"},
		ArgumentHash: "arguments",
		ContractHash: "contract",
	}
	_ = invocation.Outcome{Class: invocation.OutcomeSuccess}
	_ = invocation.HTTPExecutor{URL: "https://executor.example"}
	_ = []string{
		invocation.HeaderExecutionID, invocation.HeaderSagaID, invocation.HeaderEffectID,
		invocation.HeaderAttempt, invocation.HeaderDirection, invocation.HeaderArgumentHash,
		invocation.HeaderContractHash, invocation.HeaderFencingGrants, invocation.HeaderDeadline,
		invocation.HeaderOutcome, invocation.HeaderIdempotencyKey,
	}
	_ = []invocation.OutcomeClass{
		invocation.OutcomeSuccess, invocation.OutcomeRetryableKnownNotCommitted,
		invocation.OutcomePermanentFailure, invocation.OutcomeUnknown, invocation.OutcomeStaleFence,
	}
	_ = []invocation.FencingStatus{
		invocation.FencingNotRequested, invocation.FencingLocalLockOnly, invocation.FencingPropagated,
		invocation.FencingAcknowledged, invocation.FencingStaleRejected,
	}
	if err := invocation.ValidateOutcome(invocation.Outcome{Class: invocation.OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	_ = executorhttp.Request{
		Arguments:    map[string]any{"order": "review"},
		Metadata:     metadata,
		ArgumentHash: "arguments",
		ContractHash: "contract",
	}
	var _ executorhttp.HandlerFunc = func(context.Context, executorhttp.Request) executorhttp.Outcome {
		return executorhttp.Success(map[string]any{"ok": true})
	}
	_ = executorhttp.DirectionForward
	_ = executorhttp.DirectionCompensation
	_ = executorhttp.Retryable
	_ = executorhttp.Permanent
	_ = executorhttp.Unknown
	_ = executorhttp.StaleFence
	_, err := executorhttp.NewHandler(executorhttp.Options{}, func(context.Context, executorhttp.Request) executorhttp.Outcome {
		return executorhttp.Success(map[string]any{"ok": true})
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = invocation.NewHTTPExecutor(invocation.HTTPExecutor{URL: "https://executor.example"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyExternalConsumerWithPositionalRequestLiteralsCompiles(t *testing.T) {
	// Keep positional literals in an external-consumer fixture. go vet examines
	// this package too, so the fixture is compiled explicitly with vet disabled.
	// The compiler remains responsible for accepting the frozen v0.3 layout.
	command := exec.Command("go", "test", "-vet=off", "./testdata/legacyconsumer")
	command.Dir = filepath.Join(".")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compile legacy external consumer: %v\n%s", err, output)
	}
}

func TestExternalV03HandlerAdaptsFrozenRequest(t *testing.T) {
	handler, err := executorhttp.NewHandler(executorhttp.Options{}, func(_ context.Context, request executorhttp.Request) executorhttp.Outcome {
		if request.Arguments["order"] != "review" || request.ArgumentHash != "arguments" || request.ContractHash != "contract" {
			t.Fatalf("unexpected v0.3 request: %#v", request)
		}
		if request.Metadata.ExecutionID != "execution" || request.Metadata.RequestID != "request" {
			t.Fatalf("unexpected v0.3 metadata: %#v", request.Metadata)
		}
		return executorhttp.Success(map[string]any{"ok": true})
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"order":"review"}`))
	request.Header.Set("X-Effectus-Verb", "review-order")
	request.Header.Set("X-Effectus-Request-ID", "request")
	request.Header.Set(invocation.HeaderExecutionID, "execution")
	request.Header.Set(invocation.HeaderSagaID, "saga")
	request.Header.Set(invocation.HeaderEffectID, "effect")
	request.Header.Set(invocation.HeaderAttempt, "1")
	request.Header.Set(invocation.HeaderDirection, string(invocation.DirectionForward))
	request.Header.Set(invocation.HeaderIdempotencyKey, "idempotency")
	request.Header.Set(invocation.HeaderArgumentHash, "arguments")
	request.Header.Set(invocation.HeaderContractHash, "contract")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("handler status = %d, body = %s", response.Code, response.Body.String())
	}
}

type executorFunc func(context.Context, invocation.Request) invocation.Outcome

func (fn executorFunc) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	return fn(ctx, request)
}
