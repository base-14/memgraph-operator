// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"github.com/base14/memgraph-operator/internal/memgraph"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// Cluster metrics
	clusterPhaseGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_phase",
			Help: "Current phase of the Memgraph cluster (0=Pending, 1=Initializing, 2=Running, 3=Failed)",
		},
		[]string{"cluster", "namespace"},
	)

	clusterReadyInstancesGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_ready_instances",
			Help: "Number of ready instances in the cluster",
		},
		[]string{"cluster", "namespace"},
	)

	clusterDesiredInstancesGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_desired_instances",
			Help: "Desired number of instances in the cluster",
		},
		[]string{"cluster", "namespace"},
	)

	clusterRegisteredReplicasGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_registered_replicas",
			Help: "Number of registered replicas with the main instance",
		},
		[]string{"cluster", "namespace"},
	)

	// Replication metrics
	replicationLagGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replication_lag_milliseconds",
			Help: "Replication lag in milliseconds",
		},
		[]string{"cluster", "namespace"},
	)

	replicationHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replication_healthy",
			Help: "Whether replication is healthy (1) or not (0)",
		},
		[]string{"cluster", "namespace"},
	)

	// Per-replica replication metrics (from SHOW REPLICAS data_info).
	// These distinguish "registered" from "actually streaming data": a replica
	// can be registered and heartbeating while replicating nothing (empty
	// data_info), which is invisible to registration-count based metrics.
	replicaHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replica_healthy",
			Help: "Whether the replica is streaming data (1 = status ready/replicating, 0 = recovery/invalid)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	replicaStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replica_status",
			Help: "Current replication status of the replica (1 for the active status label: ready, replicating, recovery, invalid)",
		},
		[]string{"cluster", "namespace", "replica", "status"},
	)

	replicaBehindGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replica_behind_count",
			Help: "Number of transactions the replica is behind the main (from SHOW REPLICAS data_info)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	replicaLastConfirmedTimestampGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replica_last_confirmed_timestamp_seconds",
			Help: "Last confirmed replication timestamp for the replica (0 = data streaming never engaged)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	replicaDataInfoPresentGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replica_data_info_present",
			Help: "Whether the main has confirmed replication state for the replica (0 = data_info empty, streaming never engaged)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	clusterReplicasTotalGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_replicas_total",
			Help: "Number of replicas registered with the main instance",
		},
		[]string{"cluster", "namespace"},
	)

	clusterReplicasHealthyTotalGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_cluster_replicas_healthy_total",
			Help: "Number of registered replicas that are actively streaming data (alert when < memgraph_cluster_replicas_total)",
		},
		[]string{"cluster", "namespace"},
	)

	// Data drift metrics: explicit main-minus-replica counts so a silently
	// empty or stale replica is directly observable.
	replicationVertexDriftGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replication_vertex_drift",
			Help: "Vertex count difference between main and the replica (main minus replica; non-zero indicates drift)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	replicationEdgeDriftGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_replication_edge_drift",
			Help: "Edge count difference between main and the replica (main minus replica; non-zero indicates drift)",
		},
		[]string{"cluster", "namespace", "replica"},
	)

	// Instance metrics
	instanceHealthGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_instance_healthy",
			Help: "Whether an instance is healthy (1) or not (0)",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	// Reconciliation metrics
	reconcileOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "memgraph_reconcile_operations_total",
			Help: "Total number of reconcile operations by result",
		},
		[]string{"cluster", "namespace", "result"},
	)

	reconcileDurationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "memgraph_reconcile_duration_seconds",
			Help:    "Duration of reconcile operations in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
		},
		[]string{"cluster", "namespace"},
	)

	// Snapshot metrics
	snapshotLastSuccessTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_snapshot_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful snapshot",
		},
		[]string{"cluster", "namespace"},
	)

	snapshotOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "memgraph_snapshot_operations_total",
			Help: "Total number of snapshot operations by result",
		},
		[]string{"cluster", "namespace", "result"},
	)

	// Failover metrics
	failoverEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "memgraph_failover_events_total",
			Help: "Total number of failover events",
		},
		[]string{"cluster", "namespace", "from_instance", "to_instance"},
	)

	// Validation metrics
	validationLastRunTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_validation_last_run_timestamp_seconds",
			Help: "Unix timestamp of the last validation run",
		},
		[]string{"cluster", "namespace"},
	)

	validationPassedGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_validation_passed",
			Help: "Whether the last validation passed (1) or not (0)",
		},
		[]string{"cluster", "namespace"},
	)

	// Memgraph storage metrics (from SHOW STORAGE INFO)
	storageVertexCountGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_vertex_count",
			Help: "Number of vertices in the database",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageEdgeCountGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_edge_count",
			Help: "Number of edges in the database",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageAverageDegreeGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_average_degree",
			Help: "Average degree of vertices in the database",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageMemoryResGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_memory_resident_bytes",
			Help: "Current resident memory usage in bytes",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storagePeakMemoryResGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_memory_peak_bytes",
			Help: "Peak resident memory usage in bytes",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageDiskUsageGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_disk_usage_bytes",
			Help: "Disk space consumed in bytes",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageMemoryTrackedGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_memory_tracked_bytes",
			Help: "Actively tracked memory allocation in bytes",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageAllocationLimitGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_allocation_limit_bytes",
			Help: "Maximum memory allocation limit in bytes",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)

	storageUnreleasedDeltaObjectsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memgraph_storage_unreleased_delta_objects",
			Help: "Count of delta objects awaiting cleanup",
		},
		[]string{"cluster", "namespace", "instance", "role"},
	)
)

