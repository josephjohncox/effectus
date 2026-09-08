package invocation

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPExecutorRequiresOneCompleteSuccessfulJSONValue(t *testing.T) {
	for _, test := range []struct {
		name, body string
		limit      int64
		success    bool
	}{
		{"object", `{"ok":true}`, 0, true},
		{"null", "null", 0, true},
		{"whitespace", "null \n\t", 0, true},
		{"empty", "", 0, false},
		{"only-whitespace", " \n", 0, false},
		{"trailing-junk", "null junk", 0, false},
		{"second-value", "null {}", 0, false},
		{"exact-limit", "null", 4, true},
		{"overflow-space", "null ", 4, false},
		{"oversize", `{"long":"response"}`, 4, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }))
			defer server.Close()
			executor, err := NewHTTPExecutor(HTTPExecutor{URL: server.URL, MaxResponseBytes: test.limit})
			require.NoError(t, err)
			outcome := executor.Invoke(t.Context(), Request{Arguments: map[string]any{}})
			if test.success {
				require.NoError(t, outcome.Err)
				require.Equal(t, OutcomeSuccess, outcome.Class)
			} else {
				require.Error(t, outcome.Err)
				require.Equal(t, OutcomeUnknown, outcome.Class)
			}
		})
	}
}

func TestHTTPExecutorValidatesPublicStructAndOwnedDefaults(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte("null"))
	}))
	defer server.Close()
	for _, limit := range []int64{-1, math.MinInt64, math.MaxInt64} {
		_, err := NewHTTPExecutor(HTTPExecutor{URL: server.URL, MaxResponseBytes: limit})
		require.Error(t, err)
		raw := &HTTPExecutor{URL: server.URL, MaxResponseBytes: limit}
		outcome := raw.Invoke(t.Context(), Request{})
		require.Equal(t, OutcomePermanentFailure, outcome.Class)
		require.Error(t, outcome.Err)
	}
	require.Zero(t, calls.Load())
	raw := &HTTPExecutor{URL: server.URL}
	outcome := raw.Invoke(t.Context(), Request{})
	require.NoError(t, outcome.Err)
	require.Equal(t, OutcomeSuccess, outcome.Class)
	require.Nil(t, raw.Client, "Invoke defaults must not mutate the public configuration")
	require.Zero(t, raw.MaxResponseBytes)
	headers := map[string]string{"X-Trace": "original"}
	executor, err := NewHTTPExecutor(HTTPExecutor{URL: server.URL, Headers: headers})
	require.NoError(t, err)
	headers["X-Trace"] = "changed"
	require.Equal(t, "original", executor.Headers["X-Trace"])
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := calls.Load()
	outcome = executor.Invoke(ctx, Request{})
	require.ErrorIs(t, outcome.Err, context.Canceled)
	require.Equal(t, OutcomeRetryableKnownNotCommitted, outcome.Class)
	require.Equal(t, before, calls.Load())
}
