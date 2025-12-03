// Copyright 2025 Base14. See LICENSE file for details.

package controller

// Event reason constants for Kubernetes events
const (
	// Cluster lifecycle events
	EventReasonClusterCreated  = "ClusterCreated"
	EventReasonClusterReady    = "ClusterReady"
	EventReasonClusterDegraded = "ClusterDegraded"

	// StatefulSet events
	EventReasonCreatingStatefulSet = "CreatingStatefulSet"
	EventReasonScalingStatefulSet  = "ScalingStatefulSet"

	// Replication events
	EventReasonMainInstanceConfigured = "MainInstanceConfigured"
	EventReasonReplicaRegistered      = "ReplicaRegistered"
	EventReasonReplicaUnregistered    = "ReplicaUnregistered"
	EventReasonReplicationHealthy     = "ReplicationHealthy"
	EventReasonReplicationError       = "ReplicationError"
	EventReasonReplicaUnhealthy       = "ReplicaUnhealthy"
	EventReasonReplicationLagHigh     = "ReplicationLagHigh"

	// Failover events
	EventReasonMainInstanceFailed = "MainInstanceFailed"
	EventReasonFailoverStarted    = "FailoverStarted"
	EventReasonFailoverCompleted  = "FailoverCompleted"
	EventReasonFailoverFailed     = "FailoverFailed"

	// Snapshot events
	EventReasonSnapshotCronJobCreated = "SnapshotCronJobCreated"
	EventReasonSnapshotCronJobUpdated = "SnapshotCronJobUpdated"
	EventReasonSnapshotSucceeded      = "SnapshotSucceeded"
	EventReasonSnapshotFailed         = "SnapshotFailed"

	// S3 backup events
	EventReasonS3BackupSucceeded = "S3BackupSucceeded"
	EventReasonS3BackupFailed    = "S3BackupFailed"

	// Health check events
	EventReasonHealthCheckPassed = "HealthCheckPassed"
	EventReasonHealthCheckFailed = "HealthCheckFailed"

	// Write service events
	EventReasonUpdatedWriteService = "UpdatedWriteService"

	// Reconcile events
	EventReasonReconcileError = "ReconcileError"
)
