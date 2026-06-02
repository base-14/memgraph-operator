// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

// ReplicationManager handles Memgraph replication configuration and health checks.
type ReplicationManager struct {
	client   *memgraph.Client
	recorder record.EventRecorder
	metrics  *MetricsRecorder

	// states tracks per-replica timers for classification. Keyed by
	// "namespace/name" -> replica name -> state. Lost on operator restart.
	statesMu sync.Mutex
	states   map[string]map[string]*replicaState

	// now is the clock used for classification. Overridable in tests.
	now func() time.Time
}

// NewReplicationManager creates a new ReplicationManager
func NewReplicationManager(client *memgraph.Client, recorder record.EventRecorder) *ReplicationManager {
	return &ReplicationManager{
		client:   client,
		recorder: recorder,
		states:   make(map[string]map[string]*replicaState),
		now:      time.Now,
	}
}

// SetMetricsRecorder wires the metrics recorder. Safe to call once at startup.
func (rm *ReplicationManager) SetMetricsRecorder(m *MetricsRecorder) {
	rm.metrics = m
}

// stateFor returns (creating if necessary) the per-replica state entry.
// Caller must hold statesMu.
func (rm *ReplicationManager) stateFor(clusterKey, replicaName string) *replicaState {
	byReplica, ok := rm.states[clusterKey]
	if !ok {
		byReplica = make(map[string]*replicaState)
		rm.states[clusterKey] = byReplica
	}
	st, ok := byReplica[replicaName]
	if !ok {
		st = &replicaState{}
		byReplica[replicaName] = st
	}
	return st
}

// pruneStates drops state entries for replicas no longer in the observed set.
// Caller must hold statesMu.
func (rm *ReplicationManager) pruneStates(clusterKey string, observed map[string]struct{}) {
	byReplica, ok := rm.states[clusterKey]
	if !ok {
		return
	}
	for name := range byReplica {
		if _, kept := observed[name]; !kept {
			delete(byReplica, name)
		}
	}
}

// DeleteClusterState removes all per-replica state for a cluster and
// drops the matching per-replica metric series. Called during cluster deletion.
func (rm *ReplicationManager) DeleteClusterState(clusterName, namespace string) {
	clusterKey := namespace + "/" + clusterName

	rm.statesMu.Lock()
	defer rm.statesMu.Unlock()

	byReplica, ok := rm.states[clusterKey]
	if !ok {
		return
	}
	if rm.metrics != nil {
		for replicaName := range byReplica {
			rm.metrics.DeleteReplicaMetrics(clusterName, namespace, replicaName)
		}
	}
	delete(rm.states, clusterKey)
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
				if rm.metrics != nil {
					rm.metrics.DeleteReplicaMetrics(cluster.Name, cluster.Namespace, replica.Name)
				}
			}
		}
	}
}

// CheckReplicationHealth classifies each registered replica, emits events,
// updates per-replica metrics, and returns aggregate health counts.
func (rm *ReplicationManager) CheckReplicationHealth(
	ctx context.Context,
	cluster *memgraphv1alpha1.MemgraphCluster,
	writeInstance string,
	log *zap.Logger,
) (*memgraphv1alpha1.ReplicationHealth, error) {
	if writeInstance == "" {
		return nil, fmt.Errorf("no write instance specified")
	}

	replicas, err := rm.client.ShowReplicas(ctx, cluster.Namespace, writeInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to show replicas: %w", err)
	}

	behindThreshold := cluster.Spec.Replication.BehindAlertThreshold.Duration
	if behindThreshold <= 0 {
		behindThreshold = 5 * time.Minute // safety net if defaulting did not apply
	}

	now := rm.now()
	clusterKey := cluster.Namespace + "/" + cluster.Name

	health := &memgraphv1alpha1.ReplicationHealth{
		TotalReplicas:   int32(len(replicas)),
		HealthyReplicas: 0,
	}

	observed := make(map[string]struct{}, len(replicas))

	rm.statesMu.Lock()
	defer rm.statesMu.Unlock()

	for _, replica := range replicas {
		observed[replica.Name] = struct{}{}
		st := rm.stateFor(clusterKey, replica.Name)

		class := classifyReplica(replica, st, now, behindThreshold)

		if class.isHealthy() {
			health.HealthyReplicas++
		}

		// Metrics
		if rm.metrics != nil {
			channelUp := class != classificationDataChannelDown &&
				class != classificationTransient &&
				class != classificationInvalid &&
				class != classificationUnknownStatus
			rm.metrics.RecordReplicaDataChannel(cluster.Name, cluster.Namespace, replica.Name, channelUp)

			behindSeconds := 0.0
			if !st.behindSince.IsZero() {
				behindSeconds = now.Sub(st.behindSince).Seconds()
			}
			rm.metrics.RecordReplicaBehindSeconds(cluster.Name, cluster.Namespace, replica.Name, behindSeconds)
		}

		// Events for unhealthy classifications only.
		switch class {
		case classificationDataChannelDown:
			log.Warn("replica data channel down",
				zap.String("replica", replica.Name),
				zap.Duration("emptyFor", now.Sub(st.channelDownSince)))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaDataChannelDown,
				fmt.Sprintf("Replica %s registered but data_info is empty for %s — main has not opened a data channel. Check connectivity on :10000, replica role, and version match. Re-registering with a fresh PVC usually resolves this.",
					replica.Name, now.Sub(st.channelDownSince).Round(time.Second)))
		case classificationInvalid:
			log.Warn("replica in invalid state",
				zap.String("replica", replica.Name),
				zap.Any("dataInfo", replica.DataInfo))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaInvalid,
				fmt.Sprintf("Replica %s reports invalid status. Manual intervention required (drop + re-register).", replica.Name))
		case classificationBehindTooLong:
			log.Warn("replica behind for too long",
				zap.String("replica", replica.Name),
				zap.Duration("behindFor", now.Sub(st.behindSince)),
				zap.Duration("threshold", behindThreshold))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaBehindTooLong,
				fmt.Sprintf("Replica %s has been behind for %s (threshold %s).",
					replica.Name, now.Sub(st.behindSince).Round(time.Second), behindThreshold))
		case classificationUnknownStatus:
			log.Warn("replica reports unknown status",
				zap.String("replica", replica.Name),
				zap.Any("dataInfo", replica.DataInfo))
		}
	}

	rm.pruneStates(clusterKey, observed)

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
