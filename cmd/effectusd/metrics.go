package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	effectusruntime "github.com/effectus/effectus-go/runtime"
)

type hotloadMetrics struct {
	hotloadAttempts  uint64
	hotloadFailures  uint64
	ruleCompiles     uint64
	listExecutions   uint64
	flowExecutions   uint64
	execFailures     uint64
	verbExecutions   uint64
	verbFailures     uint64
	typecheckCount   uint64
	typecheckSumNs   int64
	typecheckBins    []uint64
	engineExecutions uint64
	engineErrors     uint64
	recoveryErrors   uint64
	recoveryBlocked  uint64
	recoveryBacklog  int64
}

var typecheckBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}

var metrics = newHotloadMetrics()
var metricsDatabase atomic.Pointer[sql.DB]

func setMetricsDatabase(db *sql.DB) {
	metricsDatabase.Store(db)
}

func newHotloadMetrics() *hotloadMetrics {
	return &hotloadMetrics{
		typecheckBins: make([]uint64, len(typecheckBuckets)+1),
	}
}

func recordHotloadAttempt() {
	atomic.AddUint64(&metrics.hotloadAttempts, 1)
}

func recordHotloadFailure() {
	atomic.AddUint64(&metrics.hotloadFailures, 1)
}

func recordRuleCompile() {
	atomic.AddUint64(&metrics.ruleCompiles, 1)
}

func recordListExecution() {
	atomic.AddUint64(&metrics.listExecutions, 1)
}

func recordFlowExecution() {
	atomic.AddUint64(&metrics.flowExecutions, 1)
}

func recordExecutionFailure() {
	atomic.AddUint64(&metrics.execFailures, 1)
}

func recordVerbExecution() {
	atomic.AddUint64(&metrics.verbExecutions, 1)
}

func recordVerbFailure() {
	atomic.AddUint64(&metrics.verbFailures, 1)
}

func observeTypecheckDuration(d time.Duration) {
	atomic.AddUint64(&metrics.typecheckCount, 1)
	atomic.AddInt64(&metrics.typecheckSumNs, d.Nanoseconds())
	seconds := d.Seconds()
	for i, bound := range typecheckBuckets {
		if seconds <= bound {
			atomic.AddUint64(&metrics.typecheckBins[i], 1)
		}
	}
	atomic.AddUint64(&metrics.typecheckBins[len(typecheckBuckets)], 1)
}

func (m *hotloadMetrics) ObserveExecution(result effectusruntime.ExecuteResult, err error) {
	atomic.AddUint64(&m.engineExecutions, 1)
	if err != nil {
		atomic.AddUint64(&m.engineErrors, 1)
	}
}

func (m *hotloadMetrics) ObserveRecovery(observation effectusruntime.RecoveryObservation) {
	if observation.BacklogMeasured {
		atomic.StoreInt64(&m.recoveryBacklog, int64(observation.Backlog))
	}
	if observation.Err != nil {
		atomic.AddUint64(&m.recoveryErrors, 1)
	}
	if strings.HasPrefix(observation.State, "blocked_") {
		atomic.AddUint64(&m.recoveryBlocked, 1)
	}
}

func writeMetrics(w io.Writer) {
	writeCounter(w, "effectusd_hotload_attempt_total", "Total hotload attempts", atomic.LoadUint64(&metrics.hotloadAttempts))
	writeCounter(w, "effectusd_hotload_failure_total", "Total hotload failures", atomic.LoadUint64(&metrics.hotloadFailures))
	writeCounter(w, "effectusd_rule_compile_total", "Total rule compiles", atomic.LoadUint64(&metrics.ruleCompiles))
	writeCounter(w, "effectusd_list_execution_total", "Total list rule executions", atomic.LoadUint64(&metrics.listExecutions))
	writeCounter(w, "effectusd_flow_execution_total", "Total flow executions", atomic.LoadUint64(&metrics.flowExecutions))
	writeCounter(w, "effectusd_execution_failure_total", "Total rule/flow execution failures", atomic.LoadUint64(&metrics.execFailures))
	writeCounter(w, "effectusd_verb_execution_total", "Total verb executions", atomic.LoadUint64(&metrics.verbExecutions))
	writeCounter(w, "effectusd_verb_failure_total", "Total verb execution failures", atomic.LoadUint64(&metrics.verbFailures))
	writeCounter(w, "effectusd_checked_execution_total", "Total checked engine executions", atomic.LoadUint64(&metrics.engineExecutions))
	writeCounter(w, "effectusd_checked_execution_error_total", "Total checked engine errors", atomic.LoadUint64(&metrics.engineErrors))
	writeCounter(w, "effectusd_recovery_error_total", "Total per-execution recovery errors", atomic.LoadUint64(&metrics.recoveryErrors))
	writeCounter(w, "effectusd_recovery_blocked_total", "Total blocked recovery dispositions", atomic.LoadUint64(&metrics.recoveryBlocked))
	fmt.Fprintf(w, "# TYPE effectusd_recovery_backlog gauge\neffectusd_recovery_backlog %d\n", atomic.LoadInt64(&metrics.recoveryBacklog))
	count := atomic.LoadUint64(&metrics.typecheckCount)
	sumNs := atomic.LoadInt64(&metrics.typecheckSumNs)
	writeHistogram(w, "effectusd_rule_typecheck_duration_seconds", "Rule typecheck duration", count, sumNs, metrics.typecheckBins)
	if db := metricsDatabase.Load(); db != nil {
		stats := db.Stats()
		writeGauge(w, "effectusd_database_open_connections", "Open PostgreSQL connections", int64(stats.OpenConnections))
		writeGauge(w, "effectusd_database_in_use_connections", "In-use PostgreSQL connections", int64(stats.InUse))
		writeGauge(w, "effectusd_database_idle_connections", "Idle PostgreSQL connections", int64(stats.Idle))
		writeCounter(w, "effectusd_database_wait_total", "PostgreSQL pool waits", uint64(stats.WaitCount))
		writeFloatCounter(w, "effectusd_database_wait_duration_seconds", "Cumulative PostgreSQL pool wait duration", stats.WaitDuration.Seconds())
		writeGauge(w, "effectusd_database_max_open_connections", "Configured PostgreSQL connection limit", int64(stats.MaxOpenConnections))
	}
}

func writeGauge(w io.Writer, name, help string, value int64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

func writeFloatCounter(w io.Writer, name, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %.9f\n", name, value)
}

func writeCounter(w io.Writer, name, help string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

func writeHistogram(w io.Writer, name, help string, count uint64, sumNs int64, bins []uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	for i, bound := range typecheckBuckets {
		value := uint64(0)
		if i < len(bins) {
			value = atomic.LoadUint64(&bins[i])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, bound, value)
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, count)
	fmt.Fprintf(w, "%s_sum %.9f\n", name, float64(sumNs)/1e9)
	fmt.Fprintf(w, "%s_count %d\n", name, count)
}

func newMetricsServer(addr string) (*http.Server, net.Listener, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		reqID, _ := withRequestID(r)
		if reqID != "" {
			w.Header().Set("X-Request-ID", reqID)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writeMetrics(w)
	})

	server := &http.Server{Addr: addr, Handler: mux}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on metrics address %s: %w", addr, err)
	}
	return server, listener, nil
}

func serveMetricsServer(ctx context.Context, server *http.Server, listener net.Listener) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Starting metrics server on %s\n", listener.Addr())
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve metrics: %w", err)
	}
	return nil
}
