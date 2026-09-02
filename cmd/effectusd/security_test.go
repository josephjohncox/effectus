package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTokenMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := httpTokenMiddleware("test-token", next)

	for name, authorization := range map[string]string{
		"missing":       "",
		"wrong scheme":  "Basic test-token",
		"wrong token":   "Bearer wrong-token",
		"correct token": "Bearer test-token",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := http.StatusUnauthorized
			if name == "correct token" {
				want = http.StatusNoContent
			}
			if response.Code != want {
				t.Fatalf("status = %d, want %d", response.Code, want)
			}
		})
	}
}

func TestConstantTimeTokenEqualRejectsEmptyAndMismatchedTokens(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"token", ""}, {"token", "other"}} {
		if constantTimeTokenEqual(pair[0], pair[1]) {
			t.Fatalf("constantTimeTokenEqual(%q, %q) = true", pair[0], pair[1])
		}
	}
	if !constantTimeTokenEqual("token", "token") {
		t.Fatal("matching token was rejected")
	}
}