func init() {
	// Register all metrics with the controller-runtime metrics registry
	metrics.Registry.MustRegister(
		clusterPhaseGauge,
		clusterReadyInstancesGauge,
		clusterDesiredInstancesGauge,
		clusterRegisteredReplicasGauge,
		replicationLagGauge,
		replicationHealthyGauge,
		replicaHealthyGauge,
		replicaStatusGauge,
		replicaBehindGauge,
		replicaLastConfirmedTimestampGauge,
		replicaDataInfoPresentGauge,
		clusterReplicasTotalGauge,
		clusterReplicasHealthyTotalGauge,
		replicationVertexDriftGauge,
		replicationEdgeDriftGauge,
		instanceHealthGauge,
		reconcileOperationsTotal,
		reconcileDurationHistogram,
		snapshotLastSuccessTimestamp,
		snapshotOperationsTotal,
		failoverEventsTotal,
		validationLastRunTimestamp,
		validationPassedGauge,
		// Storage metrics
		storageVertexCountGauge,
		storageEdgeCountGauge,
		storageAverageDegreeGauge,
		storageMemoryResGauge,
		storagePeakMemoryResGauge,
		storageDiskUsageGauge,
		storageMemoryTrackedGauge,
		storageAllocationLimitGauge,
		storageUnreleasedDeltaObjectsGauge,
	)
}

// MetricsRecorder records metrics for the Memgraph operator
type MetricsRecorder struct{}

// NewMetricsRecorder creates a new MetricsRecorder
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{}
}

// RecordClusterPhase records the current cluster phase
func (m *MetricsRecorder) RecordClusterPhase(cluster, namespace, phase string) {
	phaseValue := 0.0
	switch phase {
	case "Pending":
		phaseValue = 0
	case "Initializing":
		phaseValue = 1
	case "Running":
		phaseValue = 2
	case "Failed":
		phaseValue = 3
	}
	clusterPhaseGauge.WithLabelValues(cluster, namespace).Set(phaseValue)
}

// RecordClusterInstances records the instance counts
func (m *MetricsRecorder) RecordClusterInstances(cluster, namespace string, ready, desired, registered int32) {
	clusterReadyInstancesGauge.WithLabelValues(cluster, namespace).Set(float64(ready))
	clusterDesiredInstancesGauge.WithLabelValues(cluster, namespace).Set(float64(desired))
	clusterRegisteredReplicasGauge.WithLabelValues(cluster, namespace).Set(float64(registered))
}

// RecordReplicationLag records the measured replication lag.
// Note: memgraph_replication_healthy is owned by RecordReplicaSet (streaming
// health from SHOW REPLICAS data_info), not by the validation lag test.
func (m *MetricsRecorder) RecordReplicationLag(cluster, namespace string, lagMs int64) {
	replicationLagGauge.WithLabelValues(cluster, namespace).Set(float64(lagMs))
}

// knownReplicaStatuses is the one-hot label set for memgraph_replica_status
var knownReplicaStatuses = []string{
	memgraph.ReplicaStatusReady,
	memgraph.ReplicaStatusReplicating,
	memgraph.ReplicaStatusRecovery,
	memgraph.ReplicaStatusInvalid,
}

