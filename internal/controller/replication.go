// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

// ReplicationManager handles Memgraph replication configuration
type ReplicationManager struct {
	client   *memgraph.Client
	recorder record.EventRecorder
}

// NewReplicationManager creates a new ReplicationManager
func NewReplicationManager(client *memgraph.Client, recorder record.EventRecorder) *ReplicationManager {
	return &ReplicationManager{
		client:   client,
		recorder: recorder,
	}
}

// Client returns the underlying memgraph client
func (rm *ReplicationManager) Client() *memgraph.Client {
	return rm.client
}

// ConfigureReplication sets up replication for the cluster
// It configures the write instance as MAIN and all other instances as REPLICAs
func (rm *ReplicationManager) ConfigureReplication(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string, log *zap.Logger) error {
	if writeInstance == "" {
		return nil
	}

	// Get ready pods sorted by name for consistent ordering
	readyPods := rm.getReadyPodsSorted(pods)
	if len(readyPods) == 0 {
		return nil
	}

	// Get the replication mode from spec
	mode := string(cluster.Spec.Replication.Mode)
	if mode == "" {
		mode = "ASYNC"
	}

	// Check and configure the MAIN instance
	if err := rm.ensureMainRole(ctx, cluster, writeInstance, log); err != nil {
		return fmt.Errorf("failed to ensure main role on %s: %w", writeInstance, err)
	}

	// Get current registered replicas from MAIN
	currentReplicas, err := rm.client.ShowReplicas(ctx, cluster.Namespace, writeInstance)
	if err != nil {
		log.Debug("could not get current replicas, proceeding with configuration",
			zap.String("mainInstance", writeInstance),
			zap.Error(err))
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
		if err := rm.ensureReplicaRole(ctx, cluster.Namespace, pod.Name, log); err != nil {
			log.Error("failed to set replica role",
				zap.String("pod", pod.Name),
				zap.Error(err))
			continue
		}

		// Register with main if not already registered
		if !currentReplicaNames[replicaName] {
			log.Info("registering replica with main",
				zap.String("replica", replicaName),
				zap.String("host", replicaHost),
				zap.String("mode", mode),
				zap.String("mainInstance", writeInstance))

			if err := rm.client.RegisterReplica(ctx, cluster.Namespace, writeInstance, replicaName, replicaHost, mode); err != nil {
				log.Error("failed to register replica",
					zap.String("replica", replicaName),
					zap.Error(err))
				continue
			}
			rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReplicaRegistered,
				fmt.Sprintf("Replica %s registered with main %s", replicaName, writeInstance))
		}

		registeredCount++
	}

	// Clean up stale replicas (pods that no longer exist)
	rm.cleanupStaleReplicas(ctx, cluster, writeInstance, readyPods, currentReplicas, log)

	log.Info("replication configured",
		zap.String("mainInstance", writeInstance),
		zap.Int32("registeredReplicas", registeredCount),
		zap.String("mode", mode))

	return nil
}

// ensureMainRole ensures the given pod is configured as MAIN
func (rm *ReplicationManager) ensureMainRole(
	ctx context.Context,
	cluster *memgraphv1alpha1.MemgraphCluster,
	podName string,
	log *zap.Logger,
) error {
	// Check current role
	currentRole, err := rm.client.GetReplicationRole(ctx, cluster.Namespace, podName)
	if err != nil {
		log.Debug("could not get current role, will set to MAIN",
			zap.String("pod", podName),
			zap.Error(err))
	}

	if currentRole == "MAIN" {
		return nil
	}

	log.Info("setting replication role to MAIN",
		zap.String("pod", podName),
		zap.String("previousRole", currentRole))
	if err := rm.client.SetReplicationRole(ctx, cluster.Namespace, podName, true); err != nil {
		return err
	}
	rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonMainInstanceConfigured,
		fmt.Sprintf("Instance %s configured as MAIN", podName))
	return nil
}

