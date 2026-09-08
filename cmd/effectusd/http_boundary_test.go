package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestHTTPErrorMappingIsTypedAndSanitized(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{runtime.ErrInvalidExecuteRequest, 400}, {runtime.ErrIdentityConflict, 409}, {runtime.ErrGenerationMismatch, 409},
		{sql.ErrConnDone, 503}, {runtime.ErrExecutionBusy, 503}, {runtime.ErrBlockedDependency, 503},
		{context.Canceled, 408}, {context.DeadlineExceeded, 504}, {errors.New("password=secret internal SQL"), 500},
		{&runtime.TerminalExecutionError{State: schema.ExecutionFailed, Cause: errors.New("password=secret")}, 422},
		{&runtime.TerminalExecutionError{State: schema.ExecutionBlockedDependency, Cause: context.Canceled}, 409},
	} {
		response := httptest.NewRecorder()
		writeError(response, fmt.Errorf("password=secret: %w", test.err))
		require.Equal(t, test.status, response.Code)
		require.NotContains(t, response.Body.String(), "secret")
		var body map[string]string
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.NotEmpty(t, body["error"])
	}
}

func TestHTTPRoutesAndMethods(t *testing.T) {
	d, _, close := newHTTPContractDaemon(t)
	defer close()
	handler := d.httpHandler("token")
	for _, route := range []struct {
		path, method string
		status       int
	}{
		{"/healthz", "GET", 200}, {"/readyz", "GET", 200}, {"/v1/status", "GET", 200},
		{"/v1/dry-run", "POST", 200}, {"/v1/execute", "POST", 202},
	} {
		for _, method := range []string{"GET", "POST", "HEAD", "DELETE"} {
			t.Run(route.path+"/"+method, func(t *testing.T) {
				body := `{"facts":{"order.risk":0,"order.id":"one"}}`
				if route.path == "/v1/execute" {
					body = `{"namespace":"test","facts":{"order.risk":0,"order.id":"one"}}`
				}
				request := httptest.NewRequest(method, route.path, strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer token")
				request.Header.Set(invocation.HeaderIdempotencyKey, "key")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if method == route.method {
					require.Equal(t, route.status, response.Code, response.Body.String())
				} else {
					require.Equal(t, 405, response.Code)
					require.Equal(t, route.method, response.Header().Get("Allow"))
				}
				require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			})
		}
	}
	for _, path := range []string{"/missing", "/v1", "/v1/", "/v1/execute/", "/v1//execute", "/v1/../healthz", "/healthz/"} {
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, 404, response.Code, path)
		require.JSONEq(t, `{"error":"route not found"}`, response.Body.String())
	}
}

func TestHTTPJSONBoundariesIncludingChunkedBodies(t *testing.T) {
	d, _, close := newHTTPContractDaemon(t)
	defer close()
	server := httptest.NewServer(d.httpHandler("token"))
	defer server.Close()
	for _, route := range []string{"/v1/dry-run", "/v1/execute"} {
		base := `{"facts":{"order.risk":0,"order.id":"one"}}`
		accepted := 200
		if route == "/v1/execute" {
			base = `{"namespace":"test","facts":{"order.risk":0,"order.id":"one"}}`
			accepted = 202
		}
		for _, test := range []struct {
			name, body string
			status     int
		}{
			{"valid", base, accepted}, {"exact", base + strings.Repeat(" ", maxHTTPBodyBytes-len(base)), accepted},
			{"overflow", base + strings.Repeat(" ", maxHTTPBodyBytes+1-len(base)), 413},
			{"trailing", base + " junk", 400}, {"multiple", base + " {}", 400}, {"unknown", `{"unsupported":true}`, 400}, {"empty", "", 400},
		} {
			t.Run(route+"/"+test.name, func(t *testing.T) {
				// Hide the reader's length so net/http sends an actual chunked body.
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+route, io.NopCloser(strings.NewReader(test.body)))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer token")
				request.Header.Set(invocation.HeaderIdempotencyKey, "key")
				response, err := server.Client().Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, test.status, response.StatusCode, string(body))
			})
		}
	}
}

func TestHTTPDecoderPreservesFullIntegerText(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/execute", strings.NewReader(`{"facts":{"n":9223372036854775807}}`))
	var body executeBody
	require.NoError(t, decodeJSON(request, &body))
	require.Equal(t, json.Number("9223372036854775807"), body.Facts["n"])
}

func TestHTTPNamespaceAndUniverseIdentity(t *testing.T) {
	d, _, close := newHTTPContractDaemon(t)
	defer close()
	handler := d.httpHandler("token")
	for _, body := range []string{`{"namespace":"","facts":{}}`, `{"namespace":"  ","universe":" ","facts":{}}`, `{"namespace":"one","universe":"two","facts":{}}`} {
		response := executeHTTPContractRequest(t, handler, "token", "same-key", body)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	var executionID string
	for _, prefix := range []string{`"namespace":"test"`, `"namespace":" test "`, `"universe":"test"`, `"namespace":" ","universe":" test "`, `"namespace":"test","universe":"test"`} {
		response := executeHTTPContractRequest(t, handler, "token", "same-key", "{"+prefix+`,"facts":{"order.risk":0,"order.id":"one"}}`)
		require.Equal(t, 202, response.Code, response.Body.String())
		var result runtime.ExecuteResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		if executionID == "" {
			executionID = result.ExecutionID
		}
		require.NotEmpty(t, executionID)
		require.Equal(t, executionID, result.ExecutionID)
	}
}
