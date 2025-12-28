package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

type requestIDKey struct{}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func withRequestID(r *http.Request) (string, *http.Request) {
	if r == nil {
		return "", r
	}
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID, _ = generateToken()
	}
	ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
	return reqID, r.WithContext(ctx)
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(requestIDKey{}).(string); ok {
		return value
	}
	return ""
}

func logAPIRequest(r *http.Request, rec *statusRecorder, elapsed time.Duration, reqID string) {
	if r == nil || rec == nil {
		return
	}
	level := "info"
	if rec.status >= http.StatusBadRequest {
		level = "error"
	}
	entry := map[string]interface{}{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"level":      level,
		"request_id": reqID,
		"method":     r.Method,
		"path":       r.URL.Path,
		"status":     rec.status,
		"bytes":      rec.bytes,
		"latency_ms": float64(elapsed.Milliseconds()),
	}
	if reqID == "" {
		entry["request_id"] = requestIDFromContext(r.Context())
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(append(data, '\n'))
}
