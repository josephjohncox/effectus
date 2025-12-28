package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

type hotloadMetrics struct {
	hotloadAttempts uint64
	hotloadFailures uint64
	ruleCompiles    uint64
	typecheckCount  uint64
	typecheckSumNs  int64
	typecheckBins   []uint64
}

var typecheckBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}

var metrics = newHotloadMetrics()

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

func writeMetrics(w io.Writer) {
	writeCounter(w, "effectusd_hotload_attempt_total", "Total hotload attempts", atomic.LoadUint64(&metrics.hotloadAttempts))
	writeCounter(w, "effectusd_hotload_failure_total", "Total hotload failures", atomic.LoadUint64(&metrics.hotloadFailures))
	writeCounter(w, "effectusd_rule_compile_total", "Total rule compiles", atomic.LoadUint64(&metrics.ruleCompiles))

	count := atomic.LoadUint64(&metrics.typecheckCount)
	sumNs := atomic.LoadInt64(&metrics.typecheckSumNs)
	writeHistogram(w, "effectusd_rule_typecheck_duration_seconds", "Rule typecheck duration", count, sumNs, metrics.typecheckBins)
}

func writeCounter(w io.Writer, name, help string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

func writeHistogram(w io.Writer, name, help string, count uint64, sumNs int64, bins []uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	var cumulative uint64
	for i, bound := range typecheckBuckets {
		if i < len(bins) {
			cumulative += atomic.LoadUint64(&bins[i])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, bound, cumulative)
	}
	if len(bins) > len(typecheckBuckets) {
		cumulative += atomic.LoadUint64(&bins[len(typecheckBuckets)])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, cumulative)
	fmt.Fprintf(w, "%s_sum %.9f\n", name, float64(sumNs)/1e9)
	fmt.Fprintf(w, "%s_count %d\n", name, count)
}

func startMetricsServer(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writeMetrics(w)
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Starting metrics server on %s\n", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("Metrics server error: %v\n", err)
	}
	fmt.Println("Shutting down metrics server")
}
