package executorhttp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func testArgumentHash(body string) string {
	var arguments any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&arguments) != nil {
		return "invalid-test-body"
	}
	_, hash, err := schema.CanonicalJSON(arguments)
	if err != nil {
		return "invalid-test-value"
	}
	return hash
}

func TestHandlerVerifiesCanonicalHashBeforeBusinessInvocation(t *testing.T) {
	canonical := testArgumentHash(`{"a":1,"b":{"x":"<tag>"}}`)
	for _, test := range []struct {
		name, body, hash string
		valid            bool
	}{
		{"canonical", `{"a":1,"b":{"x":"<tag>"}}`, canonical, true},
		{"whitespace-order-escape", `{ "b": {"x":"\u003ctag\u003e"}, "a": 1 }`, canonical, true},
		{"upper-hex", `{"a":1,"b":{"x":"<tag>"}}`, strings.ToUpper(canonical), true},
		{"changed-argument", `{"a":2,"b":{"x":"<tag>"}}`, canonical, false},
		{"wrong-digest", `{"a":1,"b":{"x":"<tag>"}}`, strings.Repeat("0", 64), false},
		{"invalid-hex", `{"a":1}`, "not-a-hash", false},
		{"missing", `{"a":1}`, "", false},
		{"null-arguments", "null", testArgumentHash("null"), false},
		{"numeric-spelling", `{"a":1.0,"b":{"x":"<tag>"}}`, canonical, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler, err := NewHandler(Options{}, func(_ context.Context, request invocation.Request) invocation.Outcome {
				calls++
				require.Equal(t, canonical, request.ArgumentHash)
				return Success(true)
			})
			require.NoError(t, err)
			request := validRequest(test.body)
			request.Header.Set(invocation.HeaderArgumentHash, test.hash)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if test.valid {
				require.Equal(t, http.StatusOK, response.Code)
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, http.StatusBadRequest, response.Code)
				require.Zero(t, calls)
			}
		})
	}
}

func TestHandlerRejectsUnrepresentableOverflowLimit(t *testing.T) {
	for _, limit := range []int64{-1, math.MaxInt64} {
		_, err := NewHandler(Options{MaxRequestBytes: limit}, func(context.Context, invocation.Request) invocation.Outcome { return Success(true) })
		require.Error(t, err)
	}
}
