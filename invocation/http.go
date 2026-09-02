package invocation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderVerb           = "X-Effectus-Verb"
	HeaderRequestID      = "X-Effectus-Request-ID"
	HeaderExecutionID    = "X-Effectus-Execution-ID"
	HeaderSagaID         = "X-Effectus-Saga-ID"
	HeaderEffectID       = "X-Effectus-Effect-ID"
	HeaderAttempt        = "X-Effectus-Attempt"
	HeaderDirection      = "X-Effectus-Direction"
	HeaderArgumentHash   = "X-Effectus-Argument-Hash"
	HeaderContractHash   = "X-Effectus-Contract-Hash"
	HeaderFencingGrants  = "X-Effectus-Fencing-Grants"
	HeaderDeadline       = "X-Effectus-Deadline"
	HeaderOutcome        = "X-Effectus-Outcome"
	HeaderIdempotencyKey = "Idempotency-Key"
)

var reservedHTTPHeaders = map[string]struct{}{
	strings.ToLower(HeaderVerb): {}, strings.ToLower(HeaderRequestID): {}, strings.ToLower(HeaderExecutionID): {}, strings.ToLower(HeaderSagaID): {},
	strings.ToLower(HeaderEffectID): {}, strings.ToLower(HeaderAttempt): {},
	strings.ToLower(HeaderDirection): {}, strings.ToLower(HeaderArgumentHash): {},
	strings.ToLower(HeaderContractHash): {}, strings.ToLower(HeaderFencingGrants): {},
	strings.ToLower(HeaderDeadline): {}, strings.ToLower(HeaderOutcome): {},
	strings.ToLower(HeaderIdempotencyKey): {},
}

// HTTPExecutor sends one invocation without an internal retry loop.
type HTTPExecutor struct {
	URL              string
	Method           string
	Headers          map[string]string
	Client           *http.Client
	MaxResponseBytes int64
}

const HTTPResolverID = "effectus/http/v1"

func NewHTTPExecutor(executor HTTPExecutor) (*HTTPExecutor, error) {
	if strings.TrimSpace(executor.URL) == "" {
		return nil, fmt.Errorf("invocation HTTP URL is required")
	}
	if executor.Method == "" {
		executor.Method = http.MethodPost
	}
	if err := validateStaticHTTPHeaders(executor.Headers); err != nil {
		return nil, err
	}
	if executor.Client == nil {
		executor.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if executor.MaxResponseBytes <= 0 {
		executor.MaxResponseBytes = 1 << 20
	}
	return &executor, nil
}

// InvocationResolverDescriptor returns the immutable transport configuration.
func (executor *HTTPExecutor) InvocationResolverDescriptor() (Descriptor, error) {
	if executor == nil {
		return Descriptor{}, fmt.Errorf("invocation HTTP executor is nil")
	}
	return NewDescriptor(DescriptorSpec{
		Type: DescriptorHTTP, ResolverID: HTTPResolverID, Reference: executor.URL,
		Headers: executor.Headers,
		Settings: map[string]string{
			"method":             executor.Method,
			"max_response_bytes": strconv.FormatInt(executor.MaxResponseBytes, 10),
		},
	})
}

func (executor *HTTPExecutor) Invoke(ctx context.Context, request Request) Outcome {
	if executor == nil {
		return Outcome{Class: OutcomePermanentFailure, Err: fmt.Errorf("invocation HTTP executor is nil")}
	}
	if err := validateStaticHTTPHeaders(executor.Headers); err != nil {
		return Outcome{Class: OutcomePermanentFailure, Err: err}
	}
	payload, err := json.Marshal(request.Arguments)
	if err != nil {
		return Outcome{Class: OutcomePermanentFailure, Err: fmt.Errorf("encode invocation arguments: %w", err)}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, executor.Method, executor.URL, bytes.NewReader(payload))
	if err != nil {
		return Outcome{Class: OutcomePermanentFailure, Err: fmt.Errorf("build invocation request: %w", err)}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	for name, value := range executor.Headers {
		httpRequest.Header.Set(name, value)
	}
	setInvocationHeaders(httpRequest.Header, request)
	response, err := executor.Client.Do(httpRequest)
	if err != nil {
		return Outcome{Class: OutcomeUnknown, Err: fmt.Errorf("send invocation: %w", err)}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, executor.MaxResponseBytes+1))
	if readErr != nil {
		return Outcome{Class: OutcomeUnknown, Err: fmt.Errorf("read invocation response: %w", readErr)}
	}
	if int64(len(body)) > executor.MaxResponseBytes {
		return Outcome{Class: OutcomeUnknown, Err: fmt.Errorf("invocation response exceeds %d bytes", executor.MaxResponseBytes)}
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		if len(body) == 0 {
			return Outcome{Class: OutcomeSuccess, Result: nil}
		}
		var result any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil {
			return Outcome{Class: OutcomeUnknown, Err: fmt.Errorf("decode successful invocation response: %w", err)}
		}
		return Outcome{Class: OutcomeSuccess, Result: result}
	}
	class := OutcomeClass(strings.TrimSpace(response.Header.Get(HeaderOutcome)))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	outcome := Outcome{Class: class, Err: fmt.Errorf("invocation HTTP status %d: %s", response.StatusCode, message)}
	if ValidateOutcome(outcome) != nil || class == OutcomeSuccess {
		outcome.Class = OutcomeUnknown
	}
	return outcome
}

// validateStaticHTTPHeaders rejects collisions before a map can be canonicalized
// or emitted. HTTP field names are case-insensitive, including reserved names.
func validateStaticHTTPHeaders(headers map[string]string) error {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	seenFoldedNames := make(map[string]string, len(names))
	for _, name := range names {
		foldedName := strings.ToLower(name)
		if previous, duplicate := seenFoldedNames[foldedName]; duplicate {
			return fmt.Errorf("static headers %q and %q collide case-insensitively", previous, name)
		}
		seenFoldedNames[foldedName] = name
		if _, reserved := reservedHTTPHeaders[foldedName]; reserved || strings.HasPrefix(foldedName, "x-effectus-") {
			return fmt.Errorf("static header %q is reserved for Effectus invocation metadata", name)
		}
	}
	return nil
}

func setInvocationHeaders(headers http.Header, request Request) {
	grants, _ := json.Marshal(request.Metadata.FencingGrants)
	headers.Set(HeaderVerb, request.Verb)
	headers.Set(HeaderRequestID, request.Metadata.RequestID)
	headers.Set(HeaderExecutionID, request.Metadata.ExecutionID)
	headers.Set(HeaderSagaID, request.Metadata.Saga.SagaID)
	headers.Set(HeaderEffectID, request.Metadata.Saga.EffectID)
	headers.Set(HeaderAttempt, strconv.FormatUint(request.Metadata.Saga.Attempt, 10))
	headers.Set(HeaderDirection, string(request.Metadata.Saga.Direction))
	headers.Set(HeaderIdempotencyKey, request.Metadata.Saga.IdempotencyKey)
	headers.Set(HeaderArgumentHash, request.ArgumentHash)
	headers.Set(HeaderContractHash, request.ContractHash)
	headers.Set(HeaderFencingGrants, string(grants))
	if !request.Metadata.Deadline.IsZero() {
		headers.Set(HeaderDeadline, request.Metadata.Deadline.UTC().Format(time.RFC3339Nano))
	}
}
