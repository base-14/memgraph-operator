// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

func TestMetricsRecorder_RecordClusterPhase(t *testing.T) {
	m := NewMetricsRecorder()

	tests := []struct {
		phase    string
		expected float64
	}{
		{"Pending", 0},
		{"Initializing", 1},
		{"Running", 2},
		{"Failed", 3},
		{"Unknown", 0}, // Unknown defaults to 0
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			// Just verify no panic occurs
			m.RecordClusterPhase("test-cluster", "default", tt.phase)
		})
	}
}

func TestMetricsRecorder_RecordClusterInstances(t *testing.T) {
	m := NewMetricsRecorder()

	// Just verify no panic occurs
	m.RecordClusterInstances("test-cluster", "default", 3, 5, 2)
}

func TestMetricsRecorder_RecordReplicationLag(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordReplicationLag("test-cluster", "default", 50)
	if got := testutil.ToFloat64(replicationLagGauge.WithLabelValues("test-cluster", "default")); got != 50 {
		t.Errorf("replication lag = %v, want 50", got)
	}

	m.RecordReplicationLag("test-cluster", "default", 15000)
	if got := testutil.ToFloat64(replicationLagGauge.WithLabelValues("test-cluster", "default")); got != 15000 {
		t.Errorf("replication lag = %v, want 15000", got)
	}
}

const testMetricsNamespace = "default"

func TestMetricsRecorder_RecordReplicaSet(t *testing.T) {
	m := NewMetricsRecorder()
	cluster, namespace := "replica-set-cluster", testMetricsNamespace

	healthy := memgraph.ReplicaInfo{
		Name:      "replica_1",
		Status:    memgraph.ReplicaStatusReady,
		Behind:    0,
		Timestamp: 42,
		DataInfo:  map[string]memgraph.ReplicaDBInfo{"memgraph": {Status: "ready", Timestamp: 42}},
	}
	// The silent-desync shape: registered, heartbeating, but data_info empty
	desynced := memgraph.ReplicaInfo{
		Name:   "replica_0",
		Status: memgraph.ReplicaStatusInvalid,
	}

	m.RecordReplicaSet(cluster, namespace, []memgraph.ReplicaInfo{healthy, desynced})

	assertGauge := func(name string, got, want float64) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	assertGauge("replicas_total", testutil.ToFloat64(clusterReplicasTotalGauge.WithLabelValues(cluster, namespace)), 2)
	assertGauge("replicas_healthy_total", testutil.ToFloat64(clusterReplicasHealthyTotalGauge.WithLabelValues(cluster, namespace)), 1)
	assertGauge("replication_healthy", testutil.ToFloat64(replicationHealthyGauge.WithLabelValues(cluster, namespace)), 0)

	assertGauge("replica_1 healthy", testutil.ToFloat64(replicaHealthyGauge.WithLabelValues(cluster, namespace, "replica_1")), 1)
	assertGauge("replica_1 ts", testutil.ToFloat64(replicaLastConfirmedTimestampGauge.WithLabelValues(cluster, namespace, "replica_1")), 42)
	assertGauge("replica_1 data_info_present", testutil.ToFloat64(replicaDataInfoPresentGauge.WithLabelValues(cluster, namespace, "replica_1")), 1)
	assertGauge("replica_1 status ready", testutil.ToFloat64(replicaStatusGauge.WithLabelValues(cluster, namespace, "replica_1", "ready")), 1)
	assertGauge("replica_1 status invalid", testutil.ToFloat64(replicaStatusGauge.WithLabelValues(cluster, namespace, "replica_1", "invalid")), 0)

	assertGauge("replica_0 healthy", testutil.ToFloat64(replicaHealthyGauge.WithLabelValues(cluster, namespace, "replica_0")), 0)
	assertGauge("replica_0 ts", testutil.ToFloat64(replicaLastConfirmedTimestampGauge.WithLabelValues(cluster, namespace, "replica_0")), 0)
	assertGauge("replica_0 data_info_present", testutil.ToFloat64(replicaDataInfoPresentGauge.WithLabelValues(cluster, namespace, "replica_0")), 0)
	assertGauge("replica_0 status invalid", testutil.ToFloat64(replicaStatusGauge.WithLabelValues(cluster, namespace, "replica_0", "invalid")), 1)

	// Re-record with replica_0 gone: its series must disappear
	m.RecordReplicaSet(cluster, namespace, []memgraph.ReplicaInfo{healthy})

	assertGauge("replicas_total after removal", testutil.ToFloat64(clusterReplicasTotalGauge.WithLabelValues(cluster, namespace)), 1)
	assertGauge("replication_healthy after removal", testutil.ToFloat64(replicationHealthyGauge.WithLabelValues(cluster, namespace)), 1)
	if got := testutil.CollectAndCount(replicaHealthyGauge); got != 1 {
		t.Errorf("replicaHealthyGauge series count after removal = %d, want 1", got)
	}

	// All replicas gone: rollups report zero, vacuously healthy
	m.RecordReplicaSet(cluster, namespace, nil)
	assertGauge("replicas_total empty", testutil.ToFloat64(clusterReplicasTotalGauge.WithLabelValues(cluster, namespace)), 0)
	assertGauge("replication_healthy empty", testutil.ToFloat64(replicationHealthyGauge.WithLabelValues(cluster, namespace)), 1)
	if got := testutil.CollectAndCount(replicaHealthyGauge); got != 0 {
		t.Errorf("replicaHealthyGauge series count when empty = %d, want 0", got)
	}

	m.DeleteClusterMetrics(cluster, namespace)
}

