package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDocumentedHTTPAuthenticationNormalizesOnlyOuterFieldWhitespace(t *testing.T) {
	d, calls, cleanup := newHTTPContractDaemon(t)
	defer cleanup()
	server := httptest.NewServer(d.httpHandler("token"))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")

	for _, test := range []struct {
		name   string
		values []string
		status int
	}{
		{"canonical", []string{"Bearer token"}, http.StatusOK},
		{"trailing spaces", []string{"Bearer token   "}, http.StatusOK},
		{"trailing tabs", []string{"Bearer token\t\t"}, http.StatusOK},
		{"outer spaces and tabs", []string{"\t Bearer token \t"}, http.StatusOK},
		{"extra space after prefix", []string{"Bearer  token"}, http.StatusUnauthorized},
		{"extra tab after prefix", []string{"Bearer \ttoken"}, http.StatusUnauthorized},
		{"wrong token with outer whitespace", []string{"Bearer wrong-token  \t"}, http.StatusUnauthorized},
		{"wrong prefix case", []string{"bearer token"}, http.StatusUnauthorized},
		{"duplicate values", []string{"Bearer token", "Bearer token"}, http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
			require.NoError(t, err)
			defer connection.Close()
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.NoError(t, connection.SetDeadline(deadline))

			// Write the exact field bytes. http.Client and Request.Write can trim
			// whitespace before transmission and would not test the server parser.
			wire := "GET /v1/status HTTP/1.1\r\nHost: " + address + "\r\n"
			for _, value := range test.values {
				wire += "Authorization: " + value + "\r\n"
			}
			wire += "Connection: close\r\n\r\n"
			written, err := io.WriteString(connection, wire)
			require.NoError(t, err)
			require.Equal(t, len(wire), written)
			response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, test.status, response.StatusCode)
			require.Equal(t, "application/json", response.Header.Get("Content-Type"))
			if test.status == http.StatusOK {
				assertHTTPReferenceExample(t, "generation-response", body)
			} else {
				require.Equal(t, `Bearer realm="effectusd"`, response.Header.Get("WWW-Authenticate"))
				assertHTTPReferenceExample(t, "authentication-error", body)
			}
		})
	}
	require.Zero(t, *calls, "authentication/status checks must not invoke a verb")
}
