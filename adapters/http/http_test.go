package http

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHTTPSourceValidatesAuthentication(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "bearer token is required",
			config: Config{SourceID: "source", AuthMethod: "bearer_token"},
		},
		{
			name:   "API key is required",
			config: Config{SourceID: "source", AuthMethod: "api_key"},
		},
		{
			name:   "unknown method is rejected",
			config: Config{SourceID: "source", AuthMethod: "basic"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewHTTPSource(&test.config)
			require.Error(t, err)
		})
	}
}

func TestAuthenticationRejectsEmptyAndIncorrectCredentials(t *testing.T) {
	t.Run("bearer token", func(t *testing.T) {
		source := &HTTPSource{config: &Config{
			AuthMethod: "bearer_token",
			AuthConfig: map[string]string{"token": "secret"},
		}}

		request := httptest.NewRequest(stdhttp.MethodPost, "/webhook", nil)
		require.False(t, source.authenticateRequest(request))
		request.Header.Set("Authorization", "Bearer wrong")
		require.False(t, source.authenticateRequest(request))
		request.Header.Set("Authorization", "Bearer secret")
		require.True(t, source.authenticateRequest(request))
	})

	t.Run("API key", func(t *testing.T) {
		source := &HTTPSource{config: &Config{
			AuthMethod: "api_key",
			AuthConfig: map[string]string{"expected_token": "secret"},
		}}

		request := httptest.NewRequest(stdhttp.MethodPost, "/webhook", nil)
		require.False(t, source.authenticateRequest(request))
		request.Header.Set("X-API-Key", "wrong")
		require.False(t, source.authenticateRequest(request))
		request.Header.Set("X-API-Key", "secret")
		require.True(t, source.authenticateRequest(request))
	})
}

func TestHandleWebhookRejectsOversizedBody(t *testing.T) {
	source, err := NewHTTPSource(&Config{SourceID: "source", AuthMethod: "none"})
	require.NoError(t, err)

	body := strings.NewReader(strings.Repeat("x", int(maxWebhookBodyBytes)+1))
	request := httptest.NewRequest(stdhttp.MethodPost, "/webhook", body)
	response := httptest.NewRecorder()
	source.handleWebhook(response, request)

	require.Equal(t, stdhttp.StatusRequestEntityTooLarge, response.Code)
	require.Empty(t, source.factChan)
}

func TestHandleWebhookSetsJSONContentType(t *testing.T) {
	source, err := NewHTTPSource(&Config{SourceID: "source", AuthMethod: "none"})
	require.NoError(t, err)

	request := httptest.NewRequest(stdhttp.MethodPost, "/webhook", strings.NewReader(`{"ready":true}`))
	response := httptest.NewRecorder()
	source.handleWebhook(response, request)

	require.Equal(t, stdhttp.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
	require.Len(t, source.factChan, 1)
}

func TestStartReturnsListenerErrors(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	source, err := NewHTTPSource(&Config{
		SourceID:   "source",
		ListenPort: port,
		Path:       "/webhook",
		AuthMethod: "none",
	})
	require.NoError(t, err)

	err = source.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), strconv.Itoa(port))
	require.False(t, source.started)
}