func TestMetricsRecorder_RecordReplicationDrift(t *testing.T) {
	m := NewMetricsRecorder()
	cluster, namespace := "drift-cluster", testMetricsNamespace

	m.RecordReplicationDrift(cluster, namespace, "replica_0", 3151, 9000)

	if got := testutil.ToFloat64(replicationVertexDriftGauge.WithLabelValues(cluster, namespace, "replica_0")); got != 3151 {
		t.Errorf("vertex drift = %v, want 3151", got)
	}
	if got := testutil.ToFloat64(replicationEdgeDriftGauge.WithLabelValues(cluster, namespace, "replica_0")); got != 9000 {
		t.Errorf("edge drift = %v, want 9000", got)
	}

	m.DeleteReplicationDriftMetrics(cluster, namespace)
	if got := testutil.CollectAndCount(replicationVertexDriftGauge); got != 0 {
		t.Errorf("vertex drift series count after delete = %d, want 0", got)
	}
}

func TestMetricsRecorder_DeleteClusterMetricsSweepsReplicaSeries(t *testing.T) {
	m := NewMetricsRecorder()
	cluster, namespace := "sweep-cluster", testMetricsNamespace

	m.RecordReplicaSet(cluster, namespace, []memgraph.ReplicaInfo{{Name: "replica_0", Status: memgraph.ReplicaStatusInvalid}})
	m.RecordReplicationDrift(cluster, namespace, "replica_0", 1, 2)

	m.DeleteClusterMetrics(cluster, namespace)

	for name, count := range map[string]int{
		"replicaHealthyGauge":         testutil.CollectAndCount(replicaHealthyGauge),
		"replicaStatusGauge":          testutil.CollectAndCount(replicaStatusGauge),
		"replicaBehindGauge":          testutil.CollectAndCount(replicaBehindGauge),
		"replicaLastConfirmedTsGauge": testutil.CollectAndCount(replicaLastConfirmedTimestampGauge),
		"replicaDataInfoPresentGauge": testutil.CollectAndCount(replicaDataInfoPresentGauge),
		"replicationVertexDriftGauge": testutil.CollectAndCount(replicationVertexDriftGauge),
		"replicationEdgeDriftGauge":   testutil.CollectAndCount(replicationEdgeDriftGauge),
		"clusterReplicasTotalGauge":   testutil.CollectAndCount(clusterReplicasTotalGauge),
		"clusterReplicasHealthyGauge": testutil.CollectAndCount(clusterReplicasHealthyTotalGauge),
	} {
		if count != 0 {
			t.Errorf("%s has %d series after DeleteClusterMetrics, want 0", name, count)
		}
	}
}

func TestMetricsRecorder_RecordInstanceHealth(t *testing.T) {
	m := NewMetricsRecorder()

	// Test healthy main
	m.RecordInstanceHealth("test-cluster", "default", "cluster-0", "MAIN", true)

	// Test healthy replica
	m.RecordInstanceHealth("test-cluster", "default", "cluster-1", "REPLICA", true)

	// Test unhealthy replica
	m.RecordInstanceHealth("test-cluster", "default", "cluster-2", "REPLICA", false)
}

func TestMetricsRecorder_RecordReconcileOperation(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordReconcileOperation("test-cluster", "default", "success")
	m.RecordReconcileOperation("test-cluster", "default", "error")
}

func TestMetricsRecorder_RecordReconcileDuration(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordReconcileDuration("test-cluster", "default", 0.5)
	m.RecordReconcileDuration("test-cluster", "default", 2.5)
}

func TestMetricsRecorder_RecordSnapshotSuccess(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordSnapshotSuccess("test-cluster", "default", 1733152800.0)
}

func TestMetricsRecorder_RecordSnapshotFailure(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordSnapshotFailure("test-cluster", "default")
}

func TestMetricsRecorder_RecordFailoverEvent(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordFailoverEvent("test-cluster", "default", "cluster-0", "cluster-1")
}

func TestMetricsRecorder_RecordValidation(t *testing.T) {
	m := NewMetricsRecorder()

	m.RecordValidation("test-cluster", "default", 1733152800.0, true)
	m.RecordValidation("test-cluster", "default", 1733152900.0, false)
}

func TestMetricsRecorder_DeleteClusterMetrics(t *testing.T) {
	m := NewMetricsRecorder()

	// Record some metrics first
	m.RecordClusterPhase("test-cluster", "default", "Running")
	m.RecordClusterInstances("test-cluster", "default", 3, 3, 2)

	// Delete should not panic
	m.DeleteClusterMetrics("test-cluster", "default")
}

func TestMetricsRecorder_RecordStorageInfo(t *testing.T) {
	m := NewMetricsRecorder()

	// Test with nil storage info - should not panic
	m.RecordStorageInfo("test-cluster", "default", "pod-0", "main", nil)

	// Test with valid storage info
	info := &StorageInfo{
		Name:            "default",
		VertexCount:     1000,
		EdgeCount:       5000,
		AverageDegree:   10.0,
		MemoryRes:       512 * 1024 * 1024,
		PeakMemoryRes:   1024 * 1024 * 1024,
		DiskUsage:       256 * 1024 * 1024,
		MemoryTracked:   128 * 1024 * 1024,
		AllocationLimit: 2 * 1024 * 1024 * 1024,
	}
	m.RecordStorageInfo("test-cluster", "default", "pod-0", "main", info)
}

func TestMetricsRecorder_DeleteInstanceStorageMetrics(t *testing.T) {
	m := NewMetricsRecorder()

	// Record some storage metrics first
	info := &StorageInfo{
		VertexCount: 100,
		EdgeCount:   200,
	}
	m.RecordStorageInfo("test-cluster", "default", "pod-0", "main", info)

	// Delete should not panic
	m.DeleteInstanceStorageMetrics("test-cluster", "default", "pod-0", "main")
}

// StorageInfo is defined in memgraph package, adding helper type for test
type StorageInfo = memgraph.StorageInfo
