package invocation

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescriptorRoundTripIsCanonicalAndImmutable(t *testing.T) {
	headers := map[string]string{"Z-Trace": "z", "Accept": "application/json"}
	settings := map[string]string{"timeout": "2s", "method": "POST"}
	descriptor, err := NewDescriptor(DescriptorSpec{
		Type: DescriptorHTTP, ResolverID: "http/v1", Reference: "https://executor.example/review",
		Headers: headers, Settings: settings,
	})
	require.NoError(t, err)
	headers["Authorization"] = "changed"
	settings["method"] = "DELETE"

	encoded, err := descriptor.CanonicalJSON()
	require.NoError(t, err)
	parsed, err := ParseDescriptor(encoded)
	require.NoError(t, err)
	roundTrip, err := parsed.CanonicalJSON()
	require.NoError(t, err)
	require.Equal(t, encoded, roundTrip)
	require.Equal(t, "POST", parsed.Settings()["method"])
	require.NotContains(t, parsed.Headers(), "Authorization")
}

func TestDescriptorRejectsCaseInsensitiveHeaderCollisionsBeforeCanonicalEncoding(t *testing.T) {
	_, err := NewDescriptor(DescriptorSpec{
		Type: DescriptorHTTP, Headers: map[string]string{"Authorization": "one", "authorization": "two"},
	})
	require.EqualError(t, err, `invocation descriptor headers "Authorization" and "authorization" collide case-insensitively`)
	_, err = NewDescriptor(DescriptorSpec{
		Type: DescriptorHTTP, Headers: map[string]string{"X-Trace": "one", "x-trace": "two"},
	})
	require.ErrorContains(t, err, "collide case-insensitively")
	_, err = NewDescriptor(DescriptorSpec{Type: DescriptorHTTP, Headers: map[string]string{"IDEMPOTENCY-KEY": "forged"}})
	require.ErrorContains(t, err, "reserved")
}

func TestDescriptorRejectsReservedMetadataUnknownFieldsAndMutableOCI(t *testing.T) {
	_, err := NewDescriptor(DescriptorSpec{Type: DescriptorHTTP, Headers: map[string]string{HeaderExecutionID: "forged"}})
	require.ErrorContains(t, err, "reserved")
	_, err = ParseDescriptor([]byte(`{"type":"http","unknown":true}`))
	require.ErrorContains(t, err, "unknown field")
	_, err = ParseDescriptor([]byte(`{"type":"http","type":"grpc"}`))
	require.ErrorContains(t, err, "duplicate")
	_, err = NewDescriptor(DescriptorSpec{Type: DescriptorOCI, Reference: "ghcr.io/acme/executor:latest"})
	require.ErrorContains(t, err, "digest-pinned")
}

type descriptorTestExecutor struct{}

func (descriptorTestExecutor) Invoke(context.Context, Request) Outcome {
	return Outcome{Class: OutcomeSuccess}
}

type descriptorTestCloser struct{ io.Closer }

func TestRegistryFailsClosedAndReturnsOwnedCloser(t *testing.T) {
	closer := &descriptorTestCloser{Closer: io.NopCloser(strings.NewReader(""))}
	registry, err := NewRegistry([]ResolverRegistration{{
		ID: "http/v1",
		Resolver: ResolverFunc(func(context.Context, Descriptor) (Executor, io.Closer, error) {
			return descriptorTestExecutor{}, closer, nil
		}),
	}})
	require.NoError(t, err)
	descriptor, err := NewDescriptor(DescriptorSpec{Type: DescriptorHTTP, ResolverID: "http/v1"})
	require.NoError(t, err)
	executor, owned, err := registry.Resolve(context.Background(), descriptor)
	require.NoError(t, err)
	require.NotNil(t, executor)
	require.Same(t, closer, owned)

	missing, err := NewDescriptor(DescriptorSpec{Type: DescriptorHTTP, ResolverID: "missing/v1"})
	require.NoError(t, err)
	_, _, err = registry.Resolve(context.Background(), missing)
	require.ErrorContains(t, err, "not registered")
}
