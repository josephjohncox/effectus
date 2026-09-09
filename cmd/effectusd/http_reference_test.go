package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func httpReferenceExample(t *testing.T, name string) []byte {
	t.Helper()
	doc, err := os.ReadFile("../../docs/HTTP_API.md")
	require.NoError(t, err)
	marker := "<!-- http-example: " + name + " -->\n```json\n"
	require.Equal(t, 1, strings.Count(string(doc), marker), "example must have a unique marker")
	_, rest, _ := strings.Cut(string(doc), marker)
	body, _, closed := strings.Cut(rest, "```")
	require.True(t, closed)
	require.True(t, json.Valid([]byte(body)))
	return []byte(body)
}

func assertHTTPReferenceExample(t *testing.T, name string, body []byte) {
	t.Helper()
	var expected, actual any
	require.NoError(t, json.Unmarshal(httpReferenceExample(t, name), &expected))
	require.NoError(t, json.Unmarshal(body, &actual))
	assertHTTPReferenceValue(t, name, expected, actual)
}

// Only explicit angle-bracket placeholders vary. Field names, types, omissions,
// arrays, booleans, policies, and other literal values must match exactly.
func assertHTTPReferenceValue(t *testing.T, path string, expected, actual any) {
	t.Helper()
	switch value := expected.(type) {
	case map[string]any:
		object, ok := actual.(map[string]any)
		require.True(t, ok, path)
		require.Len(t, object, len(value), path)
		for key, want := range value {
			got, exists := object[key]
			require.True(t, exists, path+"."+key)
			assertHTTPReferenceValue(t, path+"."+key, want, got)
		}
	case []any:
		array, ok := actual.([]any)
		require.True(t, ok, path)
		require.Len(t, array, len(value), path)
		for i := range value {
			assertHTTPReferenceValue(t, path+"[]", value[i], array[i])
		}
	case string:
		if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
			text, ok := actual.(string)
			require.True(t, ok, path)
			require.NotEmpty(t, text, path)
			return
		}
		require.Equal(t, expected, actual, path)
	default:
		require.Equal(t, expected, actual, path)
	}
}

func requestHTTPReference(t *testing.T, server *httptest.Server, method, path string, body []byte, headers http.Header) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header = headers.Clone()
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))
	if method != http.MethodHead {
		require.True(t, bytes.HasSuffix(data, []byte("\n")), "JSON responses must retain their newline")
	}
	return response.StatusCode, response.Header.Clone(), data
}

func TestDocumentedHTTPJSONAndAcceptedOnlyDisposition(t *testing.T) {
	d, calls, cleanup := newHTTPContractDaemon(t)
	defer cleanup()
	server := httptest.NewServer(d.httpHandler("token"))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer token"}, "Idempotency-Key": {"reference"}, "Content-Type": {"application/json"}}
	code, _, body := requestHTTPReference(t, server, "GET", "/healthz", nil, nil)
	require.Equal(t, 200, code)
	assertHTTPReferenceExample(t, "health-response", body)
	for _, path := range []string{"/readyz", "/v1/status"} {
		code, responseHeaders, body := requestHTTPReference(t, server, "GET", path, nil, headers)
		require.Equal(t, 200, code)
		require.Empty(t, responseHeaders.Get("ETag"))
		assertHTTPReferenceExample(t, "generation-response", body)
	}
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/dry-run", httpReferenceExample(t, "dry-run-request"), headers)
	require.Equal(t, 200, code)
	assertHTTPReferenceExample(t, "dry-run-response", body)
	nonmatching := bytes.ReplaceAll(httpReferenceExample(t, "dry-run-request"), []byte("90"), []byte("0"))
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/dry-run", nonmatching, headers)
	require.Equal(t, 200, code)
	var evaluations []runtime.PlanEvaluation
	require.NoError(t, json.Unmarshal(body, &evaluations))
	require.Len(t, evaluations, 1, "nonmatching plans remain in the dry-run array")
	require.False(t, evaluations[0].Matched)
	require.Zero(t, *calls)
	requestBody := httpReferenceExample(t, "execute-request")
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/execute?wait_mode=terminal", requestBody, headers)
	require.Equal(t, 202, code)
	assertHTTPReferenceExample(t, "accepted-response", body)
	require.Zero(t, *calls, "HTTP does not select a terminal wait through a query")
	var admission runtime.ExecuteResult
	require.NoError(t, json.Unmarshal(body, &admission))
	require.Equal(t, d.engine.ActiveGenerationDigest(), admission.GenerationDigest)
	_, err := d.engine.Execute(t.Context(), runtime.ExecuteRequest{ResumeExecutionID: admission.ExecutionID, WaitMode: runtime.WaitTerminal})
	var terminal *runtime.TerminalExecutionError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, schema.ExecutionFailed, terminal.State)
	require.Equal(t, 1, *calls)
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/execute", requestBody, headers)
	require.Equal(t, 202, code)
	assertHTTPReferenceExample(t, "failed-replay-response", body)
	var replay runtime.ExecuteResult
	require.NoError(t, json.Unmarshal(body, &replay))
	require.Equal(t, admission.ExecutionID, replay.ExecutionID)
	conflict := bytes.ReplaceAll(requestBody, []byte(`"one"`), []byte(`"other"`))
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/execute", conflict, headers)
	require.Equal(t, 409, code)
	assertHTTPReferenceExample(t, "identity-error", body)
	require.Equal(t, 1, *calls)
}

