// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

// ReplicationManager handles Memgraph replication configuration
type ReplicationManager struct {
	client *memgraph.Client
}

// NewReplicationManager creates a new ReplicationManager
func NewReplicationManager(client *memgraph.Client) *ReplicationManager {
	return &ReplicationManager{
		client: client,
	}
}

// ConfigureReplication sets up replication for the cluster
// It configures the write instance as MAIN and all other instances as REPLICAs
func (rm *ReplicationManager) ConfigureReplication(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string) error {
	logger := log.FromContext(ctx)

	if writeInstance == "" {
		logger.Info("No write instance available, skipping replication configuration")
		return nil
	}

	// Get ready pods sorted by name for consistent ordering
	readyPods := rm.getReadyPodsSorted(pods)
	if len(readyPods) == 0 {
		logger.Info("No ready pods available")
		return nil
	}

	// Get the replication mode from spec
	mode := string(cluster.Spec.Replication.Mode)
	if mode == "" {
		mode = "ASYNC"
	}

	// Check and configure the MAIN instance
	if err := rm.ensureMainRole(ctx, cluster.Namespace, writeInstance); err != nil {
		return fmt.Errorf("failed to ensure main role on %s: %w", writeInstance, err)
	}

	// Get current registered replicas from MAIN
	currentReplicas, err := rm.client.ShowReplicas(ctx, cluster.Namespace, writeInstance)
	if err != nil {
		// If we can't get replicas, the main might not be configured yet
		logger.Info("Could not get current replicas, will proceed with configuration", "error", err)
		currentReplicas = nil
	}

	// Build set of current replica names
	currentReplicaNames := make(map[string]bool)
	for _, r := range currentReplicas {
		currentReplicaNames[r.Name] = true
	}

	// Configure each replica
	var registeredCount int32
	for _, pod := range readyPods {
		if pod.Name == writeInstance {
			continue // Skip the main instance
		}

		replicaName := rm.getReplicaName(pod.Name)
		replicaHost := getPodFQDN(pod.Name, cluster)

		// Ensure replica role
		if err := rm.ensureReplicaRole(ctx, cluster.Namespace, pod.Name); err != nil {
			logger.Error(err, "Failed to set replica role", "pod", pod.Name)
			continue
		}

		// Register with main if not already registered
		if !currentReplicaNames[replicaName] {
			logger.Info("Registering replica with main",
				"replica", replicaName,
				"host", replicaHost,
				"mode", mode)

			if err := rm.client.RegisterReplica(ctx, cluster.Namespace, writeInstance, replicaName, replicaHost, mode); err != nil {
				logger.Error(err, "Failed to register replica", "replica", replicaName)
				continue
			}
		}

		registeredCount++
	}

	// Clean up stale replicas (pods that no longer exist)
	rm.cleanupStaleReplicas(ctx, cluster, writeInstance, readyPods, currentReplicas)

	logger.Info("Replication configured",
		"main", writeInstance,
		"registeredReplicas", registeredCount)

	return nil
}

// ensureMainRole ensures the given pod is configured as MAIN
func (rm *ReplicationManager) ensureMainRole(ctx context.Context, namespace, podName string) error {
	logger := log.FromContext(ctx)

	// Check current role
	currentRole, err := rm.client.GetReplicationRole(ctx, namespace, podName)
	if err != nil {
		// If we can't get the role, try to set it
		logger.Info("Could not get current role, will set to MAIN", "pod", podName, "error", err)
	}

	if currentRole == "MAIN" {
		return nil
	}

	logger.Info("Setting replication role to MAIN", "pod", podName, "currentRole", currentRole)
	return rm.client.SetReplicationRole(ctx, namespace, podName, true)
}

// ensureReplicaRole ensures the given pod is configured as REPLICA
func (rm *ReplicationManager) ensureReplicaRole(ctx context.Context, namespace, podName string) error {
	logger := log.FromContext(ctx)

	// Check current role
	currentRole, err := rm.client.GetReplicationRole(ctx, namespace, podName)
	if err != nil {
		logger.Info("Could not get current role, will set to REPLICA", "pod", podName, "error", err)
	}

	if currentRole == "REPLICA" {
		return nil
	}

	logger.Info("Setting replication role to REPLICA", "pod", podName, "currentRole", currentRole)
	return rm.client.SetReplicationRole(ctx, namespace, podName, false)
}

