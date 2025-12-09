// Copyright 2025 Base14. See LICENSE file for details.

package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMemgraphCluster_DeepCopy(t *testing.T) {
	original := &MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: MemgraphClusterSpec{
			Replicas: 3,
			Image:    "memgraph/memgraph:2.21.0",
			Storage: StorageSpec{
				Size: resource.MustParse("10Gi"),
			},
			Config: MemgraphConfig{
				LogLevel:         "INFO",
				MemoryLimit:      4000,
				WALFlushEveryNTx: 50000,
			},
			Replication: ReplicationSpec{
				Mode: ReplicationModeAsync,
			},
			HighAvailability: &HighAvailabilitySpec{
				Enabled:                true,
				FailoverTimeoutSeconds: 30,
				PreferredMain:          "test-cluster-0",
			},
			Snapshot: SnapshotSpec{
				Enabled:        true,
				Schedule:       "*/15 * * * *",
				RetentionCount: 5,
				S3: &S3BackupSpec{
					Enabled: true,
					Bucket:  "test-bucket",
					Region:  "us-east-1",
				},
			},
			NodeSelector: map[string]string{
				"disktype": "ssd",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "dedicated",
					Operator: corev1.TolerationOpEqual,
					Value:    "memgraph",
				},
			},
		},
		Status: MemgraphClusterStatus{
			Phase:              ClusterPhaseRunning,
			WriteInstance:      "test-cluster-0",
			ReadInstances:      []string{"test-cluster-1", "test-cluster-2"},
			ReadyInstances:     3,
			RegisteredReplicas: 2,
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy should return a new object")
	}

	if copied.Name != original.Name {
		t.Errorf("Name mismatch: got %s, want %s", copied.Name, original.Name)
	}

	if copied.Spec.Replicas != original.Spec.Replicas {
		t.Errorf("Replicas mismatch: got %d, want %d", copied.Spec.Replicas, original.Spec.Replicas)
	}

	if copied.Status.Phase != original.Status.Phase {
		t.Errorf("Phase mismatch: got %s, want %s", copied.Status.Phase, original.Status.Phase)
	}

	// Verify deep copy - modifying the copy shouldn't affect the original
	copied.Spec.Replicas = 5
	if original.Spec.Replicas == 5 {
		t.Error("DeepCopy should create independent copy")
	}

	// Test DeepCopyObject
	objCopy := original.DeepCopyObject()
	if objCopy == nil {
		t.Error("DeepCopyObject returned nil")
	}

	typedCopy, ok := objCopy.(*MemgraphCluster)
	if !ok {
		t.Error("DeepCopyObject should return *MemgraphCluster")
	}

	if typedCopy.Name != original.Name {
		t.Errorf("Name mismatch after DeepCopyObject: got %s, want %s", typedCopy.Name, original.Name)
	}
}

func TestMemgraphClusterList_DeepCopy(t *testing.T) {
	original := &MemgraphClusterList{
		Items: []MemgraphCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       MemgraphClusterSpec{Replicas: 3},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       MemgraphClusterSpec{Replicas: 5},
			},
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if len(copied.Items) != len(original.Items) {
		t.Errorf("Items count mismatch: got %d, want %d", len(copied.Items), len(original.Items))
	}

	// Verify independence
	copied.Items[0].Spec.Replicas = 10
	if original.Items[0].Spec.Replicas == 10 {
		t.Error("DeepCopy should create independent copy")
	}

	// Test DeepCopyObject
	objCopy := original.DeepCopyObject()
	if objCopy == nil {
		t.Error("DeepCopyObject returned nil")
	}
}

