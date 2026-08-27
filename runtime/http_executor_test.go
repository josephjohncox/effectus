package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/effectus/effectus-go/compiler"
	"github.com/stretchr/testify/require"
)

func TestRuntimeHTTPExecutorBoundsResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	}))
	defer server.Close()
	executor := &HTTPExecutor{
		config: &compiler.HTTPExecutorConfig{URL: server.URL, Method: http.MethodPost},
		client: &http.Client{Timeout: time.Second},
	}
	_, err := executor.Execute(t.Context(), map[string]interface{}{"value": true})
	require.ErrorContains(t, err, "exceeds")
}

func TestRuntimeHTTPExecutorRejectsMissingFiniteTimeout(t *testing.T) {
	executor := &HTTPExecutor{config: &compiler.HTTPExecutorConfig{URL: "http://example.invalid", Method: http.MethodPost}, client: &http.Client{}}
	_, err := executor.Execute(t.Context(), nil)
	require.ErrorContains(t, err, "finite timeout")
}