// cleanupStaleReplicas removes replicas from MAIN that no longer exist as pods
func (rm *ReplicationManager) cleanupStaleReplicas(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, mainPod string, readyPods []corev1.Pod, currentReplicas []memgraph.ReplicaInfo) {
	logger := log.FromContext(ctx)

	// Build set of expected replica names
	expectedReplicas := make(map[string]bool)
	for _, pod := range readyPods {
		if pod.Name != mainPod {
			expectedReplicas[rm.getReplicaName(pod.Name)] = true
		}
	}

	// Unregister any replicas that shouldn't exist
	for _, replica := range currentReplicas {
		if !expectedReplicas[replica.Name] {
			logger.Info("Unregistering stale replica", "replica", replica.Name)
			if err := rm.client.UnregisterReplica(ctx, cluster.Namespace, mainPod, replica.Name); err != nil {
				logger.Error(err, "Failed to unregister stale replica", "replica", replica.Name)
			}
		}
	}
}

// CheckReplicationHealth checks if all replicas are healthy and in sync
func (rm *ReplicationManager) CheckReplicationHealth(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, writeInstance string) (*memgraphv1alpha1.ReplicationHealth, error) {
	logger := log.FromContext(ctx)

	if writeInstance == "" {
		return nil, fmt.Errorf("no write instance specified")
	}

	// Get registered replicas
	replicas, err := rm.client.ShowReplicas(ctx, cluster.Namespace, writeInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to show replicas: %w", err)
	}

	health := &memgraphv1alpha1.ReplicationHealth{
		TotalReplicas:   int32(len(replicas)),
		HealthyReplicas: 0,
	}

	for _, replica := range replicas {
		// Status can be "ready", "replicating", "recovery", "invalid"
		status := strings.ToLower(replica.Status)
		if status == "ready" || status == "replicating" {
			health.HealthyReplicas++
		} else {
			logger.Info("Unhealthy replica detected",
				"replica", replica.Name,
				"status", replica.Status)
		}
	}

	return health, nil
}

// HandleMainFailover handles failover when the main instance becomes unavailable
// This is called when HighAvailability is enabled and the main is detected as down
func (rm *ReplicationManager) HandleMainFailover(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, failedMain string) (string, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.HighAvailability == nil || !cluster.Spec.HighAvailability.Enabled {
		return "", fmt.Errorf("high availability is not enabled")
	}

	logger.Info("Initiating failover from failed main", "failedMain", failedMain)

	// Get ready pods excluding the failed main
	var candidates []corev1.Pod
	for _, pod := range pods {
		if pod.Name != failedMain && isPodReady(&pod) {
			candidates = append(candidates, pod)
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no healthy candidates available for failover")
	}

	// Check if preferred main is available
	if cluster.Spec.HighAvailability.PreferredMain != "" {
		for _, pod := range candidates {
			if pod.Name == cluster.Spec.HighAvailability.PreferredMain {
				logger.Info("Promoting preferred main", "pod", pod.Name)
				return pod.Name, nil
			}
		}
	}

	// Otherwise, promote the first healthy replica
	newMain := candidates[0].Name
	logger.Info("Promoting replica to main", "pod", newMain)

	return newMain, nil
}

// getReadyPodsSorted returns ready pods sorted by name
func (rm *ReplicationManager) getReadyPodsSorted(pods []corev1.Pod) []corev1.Pod {
	var ready []corev1.Pod
	for _, pod := range pods {
		if isPodReady(&pod) {
			ready = append(ready, pod)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		return ready[i].Name < ready[j].Name
	})
	return ready
}

// getReplicaName returns the Memgraph replica name for a pod
func (rm *ReplicationManager) getReplicaName(podName string) string {
	// Use pod name with underscores instead of hyphens (Memgraph naming requirement)
	return strings.ReplaceAll(podName, "-", "_")
}

// getPodFQDN returns the fully qualified domain name for a pod
// Uses the headless service name from the cluster spec
func getPodFQDN(podName string, cluster *memgraphv1alpha1.MemgraphCluster) string {
	// Format: pod-name.headless-service.namespace.svc.cluster.local
	headlessService := headlessServiceName(cluster)
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, headlessService, cluster.Namespace)
}
