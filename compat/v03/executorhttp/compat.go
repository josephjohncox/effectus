// Package executorhttp preserves the v0.3 HTTP business-executor handler API.
package executorhttp

import (
	"context"
	"net/http"

	"github.com/josephjohncox/effectus/compat/v03/invocation"
	root "github.com/josephjohncox/effectus/executorhttp"
	current "github.com/josephjohncox/effectus/invocation"
)

// Request contains business arguments and immutable Effectus metadata.
// Its field order and types are the v0.3 contract and support positional
// literals written against that release.
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
type Options = root.Options

// NewHandler creates a v0.3-compatible HTTP business executor endpoint.
// It adapts the frozen request shape to the canonical root handler.
func NewHandler(options Options, handler HandlerFunc) (http.Handler, error) {
	if handler == nil {
		return root.NewHandler(options, nil)
	}
	return root.NewHandler(options, func(ctx context.Context, request current.Request) current.Outcome {
		return handler(ctx, Request{
			Arguments:    request.Arguments,
			Metadata:     request.Metadata,
			ArgumentHash: request.ArgumentHash,
			ContractHash: request.ContractHash,
		})
	})
}

var Success = root.Success
var Retryable = root.Retryable
var Permanent = root.Permanent
var Unknown = root.Unknown
var StaleFence = root.StaleFence
