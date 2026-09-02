// Package legacyconsumer is representative source written against Effectus v0.3.
package legacyconsumer

import (
	"context"

	"github.com/josephjohncox/effectus/compat/v03/executorhttp"
	"github.com/josephjohncox/effectus/compat/v03/invocation"
)

type descriptorProvider struct{}

func (descriptorProvider) InvocationResolverDescriptor() any { return "legacy" }

var _ invocation.ResolverDescriptorProvider = descriptorProvider{}

func positionalLiterals() {
	metadata := invocation.Context{}
	_ = invocation.Request{metadata, "review-order", map[string]any{"order": "review"}, "arguments", "contract"}
	_ = executorhttp.Request{map[string]any{"order": "review"}, metadata, "arguments", "contract"}
	_ = executorhttp.HandlerFunc(func(context.Context, executorhttp.Request) executorhttp.Outcome {
		return executorhttp.Success(nil)
	})
}
