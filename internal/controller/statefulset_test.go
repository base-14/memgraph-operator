// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestBuildStatefulSet(t *testing.T) {
	tests := []struct {
		name             string
		cluster          *memgraphv1alpha1.MemgraphCluster
		expectedReplicas int32
		expectedImage    string
	}{
		{
			name: "default values",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			expectedReplicas: 3,
			expectedImage:    defaultMemgraphImage,
		},
		{
			name: "custom replicas and image",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "custom-ns",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Replicas: 5,
					Image:    "memgraph/memgraph:3.0.0",
				},
			},
			expectedReplicas: 5,
			expectedImage:    "memgraph/memgraph:3.0.0",
		},
		{
			name: "with custom storage",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Replicas: 3,
					Storage: memgraphv1alpha1.StorageSpec{
						Size: resource.MustParse("50Gi"),
					},
				},
			},
			expectedReplicas: 3,
			expectedImage:    defaultMemgraphImage,
		},
		{
			name: "with node selector and tolerations",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Replicas: 3,
					NodeSelector: map[string]string{
						"disktype": "ssd",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "dedicated",
							Operator: corev1.TolerationOpEqual,
							Value:    "memgraph",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedReplicas: 3,
			expectedImage:    defaultMemgraphImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := buildStatefulSet(tt.cluster)

			// Verify basic properties
			if sts.Name != tt.cluster.Name {
				t.Errorf("Name = %s, want %s", sts.Name, tt.cluster.Name)
			}

			if sts.Namespace != tt.cluster.Namespace {
				t.Errorf("Namespace = %s, want %s", sts.Namespace, tt.cluster.Namespace)
			}

			if *sts.Spec.Replicas != tt.expectedReplicas {
				t.Errorf("Replicas = %d, want %d", *sts.Spec.Replicas, tt.expectedReplicas)
			}

			// Verify container image
			if len(sts.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("No containers found")
			}

			if sts.Spec.Template.Spec.Containers[0].Image != tt.expectedImage {
				t.Errorf("Image = %s, want %s", sts.Spec.Template.Spec.Containers[0].Image, tt.expectedImage)
			}

			// Verify init containers exist (sysctl-init, clear-replication-state, volume-permissions)
			if len(sts.Spec.Template.Spec.InitContainers) != 3 {
				t.Errorf("InitContainers count = %d, want 3", len(sts.Spec.Template.Spec.InitContainers))
			}

			// Verify ports
			container := sts.Spec.Template.Spec.Containers[0]
			if len(container.Ports) != 2 {
				t.Errorf("Ports count = %d, want 2", len(container.Ports))
			}

			foundBolt := false
			foundReplication := false
			for _, port := range container.Ports {
				if port.Name == boltPortName && port.ContainerPort == boltPort {
					foundBolt = true
				}
				if port.Name == replicationPortName && port.ContainerPort == replicationPort {
					foundReplication = true
				}
			}

			if !foundBolt {
				t.Error("Bolt port not found")
			}
			if !foundReplication {
				t.Error("Replication port not found")
			}

			// Verify probes exist
			if container.StartupProbe == nil {
				t.Error("StartupProbe is nil")
			}
			if container.LivenessProbe == nil {
				t.Error("LivenessProbe is nil")
			}
			if container.ReadinessProbe == nil {
				t.Error("ReadinessProbe is nil")
			}

			// Verify volume claims
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				t.Errorf("VolumeClaimTemplates count = %d, want 1", len(sts.Spec.VolumeClaimTemplates))
			}

			// Verify node selector if set
			if tt.cluster.Spec.NodeSelector != nil {
				if sts.Spec.Template.Spec.NodeSelector == nil {
					t.Error("NodeSelector not set")
				}
			}

			// Verify tolerations if set
			if len(tt.cluster.Spec.Tolerations) > 0 {
				if len(sts.Spec.Template.Spec.Tolerations) != len(tt.cluster.Spec.Tolerations) {
					t.Errorf("Tolerations count = %d, want %d", len(sts.Spec.Template.Spec.Tolerations), len(tt.cluster.Spec.Tolerations))
				}
			}

			// Verify labels
			labels := sts.Labels
			if labels["app.kubernetes.io/name"] != "memgraph" {
				t.Errorf("Label app.kubernetes.io/name = %s, want memgraph", labels["app.kubernetes.io/name"])
			}
		})
	}
}

func TestBuildMemgraphArgs(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *memgraphv1alpha1.MemgraphCluster
		expectedArgs   []string
		expectedLogLvl string
	}{
		{
			name: "default args",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			expectedLogLvl: "WARNING",
		},
		{
			name: "custom log level",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Config: memgraphv1alpha1.MemgraphConfig{
						LogLevel: "DEBUG",
					},
				},
			},
			expectedLogLvl: "DEBUG",
		},
		{
			name: "custom memory limit",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Config: memgraphv1alpha1.MemgraphConfig{
						MemoryLimit: 5000,
					},
				},
			},
			expectedLogLvl: "WARNING",
		},
		{
			name: "custom wal flush",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Config: memgraphv1alpha1.MemgraphConfig{
						WALFlushEveryNTx: 50000,
					},
				},
			},
			expectedLogLvl: "WARNING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMemgraphArgs(tt.cluster)

			// Verify we have 4 args
			if len(args) != 4 {
				t.Errorf("Args count = %d, want 4", len(args))
			}

			// Find log level arg
			foundLogLevel := false
			for _, arg := range args {
				if arg == "--log-level="+tt.expectedLogLvl {
					foundLogLevel = true
					break
				}
			}

			if !foundLogLevel {
				t.Errorf("Log level arg not found with expected level %s, got args: %v", tt.expectedLogLvl, args)
			}
		})
	}
}