func TestDocumentedHTTPConditionalAndRoutingBehavior(t *testing.T) {
	d, calls, cleanup := newHTTPContractDaemon(t)
	defer cleanup()
	server := httptest.NewServer(d.httpHandler("token"))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer token"}, "Idempotency-Key": {"reference"}, "Content-Type": {"text/plain"}}
	digest := d.engine.ActiveGenerationDigest()
	for _, test := range []struct {
		value string
		code  int
	}{{"", 202}, {`""`, 202}, {digest, 202}, {`"` + digest + `"`, 202}, {`""` + digest + `""`, 202}, {"*", 409}, {`W/"` + digest + `"`, 409}, {`"` + digest + `", "other"`, 409}} {
		headers.Set("If-Match", test.value)
		code, _, _ := requestHTTPReference(t, server, "POST", "/v1/execute", httpReferenceExample(t, "execute-request"), headers)
		require.Equal(t, test.code, code, test.value)
	}
	headers.Del("If-Match")
	for _, path := range []string{"/v1", "/v1/unknown", "/v1/execute/"} {
		code, responseHeaders, body := requestHTTPReference(t, server, "GET", path, nil, nil)
		require.Equal(t, 401, code)
		require.Equal(t, `Bearer realm="effectusd"`, responseHeaders.Get("WWW-Authenticate"))
		assertHTTPReferenceExample(t, "authentication-error", body)
		code, _, body = requestHTTPReference(t, server, "GET", path, nil, headers)
		require.Equal(t, 404, code)
		assertHTTPReferenceExample(t, "route-error", body)
	}
	duplicateAuth := headers.Clone()
	duplicateAuth.Add("Authorization", "Bearer token")
	code, _, body := requestHTTPReference(t, server, "GET", "/v1/status", nil, duplicateAuth)
	require.Equal(t, 401, code)
	assertHTTPReferenceExample(t, "authentication-error", body)
	code, responseHeaders, body := requestHTTPReference(t, server, "HEAD", "/healthz", nil, nil)
	require.Equal(t, 405, code)
	require.Equal(t, "GET", responseHeaders.Get("Allow"))
	require.Empty(t, body, "the actual HTTP server suppresses HEAD bodies")
	code, _, _ = requestHTTPReference(t, server, "GET", "/healthz?ignored=true", nil, nil)
	require.Equal(t, 200, code)
	unknownField := []byte(`{"namespace":"docs","facts":{},"wait_mode":"terminal"}`)
	code, _, _ = requestHTTPReference(t, server, "POST", "/v1/execute", unknownField, headers)
	require.Equal(t, 400, code)
	require.Zero(t, *calls)
	d.stopHTTPAdmission()
	for _, path := range []string{"/healthz", "/readyz", "/v1/status", "/v1/execute", "/missing"} {
		code, _, body := requestHTTPReference(t, server, "GET", path, nil, nil)
		require.Equal(t, 503, code)
		require.JSONEq(t, `{"error":"server is draining"}`, string(body))
	}
}

func TestDocumentedHTTPReadinessDoesNotProbeTheLedger(t *testing.T) {
	d, _, cleanup := newHTTPContractDaemon(t)
	defer cleanup()
	ledger := &failingTransportLookupLedger{ExecutionLedger: schema.NewInMemoryExecutionLedger(), failure: sql.ErrConnDone}
	require.NoError(t, d.engine.ConfigureLedger(ledger, nil))
	server := httptest.NewServer(d.httpHandler("token"))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer token"}, "Idempotency-Key": {"reference"}}
	code, _, body := requestHTTPReference(t, server, "GET", "/readyz", nil, nil)
	require.Equal(t, 200, code)
	t.Logf("Readiness wire example: %s", body)
	assertHTTPReferenceExample(t, "generation-response", body)
	require.Zero(t, ledger.lookups.Load())
	code, _, body = requestHTTPReference(t, server, "POST", "/v1/execute", httpReferenceExample(t, "execute-request"), headers)
	require.Equal(t, 503, code)
	require.JSONEq(t, `{"error":"execution dependency is unavailable"}`, string(body))
	require.Positive(t, ledger.lookups.Load())
	require.Zero(t, ledger.writes.Load())
}