// RecordReplicaSet records per-replica streaming health and cluster rollups
// from a full SHOW REPLICAS result. Series for replicas that no longer exist
// are removed first, so stale replicas disappear from the scrape.
func (m *MetricsRecorder) RecordReplicaSet(cluster, namespace string, replicas []memgraph.ReplicaInfo) {
	partial := prometheus.Labels{"cluster": cluster, "namespace": namespace}
	replicaHealthyGauge.DeletePartialMatch(partial)
	replicaStatusGauge.DeletePartialMatch(partial)
	replicaBehindGauge.DeletePartialMatch(partial)
	replicaLastConfirmedTimestampGauge.DeletePartialMatch(partial)
	replicaDataInfoPresentGauge.DeletePartialMatch(partial)

	var healthyCount int
	for _, replica := range replicas {
		healthyValue := 0.0
		if replica.IsHealthy() {
			healthyValue = 1.0
			healthyCount++
		}
		replicaHealthyGauge.WithLabelValues(cluster, namespace, replica.Name).Set(healthyValue)

		for _, status := range knownReplicaStatuses {
			statusValue := 0.0
			if status == replica.Status {
				statusValue = 1.0
			}
			replicaStatusGauge.WithLabelValues(cluster, namespace, replica.Name, status).Set(statusValue)
		}

		replicaBehindGauge.WithLabelValues(cluster, namespace, replica.Name).Set(float64(replica.Behind))
		replicaLastConfirmedTimestampGauge.WithLabelValues(cluster, namespace, replica.Name).Set(float64(replica.Timestamp))

		dataInfoValue := 0.0
		if replica.DataInfoPresent() {
			dataInfoValue = 1.0
		}
		replicaDataInfoPresentGauge.WithLabelValues(cluster, namespace, replica.Name).Set(dataInfoValue)
	}

	clusterReplicasTotalGauge.WithLabelValues(cluster, namespace).Set(float64(len(replicas)))
	clusterReplicasHealthyTotalGauge.WithLabelValues(cluster, namespace).Set(float64(healthyCount))

	// Cluster-level replication health: every registered replica is streaming
	allHealthy := 0.0
	if healthyCount == len(replicas) {
		allHealthy = 1.0
	}
	replicationHealthyGauge.WithLabelValues(cluster, namespace).Set(allHealthy)
}

// RecordReplicationDrift records the data drift between main and a replica
func (m *MetricsRecorder) RecordReplicationDrift(cluster, namespace, replica string, vertexDrift, edgeDrift int64) {
	replicationVertexDriftGauge.WithLabelValues(cluster, namespace, replica).Set(float64(vertexDrift))
	replicationEdgeDriftGauge.WithLabelValues(cluster, namespace, replica).Set(float64(edgeDrift))
}

// DeleteReplicationDriftMetrics removes all drift series for a cluster so
// stale replicas do not keep reporting old drift values
func (m *MetricsRecorder) DeleteReplicationDriftMetrics(cluster, namespace string) {
	partial := prometheus.Labels{"cluster": cluster, "namespace": namespace}
	replicationVertexDriftGauge.DeletePartialMatch(partial)
	replicationEdgeDriftGauge.DeletePartialMatch(partial)
}

// RecordInstanceHealth records instance health metrics
func (m *MetricsRecorder) RecordInstanceHealth(cluster, namespace, instance, role string, healthy bool) {
	healthyValue := 0.0
	if healthy {
		healthyValue = 1.0
	}
	instanceHealthGauge.WithLabelValues(cluster, namespace, instance, role).Set(healthyValue)
}

// RecordReconcileOperation records a reconcile operation
func (m *MetricsRecorder) RecordReconcileOperation(cluster, namespace, result string) {
	reconcileOperationsTotal.WithLabelValues(cluster, namespace, result).Inc()
}

// RecordReconcileDuration records the duration of a reconcile operation
func (m *MetricsRecorder) RecordReconcileDuration(cluster, namespace string, durationSeconds float64) {
	reconcileDurationHistogram.WithLabelValues(cluster, namespace).Observe(durationSeconds)
}

// RecordSnapshotSuccess records a successful snapshot
func (m *MetricsRecorder) RecordSnapshotSuccess(cluster, namespace string, timestamp float64) {
	snapshotLastSuccessTimestamp.WithLabelValues(cluster, namespace).Set(timestamp)
	snapshotOperationsTotal.WithLabelValues(cluster, namespace, "success").Inc()
}

