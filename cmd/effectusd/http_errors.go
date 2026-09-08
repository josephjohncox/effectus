package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"net/http"

	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
)

var errHTTPBodyTooLarge = errors.New("HTTP request body exceeds 1 MiB")
var errHTTPInvalidJSON = errors.New("invalid request JSON")

func httpErrorStatus(err error) (int, string) {
	var terminal *runtime.TerminalExecutionError
	if errors.As(err, &terminal) {
		switch terminal.State {
		case schema.ExecutionFailed:
			return http.StatusUnprocessableEntity, "execution failed"
		default:
			return http.StatusConflict, "execution is blocked"
		}
	}
	switch {
	case errors.Is(err, errHTTPBodyTooLarge):
		return http.StatusRequestEntityTooLarge, "request body exceeds 1 MiB"
	case errors.Is(err, errHTTPInvalidJSON):
		return http.StatusBadRequest, "invalid request JSON"
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "execution deadline exceeded"
	case errors.Is(err, runtime.ErrInvalidExecuteRequest):
		return http.StatusBadRequest, "invalid execution request"
	case errors.Is(err, runtime.ErrIdentityConflict):
		return http.StatusConflict, "idempotency identity conflicts with an existing request"
	case errors.Is(err, runtime.ErrGenerationMismatch):
		return http.StatusConflict, "requested generation does not match"
	case errors.Is(err, schema.ErrOptimisticConflict):
		return http.StatusConflict, "execution state changed; retry the same identity"
	case errors.Is(err, runtime.ErrExecutionNotFound):
		return http.StatusNotFound, "execution is not available"
	case errors.Is(err, runtime.ErrExecutionBusy), errors.Is(err, runtime.ErrBlockedDependency), errors.Is(err, runtime.ErrGRPCUnavailable), errors.Is(err, sql.ErrConnDone), errors.Is(err, driver.ErrBadConn):
		return http.StatusServiceUnavailable, "execution dependency is unavailable"
	}
	var network net.Error
	if errors.As(err, &network) {
		return http.StatusServiceUnavailable, "execution dependency is unavailable"
	}
	return http.StatusInternalServerError, "internal execution error"
}

func allowHTTPMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	return false
}
