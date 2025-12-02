// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

// ValidationManager handles real-time validation tests for Memgraph clusters
type ValidationManager struct {
	client *memgraph.Client
}

// NewValidationManager creates a new ValidationManager
func NewValidationManager(client *memgraph.Client) *ValidationManager {
	return &ValidationManager{
		client: client,
	}
}

// ValidationResult contains the results of validation tests
type ValidationResult struct {
	ConnectivityPassed bool
	ReplicationPassed  bool
	ReplicationLagMs   int64
	InstanceResults    []InstanceValidation
}

// InstanceValidation contains validation results for a single instance
type InstanceValidation struct {
	Name    string
	Role    string
	Healthy bool
	LagMs   int64
	Error   string
}

// RunValidation runs all validation tests for the cluster
func (vm *ValidationManager) RunValidation(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string) (*ValidationResult, error) {
	logger := log.FromContext(ctx)
	logger.Info("Running cluster validation", "cluster", cluster.Name)

	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  true,
	}

	// Test connectivity to each instance
	for _, pod := range pods {
		if !isPodReady(&pod) {
			continue
		}

		iv := InstanceValidation{
			Name: pod.Name,
		}

		// Determine role
		if pod.Name == writeInstance {
			iv.Role = "MAIN"
		} else {
			iv.Role = "REPLICA"
		}

		// Test connectivity
		err := vm.client.Ping(ctx, cluster.Namespace, pod.Name)
		if err != nil {
			iv.Healthy = false
			iv.Error = fmt.Sprintf("ping failed: %v", err)
			result.ConnectivityPassed = false
		} else {
			iv.Healthy = true
		}

		result.InstanceResults = append(result.InstanceResults, iv)
	}

	// If we have a write instance and replicas, test replication
	if writeInstance != "" && len(pods) > 1 {
		lagMs, err := vm.testReplicationLag(ctx, cluster, pods, writeInstance)
		if err != nil {
			logger.Error(err, "Failed to test replication lag")
			result.ReplicationPassed = false
		} else {
			result.ReplicationLagMs = lagMs
			// Consider replication unhealthy if lag > 10 seconds
			if lagMs > 10000 {
				result.ReplicationPassed = false
			}
		}
	}

	return result, nil
}

// testReplicationLag tests replication by writing to main and reading from replicas
func (vm *ValidationManager) testReplicationLag(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string) (int64, error) {
	logger := log.FromContext(ctx)

	// Create a unique test value
	testValue := fmt.Sprintf("validation_%d", time.Now().UnixNano())

	// Write to main
	writeQuery := fmt.Sprintf(`
		MERGE (n:__ValidationTest {id: 'replication_test'})
		SET n.value = '%s', n.timestamp = timestamp()
		RETURN n.timestamp AS ts
	`, testValue)

	startTime := time.Now()
	output, err := vm.client.ExecuteQuery(ctx, cluster.Namespace, writeInstance, writeQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to write test value: %w", err)
	}

	// Parse the timestamp from output
	writeTimestamp := parseTimestamp(output)
	if writeTimestamp == 0 {
		writeTimestamp = time.Now().UnixMilli()
	}

	// Wait a bit for replication
	time.Sleep(100 * time.Millisecond)

	// Check each replica
	var maxLag int64
	for _, pod := range pods {
		if pod.Name == writeInstance || !isPodReady(&pod) {
			continue
		}

		// Read from replica
		readQuery := `MATCH (n:__ValidationTest {id: 'replication_test'}) RETURN n.value AS value, n.timestamp AS ts`
		output, err := vm.client.ExecuteQuery(ctx, cluster.Namespace, pod.Name, readQuery)
		if err != nil {
			logger.Error(err, "Failed to read from replica", "replica", pod.Name)
			continue
		}

		// Check if value is replicated
		if !strings.Contains(output, testValue) {
			// Value not replicated yet, calculate lag
			lag := time.Since(startTime).Milliseconds()
			if lag > maxLag {
				maxLag = lag
			}
		} else {
			// Value replicated, try to get actual lag
			readTimestamp := parseTimestamp(output)
			if readTimestamp > 0 && writeTimestamp > 0 {
				lag := readTimestamp - writeTimestamp
				if lag > maxLag {
					maxLag = lag
				}
			}
		}
	}

	// Cleanup test node (best effort)
	cleanupQuery := `MATCH (n:__ValidationTest {id: 'replication_test'}) DETACH DELETE n`
	_, _ = vm.client.ExecuteQuery(ctx, cluster.Namespace, writeInstance, cleanupQuery)

	return maxLag, nil
}

// parseTimestamp extracts a timestamp from query output
func parseTimestamp(output string) int64 {
	// Look for a numeric timestamp in the output
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip header lines and separators
		if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "|") && strings.Contains(line, "ts") {
			continue
		}
		// Try to extract number from pipe-separated output
		if strings.HasPrefix(line, "|") {
			parts := strings.Split(line, "|")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if ts, err := strconv.ParseInt(part, 10, 64); err == nil && ts > 0 {
					return ts
				}
			}
		}
	}
	return 0
}

// UpdateValidationStatus updates the cluster status with validation results
func (vm *ValidationManager) UpdateValidationStatus(cluster *memgraphv1alpha1.MemgraphCluster, result *ValidationResult) {
	now := metav1.Now()

	if cluster.Status.Validation == nil {
		cluster.Status.Validation = &memgraphv1alpha1.ValidationStatus{}
	}

	cluster.Status.Validation.LastConnectivityTest = &now
	cluster.Status.Validation.LastReplicationTest = &now
	cluster.Status.Validation.ReplicationLagMs = result.ReplicationLagMs
	cluster.Status.Validation.AllReplicasHealthy = result.ConnectivityPassed && result.ReplicationPassed

	// Update per-instance status
	cluster.Status.Validation.Instances = nil
	for _, iv := range result.InstanceResults {
		cluster.Status.Validation.Instances = append(cluster.Status.Validation.Instances,
			memgraphv1alpha1.InstanceValidationStatus{
				Name:         iv.Name,
				Role:         iv.Role,
				Healthy:      iv.Healthy,
				LagMs:        iv.LagMs,
				LastTestTime: &now,
			})
	}
}

// reconcileValidation runs validation tests periodically
func (r *MemgraphClusterReconciler) reconcileValidation(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string) error {
	logger := log.FromContext(ctx)

	// Only run validation when cluster is running
	if cluster.Status.Phase != memgraphv1alpha1.ClusterPhaseRunning {
		return nil
	}

	// Check if we should run validation (not too frequently)
	if cluster.Status.Validation != nil && cluster.Status.Validation.LastConnectivityTest != nil {
		lastTest := cluster.Status.Validation.LastConnectivityTest.Time
		if time.Since(lastTest) < 30*time.Second {
			return nil // Skip if tested recently
		}
	}

	if err := r.ensureReplicationManager(); err != nil {
		return err
	}

	vm := NewValidationManager(r.replicationManager.client)
	result, err := vm.RunValidation(ctx, cluster, pods, writeInstance)
	if err != nil {
		logger.Error(err, "Validation failed")
		return err
	}

	vm.UpdateValidationStatus(cluster, result)

	// Log validation results
	if result.ConnectivityPassed && result.ReplicationPassed {
		logger.Info("Cluster validation passed",
			"replicationLagMs", result.ReplicationLagMs,
			"healthyInstances", len(result.InstanceResults))
	} else {
		logger.Info("Cluster validation issues detected",
			"connectivityPassed", result.ConnectivityPassed,
			"replicationPassed", result.ReplicationPassed,
			"replicationLagMs", result.ReplicationLagMs)
	}

	return nil
}
