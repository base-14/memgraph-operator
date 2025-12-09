// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const (
	roleMain    = "MAIN"
	roleReplica = "REPLICA"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int64
	}{
		{
			name:     "Empty output",
			output:   "",
			expected: 0,
		},
		{
			name: "Valid timestamp in pipe format",
			output: `+---------------+
| ts            |
+---------------+
| 1733152800000 |
+---------------+`,
			expected: 1733152800000,
		},
		{
			name: "No timestamp",
			output: `+-------+
| value |
+-------+
| test  |
+-------+`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimestamp(tt.output)
			if result != tt.expected {
				t.Errorf("parseTimestamp() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestValidationManager_UpdateValidationStatus(t *testing.T) {
	vm := &ValidationManager{}

	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  true,
		ReplicationLagMs:   50,
		InstanceResults: []InstanceValidation{
			{Name: "cluster-0", Role: roleMain, Healthy: true},
			{Name: "cluster-1", Role: roleReplica, Healthy: true, LagMs: 50},
			{Name: "cluster-2", Role: roleReplica, Healthy: true, LagMs: 30},
		},
	}

	vm.UpdateValidationStatus(cluster, result)

	// Verify status was updated
	if cluster.Status.Validation == nil {
		t.Fatal("Validation status should not be nil")
	}

	if cluster.Status.Validation.ReplicationLagMs != 50 {
		t.Errorf("expected ReplicationLagMs 50, got %d", cluster.Status.Validation.ReplicationLagMs)
	}

	if !cluster.Status.Validation.AllReplicasHealthy {
		t.Error("expected AllReplicasHealthy to be true")
	}

	if len(cluster.Status.Validation.Instances) != 3 {
		t.Errorf("expected 3 instances, got %d", len(cluster.Status.Validation.Instances))
	}

	// Verify instance details
	for _, inst := range cluster.Status.Validation.Instances {
		if inst.Name == "cluster-0" && inst.Role != roleMain {
			t.Errorf("expected cluster-0 to have role MAIN, got %s", inst.Role)
		}
		if inst.Name == "cluster-1" && inst.Role != roleReplica {
			t.Errorf("expected cluster-1 to have role REPLICA, got %s", inst.Role)
		}
	}
}

func TestValidationManager_UpdateValidationStatus_Unhealthy(t *testing.T) {
	vm := &ValidationManager{}

	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	result := &ValidationResult{
		ConnectivityPassed: false,
		ReplicationPassed:  true,
		ReplicationLagMs:   100,
		InstanceResults: []InstanceValidation{
			{Name: "cluster-0", Role: roleMain, Healthy: true},
			{Name: "cluster-1", Role: roleReplica, Healthy: false, Error: "connection refused"},
		},
	}

	vm.UpdateValidationStatus(cluster, result)

	if cluster.Status.Validation.AllReplicasHealthy {
		t.Error("expected AllReplicasHealthy to be false when connectivity failed")
	}
}

func TestValidationManager_UpdateValidationStatus_ReplicationFailed(t *testing.T) {
	vm := &ValidationManager{}

	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  false,
		ReplicationLagMs:   5000,
		InstanceResults: []InstanceValidation{
			{Name: "cluster-0", Role: roleMain, Healthy: true},
		},
	}

	vm.UpdateValidationStatus(cluster, result)

	if cluster.Status.Validation.AllReplicasHealthy {
		t.Error("expected AllReplicasHealthy to be false when replication failed")
	}
}

func TestValidationManager_UpdateValidationStatus_EmptyResult(t *testing.T) {
	vm := &ValidationManager{}

	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  true,
		InstanceResults:    []InstanceValidation{},
	}

	vm.UpdateValidationStatus(cluster, result)

	if cluster.Status.Validation == nil {
		t.Fatal("Validation status should not be nil")
	}

	if len(cluster.Status.Validation.Instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(cluster.Status.Validation.Instances))
	}
}