func TestMemgraphClusterSpec_DeepCopy(t *testing.T) {
	storageClassName := "fast-ssd"
	secretRef := &corev1.LocalObjectReference{Name: "s3-secret"}

	original := MemgraphClusterSpec{
		Replicas: 3,
		Image:    "memgraph/memgraph:2.21.0",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		Storage: StorageSpec{
			Size:             resource.MustParse("50Gi"),
			StorageClassName: &storageClassName,
		},
		ServiceNames: &ServiceNamesSpec{
			HeadlessSuffix: "-headless",
			WriteSuffix:    "-primary",
			ReadSuffix:     "-secondary",
		},
		Snapshot: SnapshotSpec{
			Enabled:        true,
			Schedule:       "0 * * * *",
			RetentionCount: 10,
			S3: &S3BackupSpec{
				Enabled:       true,
				Bucket:        "backup-bucket",
				Region:        "us-west-2",
				Endpoint:      "https://minio.local:9000",
				Prefix:        "memgraph/backups",
				SecretRef:     secretRef,
				RetentionDays: 30,
			},
		},
		Affinity: &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{},
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Replicas != original.Replicas {
		t.Errorf("Replicas mismatch: got %d, want %d", copied.Replicas, original.Replicas)
	}

	if copied.Storage.StorageClassName == nil {
		t.Error("StorageClassName should not be nil")
	}

	if copied.ServiceNames == nil {
		t.Error("ServiceNames should not be nil")
	}

	if copied.Snapshot.S3 == nil {
		t.Error("S3 config should not be nil")
	}
}

func TestMemgraphClusterStatus_DeepCopy(t *testing.T) {
	now := metav1.Now()
	original := MemgraphClusterStatus{
		Phase:              ClusterPhaseRunning,
		WriteInstance:      "cluster-0",
		ReadInstances:      []string{"cluster-1", "cluster-2"},
		ReadyInstances:     3,
		RegisteredReplicas: 2,
		LastSnapshotTime:   &now,
		Validation: &ValidationStatus{
			LastConnectivityTest: &now,
			ReplicationLagMs:     100,
			AllReplicasHealthy:   true,
			Instances: []InstanceValidationStatus{
				{Name: "cluster-0", Role: "MAIN", Healthy: true},
				{Name: "cluster-1", Role: "REPLICA", Healthy: true, LagMs: 50},
			},
		},
		Conditions: []metav1.Condition{
			{
				Type:   ConditionTypeReady,
				Status: metav1.ConditionTrue,
			},
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Phase != original.Phase {
		t.Errorf("Phase mismatch: got %s, want %s", copied.Phase, original.Phase)
	}

	if len(copied.ReadInstances) != len(original.ReadInstances) {
		t.Errorf("ReadInstances count mismatch: got %d, want %d", len(copied.ReadInstances), len(original.ReadInstances))
	}

	if copied.Validation == nil {
		t.Error("Validation should not be nil")
	}

	if len(copied.Conditions) != len(original.Conditions) {
		t.Errorf("Conditions count mismatch: got %d, want %d", len(copied.Conditions), len(original.Conditions))
	}
}

func TestStorageSpec_DeepCopy(t *testing.T) {
	storageClassName := "premium"
	original := StorageSpec{
		Size:             resource.MustParse("100Gi"),
		StorageClassName: &storageClassName,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.StorageClassName == nil {
		t.Error("StorageClassName should not be nil")
	}

	if *copied.StorageClassName != *original.StorageClassName {
		t.Errorf("StorageClassName mismatch: got %s, want %s", *copied.StorageClassName, *original.StorageClassName)
	}

	// Modify copy and verify independence
	newName := "modified"
	copied.StorageClassName = &newName
	if *original.StorageClassName == "modified" {
		t.Error("DeepCopy should create independent copy")
	}
}

func TestSnapshotSpec_DeepCopy(t *testing.T) {
	original := SnapshotSpec{
		Enabled:        true,
		Schedule:       "0 0 * * *",
		RetentionCount: 7,
		S3: &S3BackupSpec{
			Enabled: true,
			Bucket:  "backup",
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.S3 == nil {
		t.Error("S3 should not be nil")
	}

	if copied.S3.Bucket != original.S3.Bucket {
		t.Errorf("Bucket mismatch: got %s, want %s", copied.S3.Bucket, original.S3.Bucket)
	}
}

func TestS3BackupSpec_DeepCopy(t *testing.T) {
	secretRef := &corev1.LocalObjectReference{Name: "s3-creds"}
	original := S3BackupSpec{
		Enabled:       true,
		Bucket:        "my-bucket",
		Region:        "eu-west-1",
		Endpoint:      "https://s3.example.com",
		Prefix:        "backups/",
		SecretRef:     secretRef,
		RetentionDays: 14,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.SecretRef == nil {
		t.Error("SecretRef should not be nil")
	}

	if copied.SecretRef.Name != original.SecretRef.Name {
		t.Errorf("SecretRef.Name mismatch: got %s, want %s", copied.SecretRef.Name, original.SecretRef.Name)
	}
}

func TestHighAvailabilitySpec_DeepCopy(t *testing.T) {
	original := HighAvailabilitySpec{
		Enabled:                true,
		FailoverTimeoutSeconds: 60,
		PreferredMain:          "node-0",
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Enabled != original.Enabled {
		t.Error("Enabled mismatch")
	}

	if copied.FailoverTimeoutSeconds != original.FailoverTimeoutSeconds {
		t.Errorf("FailoverTimeoutSeconds mismatch: got %d, want %d", copied.FailoverTimeoutSeconds, original.FailoverTimeoutSeconds)
	}

	if copied.PreferredMain != original.PreferredMain {
		t.Errorf("PreferredMain mismatch: got %s, want %s", copied.PreferredMain, original.PreferredMain)
	}
}

func TestValidationStatus_DeepCopy(t *testing.T) {
	now := metav1.Now()
	original := ValidationStatus{
		LastConnectivityTest: &now,
		LastReplicationTest:  &now,
		ReplicationLagMs:     200,
		AllReplicasHealthy:   false,
		Instances: []InstanceValidationStatus{
			{Name: "pod-0", Role: "MAIN", Healthy: true, LagMs: 0, LastTestTime: &now},
			{Name: "pod-1", Role: "REPLICA", Healthy: false, LagMs: 200, LastTestTime: &now},
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if len(copied.Instances) != len(original.Instances) {
		t.Errorf("Instances count mismatch: got %d, want %d", len(copied.Instances), len(original.Instances))
	}

	if copied.ReplicationLagMs != original.ReplicationLagMs {
		t.Errorf("ReplicationLagMs mismatch: got %d, want %d", copied.ReplicationLagMs, original.ReplicationLagMs)
	}
}

func TestReplicationModeConstants(t *testing.T) {
	if ReplicationModeAsync != "ASYNC" {
		t.Errorf("ReplicationModeAsync = %s, want ASYNC", ReplicationModeAsync)
	}

	if ReplicationModeSync != "SYNC" {
		t.Errorf("ReplicationModeSync = %s, want SYNC", ReplicationModeSync)
	}

	if ReplicationModeStrictSync != "STRICT_SYNC" {
		t.Errorf("ReplicationModeStrictSync = %s, want STRICT_SYNC", ReplicationModeStrictSync)
	}
}

func TestClusterPhaseConstants(t *testing.T) {
	if ClusterPhasePending != "Pending" {
		t.Errorf("ClusterPhasePending = %s, want Pending", ClusterPhasePending)
	}

	if ClusterPhaseInitializing != "Initializing" {
		t.Errorf("ClusterPhaseInitializing = %s, want Initializing", ClusterPhaseInitializing)
	}

	if ClusterPhaseRunning != "Running" {
		t.Errorf("ClusterPhaseRunning = %s, want Running", ClusterPhaseRunning)
	}

	if ClusterPhaseFailed != "Failed" {
		t.Errorf("ClusterPhaseFailed = %s, want Failed", ClusterPhaseFailed)
	}
}

func TestConditionTypeConstants(t *testing.T) {
	if ConditionTypeReady != "Ready" {
		t.Errorf("ConditionTypeReady = %s, want Ready", ConditionTypeReady)
	}

	if ConditionTypeMainAvailable != "MainAvailable" {
		t.Errorf("ConditionTypeMainAvailable = %s, want MainAvailable", ConditionTypeMainAvailable)
	}

	if ConditionTypeReplicasReady != "ReplicasReady" {
		t.Errorf("ConditionTypeReplicasReady = %s, want ReplicasReady", ConditionTypeReplicasReady)
	}

	if ConditionTypeReplicationHealthy != "ReplicationHealthy" {
		t.Errorf("ConditionTypeReplicationHealthy = %s, want ReplicationHealthy", ConditionTypeReplicationHealthy)
	}

	if ConditionTypeReplicationLagAcceptable != "ReplicationLagAcceptable" {
		t.Errorf("ConditionTypeReplicationLagAcceptable = %s, want ReplicationLagAcceptable", ConditionTypeReplicationLagAcceptable)
	}
}

func TestMemgraphConfig_DeepCopy(t *testing.T) {
	original := MemgraphConfig{
		LogLevel:         "DEBUG",
		MemoryLimit:      8000,
		WALFlushEveryNTx: 100000,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.LogLevel != original.LogLevel {
		t.Errorf("LogLevel mismatch: got %s, want %s", copied.LogLevel, original.LogLevel)
	}

	if copied.MemoryLimit != original.MemoryLimit {
		t.Errorf("MemoryLimit mismatch: got %d, want %d", copied.MemoryLimit, original.MemoryLimit)
	}
}

func TestReplicationSpec_DeepCopy(t *testing.T) {
	original := ReplicationSpec{
		Mode: ReplicationModeSync,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Mode != original.Mode {
		t.Errorf("Mode mismatch: got %s, want %s", copied.Mode, original.Mode)
	}
}

func TestReplicationHealth_DeepCopy(t *testing.T) {
	original := ReplicationHealth{
		TotalReplicas:   3,
		HealthyReplicas: 2,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.TotalReplicas != original.TotalReplicas {
		t.Errorf("TotalReplicas mismatch: got %d, want %d", copied.TotalReplicas, original.TotalReplicas)
	}
}

func TestServiceNamesSpec_DeepCopy(t *testing.T) {
	original := ServiceNamesSpec{
		HeadlessSuffix: "-headless",
		WriteSuffix:    "-primary",
		ReadSuffix:     "-replica",
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.HeadlessSuffix != original.HeadlessSuffix {
		t.Errorf("HeadlessSuffix mismatch")
	}
}

func TestInstanceValidationStatus_DeepCopy(t *testing.T) {
	now := metav1.Now()
	original := InstanceValidationStatus{
		Name:         "pod-0",
		Role:         "MAIN",
		Healthy:      true,
		LagMs:        100,
		LastTestTime: &now,
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Name != original.Name {
		t.Errorf("Name mismatch")
	}

	if copied.LastTestTime == nil {
		t.Error("LastTestTime should not be nil")
	}
}

func TestDeepCopyWithNilValues(t *testing.T) {
	// Test cluster with nil optional fields
	original := &MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: MemgraphClusterSpec{
			Replicas:         3,
			HighAvailability: nil,
			ServiceNames:     nil,
			Affinity:         nil,
		},
		Status: MemgraphClusterStatus{
			Phase:            ClusterPhasePending,
			Validation:       nil,
			LastSnapshotTime: nil,
		},
	}

	copied := original.DeepCopy()

	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.Spec.HighAvailability != nil {
		t.Error("HighAvailability should be nil")
	}

	if copied.Status.Validation != nil {
		t.Error("Validation should be nil")
	}
}

func TestMemgraphClusterList_DeepCopyObject(t *testing.T) {
	original := &MemgraphClusterList{
		Items: []MemgraphCluster{
			{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"}},
		},
	}

	copied := original.DeepCopyObject()
	if copied == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	list, ok := copied.(*MemgraphClusterList)
	if !ok {
		t.Error("DeepCopyObject should return *MemgraphClusterList")
	}

	if len(list.Items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(list.Items))
	}
}

func TestDeepCopyNilReceivers(t *testing.T) {
	// Test DeepCopy with nil receivers - these hit the nil check branches
	// in generated DeepCopy methods

	var nilCluster *MemgraphCluster
	if nilCluster.DeepCopy() != nil {
		t.Error("DeepCopy on nil MemgraphCluster should return nil")
	}

	var nilList *MemgraphClusterList
	if nilList.DeepCopy() != nil {
		t.Error("DeepCopy on nil MemgraphClusterList should return nil")
	}

	var nilSpec *MemgraphClusterSpec
	if nilSpec.DeepCopy() != nil {
		t.Error("DeepCopy on nil MemgraphClusterSpec should return nil")
	}

	var nilStatus *MemgraphClusterStatus
	if nilStatus.DeepCopy() != nil {
		t.Error("DeepCopy on nil MemgraphClusterStatus should return nil")
	}

	var nilHA *HighAvailabilitySpec
	if nilHA.DeepCopy() != nil {
		t.Error("DeepCopy on nil HighAvailabilitySpec should return nil")
	}

	var nilConfig *MemgraphConfig
	if nilConfig.DeepCopy() != nil {
		t.Error("DeepCopy on nil MemgraphConfig should return nil")
	}

	var nilRepl *ReplicationSpec
	if nilRepl.DeepCopy() != nil {
		t.Error("DeepCopy on nil ReplicationSpec should return nil")
	}

	var nilReplHealth *ReplicationHealth
	if nilReplHealth.DeepCopy() != nil {
		t.Error("DeepCopy on nil ReplicationHealth should return nil")
	}

	var nilSvcNames *ServiceNamesSpec
	if nilSvcNames.DeepCopy() != nil {
		t.Error("DeepCopy on nil ServiceNamesSpec should return nil")
	}

	var nilS3 *S3BackupSpec
	if nilS3.DeepCopy() != nil {
		t.Error("DeepCopy on nil S3BackupSpec should return nil")
	}

	var nilSnapshot *SnapshotSpec
	if nilSnapshot.DeepCopy() != nil {
		t.Error("DeepCopy on nil SnapshotSpec should return nil")
	}

	var nilStorage *StorageSpec
	if nilStorage.DeepCopy() != nil {
		t.Error("DeepCopy on nil StorageSpec should return nil")
	}

	var nilIVStatus *InstanceValidationStatus
	if nilIVStatus.DeepCopy() != nil {
		t.Error("DeepCopy on nil InstanceValidationStatus should return nil")
	}

	var nilValStatus *ValidationStatus
	if nilValStatus.DeepCopy() != nil {
		t.Error("DeepCopy on nil ValidationStatus should return nil")
	}
}

func TestDeepCopyObjectNilReceivers(t *testing.T) {
	// Test DeepCopyObject with nil receivers
	var nilCluster *MemgraphCluster
	if nilCluster.DeepCopyObject() != nil {
		t.Error("DeepCopyObject on nil MemgraphCluster should return nil")
	}

	var nilList *MemgraphClusterList
	if nilList.DeepCopyObject() != nil {
		t.Error("DeepCopyObject on nil MemgraphClusterList should return nil")
	}
}

func TestAllDeepCopyFunctions(t *testing.T) {
	// Test all remaining DeepCopy functions

	// HighAvailabilitySpec
	haSpec := HighAvailabilitySpec{Enabled: true, PreferredMain: "pod-0"}
	if haCopy := haSpec.DeepCopy(); haCopy == nil {
		t.Error("HighAvailabilitySpec.DeepCopy returned nil")
	}

	// MemgraphConfig
	config := MemgraphConfig{LogLevel: "INFO"}
	if configCopy := config.DeepCopy(); configCopy == nil {
		t.Error("MemgraphConfig.DeepCopy returned nil")
	}

	// ReplicationSpec
	replSpec := ReplicationSpec{Mode: ReplicationModeAsync}
	if replCopy := replSpec.DeepCopy(); replCopy == nil {
		t.Error("ReplicationSpec.DeepCopy returned nil")
	}

	// ReplicationHealth
	replHealth := ReplicationHealth{TotalReplicas: 3}
	if rhCopy := replHealth.DeepCopy(); rhCopy == nil {
		t.Error("ReplicationHealth.DeepCopy returned nil")
	}

	// ServiceNamesSpec
	svcNames := ServiceNamesSpec{HeadlessSuffix: "-hl"}
	if svcCopy := svcNames.DeepCopy(); svcCopy == nil {
		t.Error("ServiceNamesSpec.DeepCopy returned nil")
	}

	// S3BackupSpec with SecretRef
	secretRef := &corev1.LocalObjectReference{Name: "secret"}
	s3Spec := S3BackupSpec{Enabled: true, Bucket: "bucket", SecretRef: secretRef}
	if s3Copy := s3Spec.DeepCopy(); s3Copy == nil {
		t.Error("S3BackupSpec.DeepCopy returned nil")
	}

	// SnapshotSpec
	snapshotSpec := SnapshotSpec{Enabled: true, Schedule: "0 * * * *"}
	if snapCopy := snapshotSpec.DeepCopy(); snapCopy == nil {
		t.Error("SnapshotSpec.DeepCopy returned nil")
	}

	// StorageSpec
	storageClass := "fast"
	storageSpec := StorageSpec{Size: resource.MustParse("10Gi"), StorageClassName: &storageClass}
	if storageCopy := storageSpec.DeepCopy(); storageCopy == nil {
		t.Error("StorageSpec.DeepCopy returned nil")
	}

	// InstanceValidationStatus
	now := metav1.Now()
	ivStatus := InstanceValidationStatus{Name: "pod", LastTestTime: &now}
	if ivCopy := ivStatus.DeepCopy(); ivCopy == nil {
		t.Error("InstanceValidationStatus.DeepCopy returned nil")
	}

	// ValidationStatus
	valStatus := ValidationStatus{LastConnectivityTest: &now, Instances: []InstanceValidationStatus{ivStatus}}
	if valCopy := valStatus.DeepCopy(); valCopy == nil {
		t.Error("ValidationStatus.DeepCopy returned nil")
	}
}