// RecordSnapshotFailure records a failed snapshot
func (m *MetricsRecorder) RecordSnapshotFailure(cluster, namespace string) {
	snapshotOperationsTotal.WithLabelValues(cluster, namespace, "failure").Inc()
}

// RecordFailoverEvent records a failover event
func (m *MetricsRecorder) RecordFailoverEvent(cluster, namespace, fromInstance, toInstance string) {
	failoverEventsTotal.WithLabelValues(cluster, namespace, fromInstance, toInstance).Inc()
}

// RecordValidation records validation results
func (m *MetricsRecorder) RecordValidation(cluster, namespace string, timestamp float64, passed bool) {
	validationLastRunTimestamp.WithLabelValues(cluster, namespace).Set(timestamp)
	passedValue := 0.0
	if passed {
		passedValue = 1.0
	}
	validationPassedGauge.WithLabelValues(cluster, namespace).Set(passedValue)
}

// RecordStorageInfo records storage metrics from SHOW STORAGE INFO
func (m *MetricsRecorder) RecordStorageInfo(cluster, namespace, instance, role string, info *memgraph.StorageInfo) {
	if info == nil {
		return
	}
	storageVertexCountGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.VertexCount))
	storageEdgeCountGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.EdgeCount))
	storageAverageDegreeGauge.WithLabelValues(cluster, namespace, instance, role).Set(info.AverageDegree)
	storageMemoryResGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.MemoryRes))
	storagePeakMemoryResGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.PeakMemoryRes))
	storageDiskUsageGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.DiskUsage))
	storageMemoryTrackedGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.MemoryTracked))
	storageAllocationLimitGauge.WithLabelValues(cluster, namespace, instance, role).Set(float64(info.AllocationLimit))
	storageUnreleasedDeltaObjectsGauge.WithLabelValues(cluster, namespace, instance, role).
		Set(float64(info.UnreleasedDeltaObjects))
}

// DeleteInstanceStorageMetrics removes storage metrics for a specific instance
func (m *MetricsRecorder) DeleteInstanceStorageMetrics(cluster, namespace, instance, role string) {
	storageVertexCountGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageEdgeCountGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageAverageDegreeGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageMemoryResGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storagePeakMemoryResGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageDiskUsageGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageMemoryTrackedGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageAllocationLimitGauge.DeleteLabelValues(cluster, namespace, instance, role)
	storageUnreleasedDeltaObjectsGauge.DeleteLabelValues(cluster, namespace, instance, role)
}

// DeleteClusterMetrics removes metrics for a deleted cluster
func (m *MetricsRecorder) DeleteClusterMetrics(cluster, namespace string) {
	clusterPhaseGauge.DeleteLabelValues(cluster, namespace)
	clusterReadyInstancesGauge.DeleteLabelValues(cluster, namespace)
	clusterDesiredInstancesGauge.DeleteLabelValues(cluster, namespace)
	clusterRegisteredReplicasGauge.DeleteLabelValues(cluster, namespace)
	replicationLagGauge.DeleteLabelValues(cluster, namespace)
	replicationHealthyGauge.DeleteLabelValues(cluster, namespace)
	clusterReplicasTotalGauge.DeleteLabelValues(cluster, namespace)
	clusterReplicasHealthyTotalGauge.DeleteLabelValues(cluster, namespace)
	snapshotLastSuccessTimestamp.DeleteLabelValues(cluster, namespace)
	validationLastRunTimestamp.DeleteLabelValues(cluster, namespace)
	validationPassedGauge.DeleteLabelValues(cluster, namespace)

	// Sweep all per-replica and per-instance series for the cluster
	partial := prometheus.Labels{"cluster": cluster, "namespace": namespace}
	replicaHealthyGauge.DeletePartialMatch(partial)
	replicaStatusGauge.DeletePartialMatch(partial)
	replicaBehindGauge.DeletePartialMatch(partial)
	replicaLastConfirmedTimestampGauge.DeletePartialMatch(partial)
	replicaDataInfoPresentGauge.DeletePartialMatch(partial)
	replicationVertexDriftGauge.DeletePartialMatch(partial)
	replicationEdgeDriftGauge.DeletePartialMatch(partial)
	instanceHealthGauge.DeletePartialMatch(partial)
}
