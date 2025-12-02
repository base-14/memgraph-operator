// Copyright 2025 Base14. See LICENSE file for details.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReplicationMode defines the replication synchronization mode
// +kubebuilder:validation:Enum=ASYNC;SYNC;STRICT_SYNC
type ReplicationMode string

const (
	ReplicationModeAsync      ReplicationMode = "ASYNC"
	ReplicationModeSync       ReplicationMode = "SYNC"
	ReplicationModeStrictSync ReplicationMode = "STRICT_SYNC"
)

// ClusterPhase defines the current phase of the cluster
// +kubebuilder:validation:Enum=Pending;Initializing;Running;Failed
type ClusterPhase string

const (
	ClusterPhasePending      ClusterPhase = "Pending"
	ClusterPhaseInitializing ClusterPhase = "Initializing"
	ClusterPhaseRunning      ClusterPhase = "Running"
	ClusterPhaseFailed       ClusterPhase = "Failed"
)

// MemgraphClusterSpec defines the desired state of MemgraphCluster
type MemgraphClusterSpec struct {
	// Replicas is the total number of Memgraph instances
	// One will be designated as write (MAIN), others as read (REPLICA)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Image is the Memgraph container image to use
	// +kubebuilder:default="memgraph/memgraph:2.21.0"
	// +optional
	Image string `json:"image,omitempty"`

	// Resources defines the compute resources for all instances
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Storage defines the persistent storage configuration
	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// Config defines Memgraph-specific configuration
	// +optional
	Config MemgraphConfig `json:"config,omitempty"`

	// Replication defines the replication settings
	// +optional
	Replication ReplicationSpec `json:"replication,omitempty"`

	// HighAvailability defines automatic promotion settings
	// +optional
	HighAvailability *HighAvailabilitySpec `json:"highAvailability,omitempty"`

	// Snapshot defines snapshot and backup configuration
	// +optional
	Snapshot SnapshotSpec `json:"snapshot,omitempty"`

	// NodeSelector for pod scheduling
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for pod scheduling
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity for pod scheduling
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// StorageSpec defines the persistent storage configuration
type StorageSpec struct {
	// Size is the size of the persistent volume
	// +kubebuilder:default="10Gi"
	// +optional
	Size resource.Quantity `json:"size,omitempty"`

	// StorageClassName is the name of the storage class to use
	// If not specified, the default storage class will be used
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// MemgraphConfig defines Memgraph-specific configuration options
type MemgraphConfig struct {
	// LogLevel sets the Memgraph log level
	// +kubebuilder:validation:Enum=TRACE;DEBUG;INFO;WARNING;ERROR
	// +kubebuilder:default="WARNING"
	// +optional
	LogLevel string `json:"logLevel,omitempty"`

	// MemoryLimit sets the memory limit in MB for Memgraph
	// +kubebuilder:default=3000
	// +optional
	MemoryLimit int32 `json:"memoryLimit,omitempty"`

	// WALFlushEveryNTx sets the number of transactions before WAL flush
	// +kubebuilder:default=100000
	// +optional
	WALFlushEveryNTx int32 `json:"walFlushEveryNTx,omitempty"`
}

// ReplicationSpec defines the replication settings
type ReplicationSpec struct {
	// Mode is the replication mode (ASYNC, SYNC, STRICT_SYNC)
	// +kubebuilder:default="ASYNC"
	// +optional
	Mode ReplicationMode `json:"mode,omitempty"`
}

// HighAvailabilitySpec defines automatic promotion/failover settings
type HighAvailabilitySpec struct {
	// Enabled enables automatic promotion of a replica to main when main fails
	// NOTE: This feature is not yet implemented
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// FailoverTimeoutSeconds is the time to wait before promoting a replica
	// after the main instance becomes unavailable
	// +kubebuilder:default=30
	// +optional
	FailoverTimeoutSeconds int32 `json:"failoverTimeoutSeconds,omitempty"`

	// PreferredMain is the preferred pod name to be the main instance
	// If not set, the controller will choose the first available pod
	// +optional
	PreferredMain string `json:"preferredMain,omitempty"`
}

// SnapshotSpec defines snapshot and backup configuration
type SnapshotSpec struct {
	// Enabled enables periodic snapshots
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Schedule is a cron expression for snapshot frequency
	// +kubebuilder:default="*/15 * * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// RetentionCount is the number of snapshots to retain on disk
	// +kubebuilder:default=5
	// +optional
	RetentionCount int32 `json:"retentionCount,omitempty"`

	// S3 defines optional S3 backup configuration
	// +optional
	S3 *S3BackupSpec `json:"s3,omitempty"`
}

// S3BackupSpec defines S3 backup configuration
type S3BackupSpec struct {
	// Enabled enables S3 backups
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Bucket is the S3 bucket name
	// +optional
	Bucket string `json:"bucket,omitempty"`

	// Region is the AWS region
	// +kubebuilder:default="us-east-1"
	// +optional
	Region string `json:"region,omitempty"`

	// Endpoint is an optional S3-compatible endpoint URL
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Prefix is the path prefix within the bucket
	// +kubebuilder:default="memgraph/snapshots"
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// SecretRef references a secret containing S3 credentials
	// The secret must have keys: access-key-id, secret-access-key
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// RetentionDays is the number of days to retain S3 backups
	// +kubebuilder:default=7
	// +optional
	RetentionDays int32 `json:"retentionDays,omitempty"`
}

// MemgraphClusterStatus defines the observed state of MemgraphCluster
type MemgraphClusterStatus struct {
	// Phase is the current phase of the cluster
	// +optional
	Phase ClusterPhase `json:"phase,omitempty"`

	// WriteInstance is the pod name of the current MAIN (write) instance
	// +optional
	WriteInstance string `json:"writeInstance,omitempty"`

	// ReadInstances is the list of pod names that are REPLICAs (read)
	// +optional
	ReadInstances []string `json:"readInstances,omitempty"`

	// ReadyInstances is the number of ready instances (regardless of role)
	// +optional
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// RegisteredReplicas is the number of replicas registered with the main instance
	// +optional
	RegisteredReplicas int32 `json:"registeredReplicas,omitempty"`

	// LastSnapshotTime is the time of the last successful snapshot
	// +optional
	LastSnapshotTime *metav1.Time `json:"lastSnapshotTime,omitempty"`

	// LastS3BackupTime is the time of the last successful S3 backup
	// +optional
	LastS3BackupTime *metav1.Time `json:"lastS3BackupTime,omitempty"`

	// Validation contains real-time validation test results
	// +optional
	Validation *ValidationStatus `json:"validation,omitempty"`

	// Conditions represent the latest available observations of the cluster's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ValidationStatus contains real-time validation test results
type ValidationStatus struct {
	// LastConnectivityTest is the time of the last connectivity test
	// +optional
	LastConnectivityTest *metav1.Time `json:"lastConnectivityTest,omitempty"`

	// LastReplicationTest is the time of the last replication test
	// +optional
	LastReplicationTest *metav1.Time `json:"lastReplicationTest,omitempty"`

	// ReplicationLagMs is the measured replication lag in milliseconds
	// +optional
	ReplicationLagMs int64 `json:"replicationLagMs,omitempty"`

	// AllReplicasHealthy indicates if all replicas passed health checks
	// +optional
	AllReplicasHealthy bool `json:"allReplicasHealthy,omitempty"`

	// Instances contains per-instance validation status
	// +optional
	Instances []InstanceValidationStatus `json:"instances,omitempty"`
}

// InstanceValidationStatus contains validation status for a single instance
type InstanceValidationStatus struct {
	// Name is the pod name
	Name string `json:"name"`

	// Role is the current role (MAIN or REPLICA)
	Role string `json:"role"`

	// Healthy indicates if the instance is healthy
	Healthy bool `json:"healthy"`

	// LagMs is the replication lag for this instance in milliseconds (only for replicas)
	// +optional
	LagMs int64 `json:"lagMs,omitempty"`

	// LastTestTime is the time of the last validation test
	// +optional
	LastTestTime *metav1.Time `json:"lastTestTime,omitempty"`
}

// ReplicationHealth contains replication health information
type ReplicationHealth struct {
	// TotalReplicas is the total number of registered replicas
	TotalReplicas int32 `json:"totalReplicas"`

	// HealthyReplicas is the number of replicas that are healthy
	HealthyReplicas int32 `json:"healthyReplicas"`
}

// Condition types for MemgraphCluster
const (
	// ConditionTypeReady indicates the cluster is fully operational
	ConditionTypeReady = "Ready"

	// ConditionTypeMainAvailable indicates the main instance is available
	ConditionTypeMainAvailable = "MainAvailable"

	// ConditionTypeReplicasReady indicates all read replicas are ready
	ConditionTypeReplicasReady = "ReplicasReady"

	// ConditionTypeReplicationHealthy indicates replication is working
	ConditionTypeReplicationHealthy = "ReplicationHealthy"

	// ConditionTypeReplicationLagAcceptable indicates replication lag is within threshold
	ConditionTypeReplicationLagAcceptable = "ReplicationLagAcceptable"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Main",type=string,JSONPath=`.status.writeInstance`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyInstances`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MemgraphCluster is the Schema for the memgraphclusters API
type MemgraphCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemgraphClusterSpec   `json:"spec,omitempty"`
	Status MemgraphClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MemgraphClusterList contains a list of MemgraphCluster
type MemgraphClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemgraphCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MemgraphCluster{}, &MemgraphClusterList{})
}
