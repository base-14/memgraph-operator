// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"
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

func TestMetricsRecorder_RecordReplicationHealth(t *testing.T) {
	m := NewMetricsRecorder()

	// Test healthy
	m.RecordReplicationHealth("test-cluster", "default", 50, true)

	// Test unhealthy
	m.RecordReplicationHealth("test-cluster", "default", 15000, false)
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
