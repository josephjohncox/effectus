package main

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/stretchr/testify/require"
)

func TestMetricsListenerBindFailureIsFatal(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	server, listener, err := newMetricsServer(occupied.Addr().String())
	require.Error(t, err)
	require.Nil(t, server)
	require.Nil(t, listener)
}

func TestRecoveryExecutionObservationsPreserveMeasuredBacklog(t *testing.T) {
	observed := newHotloadMetrics()
	observed.ObserveRecovery(effectusruntime.RecoveryObservation{BacklogMeasured: true, Backlog: 7})
	observed.ObserveRecovery(effectusruntime.RecoveryObservation{ExecutionID: "execution-1", State: "completed"})
	require.Equal(t, int64(7), observed.recoveryBacklog)
}

func TestMetricsHistogramIsCumulativeExactlyOnce(t *testing.T) {
	old := metrics
	metrics = newHotloadMetrics()
	t.Cleanup(func() { metrics = old })
	observeTypecheckDuration(time.Millisecond)
	observeTypecheckDuration(20 * time.Millisecond)
	var output bytes.Buffer
	writeMetrics(&output)
	var buckets []uint64
	var count uint64
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "effectusd_rule_typecheck_duration_seconds_bucket") {
			fields := strings.Fields(line)
			value, err := strconv.ParseUint(fields[len(fields)-1], 10, 64)
			require.NoError(t, err)
			buckets = append(buckets, value)
		}
		if strings.HasPrefix(line, "effectusd_rule_typecheck_duration_seconds_count ") {
			_, err := fmt.Sscanf(line, "effectusd_rule_typecheck_duration_seconds_count %d", &count)
			require.NoError(t, err)
		}
	}
	require.NotEmpty(t, buckets)
	for index := 1; index < len(buckets); index++ {
		require.GreaterOrEqual(t, buckets[index], buckets[index-1])
	}
	require.Equal(t, count, buckets[len(buckets)-1])
}