// ensureReplicaRole ensures the given pod is configured as REPLICA
func (rm *ReplicationManager) ensureReplicaRole(ctx context.Context, namespace, podName string, log *zap.Logger) error {
	// Check current role
	currentRole, err := rm.client.GetReplicationRole(ctx, namespace, podName)
	if err != nil {
		log.Debug("could not get current role, will set to REPLICA",
			zap.String("pod", podName),
			zap.Error(err))
	}

	if currentRole == "REPLICA" {
		return nil
	}

	log.Info("setting replication role to REPLICA",
		zap.String("pod", podName),
		zap.String("previousRole", currentRole))
	return rm.client.SetReplicationRole(ctx, namespace, podName, false)
}

// cleanupStaleReplicas removes replicas from MAIN that no longer exist as pods
func (rm *ReplicationManager) cleanupStaleReplicas(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, mainPod string, readyPods []corev1.Pod, currentReplicas []memgraph.ReplicaInfo, log *zap.Logger) {
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
			log.Info("unregistering stale replica",
				zap.String("replica", replica.Name),
				zap.String("mainInstance", mainPod))
			if err := rm.client.UnregisterReplica(ctx, cluster.Namespace, mainPod, replica.Name); err != nil {
				log.Error("failed to unregister stale replica",
					zap.String("replica", replica.Name),
					zap.Error(err))
			} else {
				rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReplicaUnregistered,
					fmt.Sprintf("Stale replica %s unregistered from main", replica.Name))
			}
		}
	}
}

// CheckReplicationHealth checks if all replicas are healthy and in sync
func (rm *ReplicationManager) CheckReplicationHealth(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, writeInstance string, log *zap.Logger) (*memgraphv1alpha1.ReplicationHealth, error) {
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
			log.Warn("unhealthy replica detected",
				zap.String("replica", replica.Name),
				zap.String("status", replica.Status))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaUnhealthy,
				fmt.Sprintf("Replica %s is unhealthy: %s", replica.Name, replica.Status))
		}
	}

	// Emit event if all replicas are healthy
	if health.HealthyReplicas == health.TotalReplicas && health.TotalReplicas > 0 {
		rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReplicationHealthy,
			fmt.Sprintf("All %d replicas are healthy", health.TotalReplicas))
	}

	return health, nil
}

// HandleMainFailover handles failover when the main instance becomes unavailable
// This is called when HighAvailability is enabled and the main is detected as down
func (rm *ReplicationManager) HandleMainFailover(
	ctx context.Context,
	cluster *memgraphv1alpha1.MemgraphCluster,
	pods []corev1.Pod,
	failedMain string,
	log *zap.Logger,
) (string, error) {
	if cluster.Spec.HighAvailability == nil || !cluster.Spec.HighAvailability.Enabled {
		return "", fmt.Errorf("high availability is not enabled")
	}

	rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonMainInstanceFailed,
		fmt.Sprintf("Main instance %s has failed", failedMain))

	log.Info("initiating failover from failed main",
		zap.String("failedMain", failedMain))

	rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonFailoverStarted,
		fmt.Sprintf("Initiating failover from %s", failedMain))

	// Get ready pods excluding the failed main
	var candidates []corev1.Pod
	for _, pod := range pods {
		if pod.Name != failedMain && isPodReady(&pod) {
			candidates = append(candidates, pod)
		}
	}

	if len(candidates) == 0 {
		rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonFailoverFailed,
			"No healthy candidates available for failover")
		return "", fmt.Errorf("no healthy candidates available for failover")
	}

	// Check if preferred main is available
	var newMain string
	if cluster.Spec.HighAvailability.PreferredMain != "" {
		for _, pod := range candidates {
			if pod.Name == cluster.Spec.HighAvailability.PreferredMain {
				log.Info("promoting preferred main",
					zap.String("pod", pod.Name))
				newMain = pod.Name
				break
			}
		}
	}

	// Otherwise, promote the first healthy replica
	if newMain == "" {
		newMain = candidates[0].Name
	}

	log.Info("promoting replica to main",
		zap.String("newMain", newMain),
		zap.String("failedMain", failedMain))

	rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonFailoverCompleted,
		fmt.Sprintf("Failover completed: %s promoted to main", newMain))

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
