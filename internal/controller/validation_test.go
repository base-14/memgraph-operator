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