func TestParseTimestampVariants(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int64
	}{
		{
			name: "whitespace around value",
			output: `+---------------+
| ts            |
+---------------+
|   1733152800  |
+---------------+`,
			expected: 1733152800,
		},
		{
			name:     "just a number",
			output:   "1733152800000",
			expected: 0, // Not in expected format
		},
		{
			name: "header only",
			output: `+---------------+
| ts            |
+---------------+`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimestamp(tt.output)
			if result != tt.expected {
				t.Errorf("parseTimestamp() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestNewValidationManager(t *testing.T) {
	vm := NewValidationManager(nil)
	if vm == nil {
		t.Error("NewValidationManager returned nil")
	}
}

func TestValidationResult(t *testing.T) {
	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  false,
		ReplicationLagMs:   5000,
		InstanceResults: []InstanceValidation{
			{Name: "pod-0", Role: "MAIN", Healthy: true},
			{Name: "pod-1", Role: "REPLICA", Healthy: false, Error: "connection refused"},
		},
	}

	if result.ConnectivityPassed != true {
		t.Error("ConnectivityPassed should be true")
	}

	if result.ReplicationPassed != false {
		t.Error("ReplicationPassed should be false")
	}

	if len(result.InstanceResults) != 2 {
		t.Errorf("Expected 2 instance results, got %d", len(result.InstanceResults))
	}
}

func TestInstanceValidation(t *testing.T) {
	tests := []struct {
		name     string
		iv       InstanceValidation
		expected bool
	}{
		{
			name: "healthy main",
			iv: InstanceValidation{
				Name:    "cluster-0",
				Role:    "MAIN",
				Healthy: true,
			},
			expected: true,
		},
		{
			name: "unhealthy replica with error",
			iv: InstanceValidation{
				Name:    "cluster-1",
				Role:    "REPLICA",
				Healthy: false,
				Error:   "timeout",
			},
			expected: false,
		},
		{
			name: "healthy replica with lag",
			iv: InstanceValidation{
				Name:    "cluster-2",
				Role:    "REPLICA",
				Healthy: true,
				LagMs:   100,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.iv.Healthy != tt.expected {
				t.Errorf("Healthy = %v, want %v", tt.iv.Healthy, tt.expected)
			}
		})
	}
}

func TestParseTimestampWithComplexOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int64
	}{
		{
			name: "multiple columns",
			output: `+--------+---------------+
| value  | ts            |
+--------+---------------+
| test   | 1733152800000 |
+--------+---------------+`,
			expected: 1733152800000,
		},
		{
			name: "single value",
			output: `+---------------+
| ts            |
+---------------+
| 9999999999999 |
+---------------+`,
			expected: 9999999999999,
		},
		{
			name:     "empty pipes",
			output:   "| | |",
			expected: 0,
		},
		{
			name: "negative number",
			output: `+------+
| ts   |
+------+
| -100 |
+------+`,
			expected: 0, // Negative should be ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimestamp(tt.output)
			if result != tt.expected {
				t.Errorf("parseTimestamp() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestValidationManager_UpdateValidationStatus_WithLag(t *testing.T) {
	vm := &ValidationManager{}

	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  true,
		ReplicationLagMs:   250,
		InstanceResults: []InstanceValidation{
			{Name: "cluster-0", Role: "MAIN", Healthy: true, LagMs: 0},
			{Name: "cluster-1", Role: "REPLICA", Healthy: true, LagMs: 100},
			{Name: "cluster-2", Role: "REPLICA", Healthy: true, LagMs: 250},
		},
	}

	vm.UpdateValidationStatus(cluster, result)

	// Verify lag is recorded
	if cluster.Status.Validation.ReplicationLagMs != 250 {
		t.Errorf("ReplicationLagMs = %d, want 250", cluster.Status.Validation.ReplicationLagMs)
	}

	// Verify timestamps are set
	if cluster.Status.Validation.LastConnectivityTest == nil {
		t.Error("LastConnectivityTest should not be nil")
	}

	if cluster.Status.Validation.LastReplicationTest == nil {
		t.Error("LastReplicationTest should not be nil")
	}
}
