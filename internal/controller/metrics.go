// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
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
		instanceHealthGauge,
		reconcileOperationsTotal,
		reconcileDurationHistogram,
		snapshotLastSuccessTimestamp,
		snapshotOperationsTotal,
		failoverEventsTotal,
		validationLastRunTimestamp,
		validationPassedGauge,
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

// RecordReplicationHealth records replication health metrics
func (m *MetricsRecorder) RecordReplicationHealth(cluster, namespace string, lagMs int64, healthy bool) {
	replicationLagGauge.WithLabelValues(cluster, namespace).Set(float64(lagMs))
	healthyValue := 0.0
	if healthy {
		healthyValue = 1.0
	}
	replicationHealthyGauge.WithLabelValues(cluster, namespace).Set(healthyValue)
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

// DeleteClusterMetrics removes metrics for a deleted cluster
func (m *MetricsRecorder) DeleteClusterMetrics(cluster, namespace string) {
	clusterPhaseGauge.DeleteLabelValues(cluster, namespace)
	clusterReadyInstancesGauge.DeleteLabelValues(cluster, namespace)
	clusterDesiredInstancesGauge.DeleteLabelValues(cluster, namespace)
	clusterRegisteredReplicasGauge.DeleteLabelValues(cluster, namespace)
	replicationLagGauge.DeleteLabelValues(cluster, namespace)
	replicationHealthyGauge.DeleteLabelValues(cluster, namespace)
	snapshotLastSuccessTimestamp.DeleteLabelValues(cluster, namespace)
	validationLastRunTimestamp.DeleteLabelValues(cluster, namespace)
	validationPassedGauge.DeleteLabelValues(cluster, namespace)
}
