// Package executorhttp adapts an Effectus HTTP verb target to a Go handler.
package executorhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/josephjohncox/effectus/invocation"
)

const defaultMaxRequestBytes int64 = 1 << 20

// Request contains business arguments and immutable Effectus metadata.
type Request struct {
	Arguments    map[string]any
	Metadata     invocation.Context
	ArgumentHash string
	ContractHash string
}

// Outcome is the explicit result returned by a business executor.
type Outcome = invocation.Outcome

// Direction identifies forward and compensation calls.
type Direction = invocation.Direction

const (
	DirectionForward      = invocation.DirectionForward
	DirectionCompensation = invocation.DirectionCompensation
)

// HandlerFunc performs one idempotent business operation.
type HandlerFunc func(context.Context, Request) Outcome

// Options controls the HTTP boundary.
type Options struct {
	MaxRequestBytes int64
}

// NewHandler creates a strict Effectus business executor endpoint.
func NewHandler(options Options, handler HandlerFunc) (http.Handler, error) {
	if handler == nil {
		return nil, fmt.Errorf("executor HTTP handler is required")
	}
	if options.MaxRequestBytes < 0 {
		return nil, fmt.Errorf("executor HTTP maximum request bytes cannot be negative")
	}
	if options.MaxRequestBytes == 0 {
		options.MaxRequestBytes = defaultMaxRequestBytes
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		decoded, err := decodeRequest(request, options.MaxRequestBytes)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		outcome := handler(request.Context(), decoded)
		if err := invocation.ValidateOutcome(outcome); err != nil {
			outcome = invocation.Outcome{
				Class: invocation.OutcomeUnknown,
				Err:   fmt.Errorf("business executor returned an invalid outcome: %w", err),
			}
		}
		writeOutcome(response, outcome)
	}), nil
}

func decodeRequest(request *http.Request, maxBytes int64) (Request, error) {
	metadata, err := decodeMetadata(request.Header)
	if err != nil {
		return Request{}, err
	}
	reader := io.LimitReader(request.Body, maxBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return Request{}, fmt.Errorf("read executor request: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return Request{}, fmt.Errorf("executor request exceeds %d bytes", maxBytes)
	}
	arguments := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&arguments); err != nil {
		return Request{}, fmt.Errorf("decode executor arguments: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Request{}, err
	}
	argumentHash := strings.TrimSpace(request.Header.Get(invocation.HeaderArgumentHash))
	if argumentHash == "" {
		return Request{}, fmt.Errorf("%s header is required", invocation.HeaderArgumentHash)
	}
	contractHash := strings.TrimSpace(request.Header.Get(invocation.HeaderContractHash))
	if contractHash == "" {
		return Request{}, fmt.Errorf("%s header is required", invocation.HeaderContractHash)
	}
	return Request{
		Arguments: arguments, Metadata: metadata,
		ArgumentHash: argumentHash, ContractHash: contractHash,
	}, nil
}

func decodeMetadata(headers http.Header) (invocation.Context, error) {
	executionID := strings.TrimSpace(headers.Get(invocation.HeaderExecutionID))
	if executionID == "" {
		return invocation.Context{}, fmt.Errorf("%s header is required", invocation.HeaderExecutionID)
	}
	sagaID := strings.TrimSpace(headers.Get(invocation.HeaderSagaID))
	if sagaID == "" {
		return invocation.Context{}, fmt.Errorf("%s header is required", invocation.HeaderSagaID)
	}
	effectID := strings.TrimSpace(headers.Get(invocation.HeaderEffectID))
	if effectID == "" {
		return invocation.Context{}, fmt.Errorf("%s header is required", invocation.HeaderEffectID)
	}
	idempotencyKey := strings.TrimSpace(headers.Get(invocation.HeaderIdempotencyKey))
	if idempotencyKey == "" {
		return invocation.Context{}, fmt.Errorf("%s header is required", invocation.HeaderIdempotencyKey)
	}
	attempt, err := strconv.ParseUint(strings.TrimSpace(headers.Get(invocation.HeaderAttempt)), 10, 64)
	if err != nil || attempt == 0 {
		return invocation.Context{}, fmt.Errorf("%s header must be a positive integer", invocation.HeaderAttempt)
	}
	direction := invocation.Direction(strings.TrimSpace(headers.Get(invocation.HeaderDirection)))
	if direction != invocation.DirectionForward && direction != invocation.DirectionCompensation {
		return invocation.Context{}, fmt.Errorf("%s header must be forward or compensation", invocation.HeaderDirection)
	}
	metadata := invocation.Context{
		RequestID:   executionID,
		ExecutionID: executionID,
		Saga: invocation.Saga{
			SagaID:         sagaID,
			EffectID:       effectID,
			Attempt:        attempt,
			Direction:      direction,
			IdempotencyKey: idempotencyKey,
		},
	}
	if raw := strings.TrimSpace(headers.Get(invocation.HeaderDeadline)); raw != "" {
		deadline, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return invocation.Context{}, fmt.Errorf("%s header is invalid: %w", invocation.HeaderDeadline, err)
		}
		metadata.Deadline = deadline
	}
	if raw := strings.TrimSpace(headers.Get(invocation.HeaderFencingGrants)); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata.FencingGrants); err != nil {
			return invocation.Context{}, fmt.Errorf("%s header is invalid: %w", invocation.HeaderFencingGrants, err)
		}
	}
	return metadata, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode executor arguments: %w", err)
	}
	return fmt.Errorf("executor request contains more than one JSON value")
}

func writeOutcome(response http.ResponseWriter, outcome invocation.Outcome) {
	if outcome.Class == invocation.OutcomeSuccess {
		payload, err := json.Marshal(outcome.Result)
		if err != nil {
			response.Header().Set(invocation.HeaderOutcome, string(invocation.OutcomeUnknown))
			http.Error(response, "encode business result", http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(append(payload, '\n'))
		return
	}
	response.Header().Set(invocation.HeaderOutcome, string(outcome.Class))
	status := http.StatusInternalServerError
	switch outcome.Class {
	case invocation.OutcomeRetryableKnownNotCommitted:
		status = http.StatusServiceUnavailable
	case invocation.OutcomePermanentFailure:
		status = http.StatusUnprocessableEntity
	case invocation.OutcomeStaleFence:
		status = http.StatusConflict
	case invocation.OutcomeUnknown:
		status = http.StatusInternalServerError
	}
	http.Error(response, outcome.Err.Error(), status)
}

// Success reports a committed business operation.
func Success(result any) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

// Retryable reports a failure that is known not to have committed.
func Retryable(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
}

// Permanent reports a permanent business failure.
func Permanent(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: err}
}

// Unknown reports a failure whose commit state is not known.
func Unknown(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
}

// StaleFence reports a rejected stale fencing token.
func StaleFence(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeStaleFence, Err: err}
}
