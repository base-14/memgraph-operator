// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
func (vm *ValidationManager) RunValidation(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string, replicationStreaming bool, healthByPod map[string]bool) (*ValidationResult, error) {
	result := &ValidationResult{
		ConnectivityPassed: true,
		ReplicationPassed:  replicationStreaming,
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
		if pod.Name != writeInstance && healthByPod != nil && !healthByPod[pod.Name] {
			iv.Healthy = false
			iv.Error = "replica is not streaming from main"
		}

		result.InstanceResults = append(result.InstanceResults, iv)
	}

	return result, nil
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
func (r *MemgraphClusterReconciler) reconcileValidation(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string, replicationStreaming bool, log *zap.Logger) error {
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
	result, err := vm.RunValidation(ctx, cluster, pods, writeInstance, replicationStreaming, r.replicationManager.ReplicaHealthByPod(cluster))
	if err != nil {
		log.Error("validation failed", zap.Error(err))
		return err
	}

	vm.UpdateValidationStatus(cluster, result)

	// Emit events and log based on health check results
	if result.ConnectivityPassed && result.ReplicationPassed {
		log.Debug("health check passed",
			zap.Int64("replicationLagMs", result.ReplicationLagMs),
			zap.Int("healthyInstances", len(result.InstanceResults)))
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonHealthCheckPassed,
			fmt.Sprintf("Health check passed: %d healthy instances", len(result.InstanceResults)))
	} else {
		log.Warn("health check failed",
			zap.Bool("connectivityPassed", result.ConnectivityPassed),
			zap.Bool("replicationPassed", result.ReplicationPassed),
			zap.Int64("replicationLagMs", result.ReplicationLagMs))
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonHealthCheckFailed,
			fmt.Sprintf("Health check failed: connectivity=%t, replication=%t, lag=%dms",
				result.ConnectivityPassed, result.ReplicationPassed, result.ReplicationLagMs))
	}

	// Emit high replication lag warning
	if result.ReplicationLagMs > 5000 {
		log.Warn("high replication lag detected",
			zap.Int64("lagMs", result.ReplicationLagMs))
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicationLagHigh,
			fmt.Sprintf("Replication lag is high: %dms", result.ReplicationLagMs))
	}

	return nil
}