func TestBuildInitContainers(t *testing.T) {
	initContainers := buildInitContainers()

	if len(initContainers) != 3 {
		t.Fatalf("InitContainers count = %d, want 3", len(initContainers))
	}

	// Verify sysctl init container
	sysctlInit := initContainers[0]
	if sysctlInit.Name != "sysctl-init" {
		t.Errorf("First init container name = %s, want sysctl-init", sysctlInit.Name)
	}
	if sysctlInit.SecurityContext == nil || !*sysctlInit.SecurityContext.Privileged {
		t.Error("sysctl-init should be privileged")
	}

	// Verify clear-replication-state init container
	clearReplInit := initContainers[1]
	if clearReplInit.Name != "clear-replication-state" {
		t.Errorf("Second init container name = %s, want clear-replication-state", clearReplInit.Name)
	}
	if len(clearReplInit.VolumeMounts) != 1 {
		t.Errorf("clear-replication-state VolumeMounts count = %d, want 1", len(clearReplInit.VolumeMounts))
	}

	// Verify volume permissions init container
	volPermsInit := initContainers[2]
	if volPermsInit.Name != "volume-permissions" {
		t.Errorf("Third init container name = %s, want volume-permissions", volPermsInit.Name)
	}
	if len(volPermsInit.VolumeMounts) != 1 {
		t.Errorf("volume-permissions VolumeMounts count = %d, want 1", len(volPermsInit.VolumeMounts))
	}
}

func TestBuildStartupProbe(t *testing.T) {
	probe := buildStartupProbe()

	if probe == nil {
		t.Fatal("Probe is nil")
	}

	if probe.TCPSocket == nil {
		t.Error("TCPSocket is nil")
	}

	if probe.TCPSocket.Port.String() != boltPortName {
		t.Errorf("Port = %s, want %s", probe.TCPSocket.Port.String(), boltPortName)
	}

	if probe.InitialDelaySeconds != 10 {
		t.Errorf("InitialDelaySeconds = %d, want 10", probe.InitialDelaySeconds)
	}

	if probe.FailureThreshold != 30 {
		t.Errorf("FailureThreshold = %d, want 30", probe.FailureThreshold)
	}
}

func TestBuildLivenessProbe(t *testing.T) {
	probe := buildLivenessProbe()

	if probe == nil {
		t.Fatal("Probe is nil")
	}

	if probe.TCPSocket == nil {
		t.Error("TCPSocket is nil")
	}

	if probe.TCPSocket.Port.String() != boltPortName {
		t.Errorf("Port = %s, want %s", probe.TCPSocket.Port.String(), boltPortName)
	}

	if probe.InitialDelaySeconds != 60 {
		t.Errorf("InitialDelaySeconds = %d, want 60", probe.InitialDelaySeconds)
	}

	if probe.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", probe.FailureThreshold)
	}
}

func TestBuildReadinessProbe(t *testing.T) {
	probe := buildReadinessProbe()

	if probe == nil {
		t.Fatal("Probe is nil")
	}

	if probe.TCPSocket == nil {
		t.Error("TCPSocket is nil")
	}

	if probe.TCPSocket.Port.String() != boltPortName {
		t.Errorf("Port = %s, want %s", probe.TCPSocket.Port.String(), boltPortName)
	}

	if probe.InitialDelaySeconds != 30 {
		t.Errorf("InitialDelaySeconds = %d, want 30", probe.InitialDelaySeconds)
	}

	if probe.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", probe.FailureThreshold)
	}
}

func TestBuildStatefulSetWithStorageClass(t *testing.T) {
	storageClassName := "fast-ssd"
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 3,
			Storage: memgraphv1alpha1.StorageSpec{
				Size:             resource.MustParse("100Gi"),
				StorageClassName: &storageClassName,
			},
		},
	}

	sts := buildStatefulSet(cluster)

	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("VolumeClaimTemplates count = %d, want 1", len(sts.Spec.VolumeClaimTemplates))
	}

	pvc := sts.Spec.VolumeClaimTemplates[0]
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != storageClassName {
		t.Errorf("StorageClassName = %v, want %s", pvc.Spec.StorageClassName, storageClassName)
	}

	// Verify storage size
	storageRequest := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageRequest.String() != "100Gi" {
		t.Errorf("Storage size = %s, want 100Gi", storageRequest.String())
	}
}

func TestBuildStatefulSetWithResources(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 3,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
	}

	sts := buildStatefulSet(cluster)

	container := sts.Spec.Template.Spec.Containers[0]

	// Verify requests
	cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
	if cpuRequest.String() != "1" {
		t.Errorf("CPU request = %s, want 1", cpuRequest.String())
	}

	memRequest := container.Resources.Requests[corev1.ResourceMemory]
	if memRequest.String() != "4Gi" {
		t.Errorf("Memory request = %s, want 4Gi", memRequest.String())
	}

	// Verify limits
	cpuLimit := container.Resources.Limits[corev1.ResourceCPU]
	if cpuLimit.String() != "2" {
		t.Errorf("CPU limit = %s, want 2", cpuLimit.String())
	}

	memLimit := container.Resources.Limits[corev1.ResourceMemory]
	if memLimit.String() != "8Gi" {
		t.Errorf("Memory limit = %s, want 8Gi", memLimit.String())
	}
}

func TestBuildStatefulSetWithAffinity(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 3,
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		},
	}

	sts := buildStatefulSet(cluster)

	if sts.Spec.Template.Spec.Affinity == nil {
		t.Error("Affinity is nil")
	}

	if sts.Spec.Template.Spec.Affinity.PodAntiAffinity == nil {
		t.Error("PodAntiAffinity is nil")
	}
}
