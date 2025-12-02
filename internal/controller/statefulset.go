// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const (
	// Container and port names
	memgraphContainerName = "memgraph"
	boltPortName          = "bolt"
	boltPort              = 7687
	replicationPortName   = "replication"
	replicationPort       = 10000

	// Volume names
	dataVolumeName = "data"
	dataVolumePath = "/var/lib/memgraph"

	// Default values
	defaultTerminationGracePeriod = int64(300) // 5 minutes
	defaultMemgraphImage          = "memgraph/memgraph:2.21.0"
	defaultStorageSize            = "10Gi"
	defaultMemoryLimit            = 3000
	defaultLogLevel               = "WARNING"
	defaultWALFlushEveryNTx       = 100000
)

// Memgraph user/group (as variables so we can take addresses)
var (
	memgraphUID = int64(999)
	memgraphGID = int64(999)
)

// buildStatefulSet creates a StatefulSet for the MemgraphCluster
func buildStatefulSet(cluster *memgraphv1alpha1.MemgraphCluster) *appsv1.StatefulSet {
	labels := labelsForCluster(cluster)
	replicas := cluster.Spec.Replicas
	if replicas == 0 {
		replicas = 3
	}

	// Get image
	image := cluster.Spec.Image
	if image == "" {
		image = defaultMemgraphImage
	}

	// Get storage size
	storageSize := cluster.Spec.Storage.Size
	if storageSize.IsZero() {
		storageSize = resource.MustParse(defaultStorageSize)
	}

	// Build Memgraph args
	args := buildMemgraphArgs(cluster)

	// Build init containers
	initContainers := buildInitContainers()

	// Build preStop hook for graceful shutdown with snapshot
	preStopHook := &corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{
			Command: []string{
				"/bin/bash",
				"-c",
				`echo "Creating snapshot before shutdown..."
echo "CREATE SNAPSHOT;" | mgconsole --host 127.0.0.1 --port 7687 --use-ssl=false
echo "Snapshot created, proceeding with shutdown"
sleep 5`,
			},
		},
	}

	terminationGracePeriod := defaultTerminationGracePeriod

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: headlessServiceName(cluster),
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGracePeriod,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &memgraphGID,
					},
					InitContainers: initContainers,
					Containers: []corev1.Container{
						{
							Name:            memgraphContainerName,
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/usr/lib/memgraph/memgraph"},
							Args:            args,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &memgraphUID,
								RunAsGroup: &memgraphGID,
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          boltPortName,
									ContainerPort: boltPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          replicationPortName,
									ContainerPort: replicationPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: cluster.Spec.Resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      dataVolumeName,
									MountPath: dataVolumePath,
								},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: preStopHook,
							},
							StartupProbe:   buildStartupProbe(),
							LivenessProbe:  buildLivenessProbe(),
							ReadinessProbe: buildReadinessProbe(),
						},
					},
					NodeSelector: cluster.Spec.NodeSelector,
					Tolerations:  cluster.Spec.Tolerations,
					Affinity:     cluster.Spec.Affinity,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: dataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						StorageClassName: cluster.Spec.Storage.StorageClassName,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: storageSize,
							},
						},
					},
				},
			},
		},
	}

	return sts
}

// buildMemgraphArgs builds the command-line arguments for Memgraph
func buildMemgraphArgs(cluster *memgraphv1alpha1.MemgraphCluster) []string {
	logLevel := cluster.Spec.Config.LogLevel
	if logLevel == "" {
		logLevel = defaultLogLevel
	}

	memoryLimit := cluster.Spec.Config.MemoryLimit
	if memoryLimit == 0 {
		memoryLimit = defaultMemoryLimit
	}

	walFlushEveryNTx := cluster.Spec.Config.WALFlushEveryNTx
	if walFlushEveryNTx == 0 {
		walFlushEveryNTx = defaultWALFlushEveryNTx
	}

	return []string{
		fmt.Sprintf("--log-level=%s", logLevel),
		fmt.Sprintf("--log-file=%s/memgraph.log", dataVolumePath),
		fmt.Sprintf("--memory-limit=%d", memoryLimit),
		fmt.Sprintf("--storage-wal-file-flush-every-n-tx=%d", walFlushEveryNTx),
	}
}

// buildInitContainers builds the init containers for the pod
func buildInitContainers() []corev1.Container {
	privileged := true
	runAsRoot := int64(0)

	return []corev1.Container{
		// Sysctl init container - sets vm.max_map_count
		{
			Name:            "sysctl-init",
			Image:           "busybox:1.36",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				"sysctl -w vm.max_map_count=262144",
			},
			SecurityContext: &corev1.SecurityContext{
				Privileged: &privileged,
				RunAsUser:  &runAsRoot,
			},
		},
		// Volume permissions init container
		{
			Name:            "volume-permissions",
			Image:           "busybox:1.36",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				fmt.Sprintf("chown -R %d:%d %s && chmod -R 755 %s",
					memgraphUID, memgraphGID, dataVolumePath, dataVolumePath),
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: &runAsRoot,
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      dataVolumeName,
					MountPath: dataVolumePath,
				},
			},
		},
	}
}

// buildStartupProbe builds the startup probe for the Memgraph container
func buildStartupProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromString(boltPortName),
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
		FailureThreshold:    30,
	}
}

// buildLivenessProbe builds the liveness probe for the Memgraph container
func buildLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromString(boltPortName),
			},
		},
		InitialDelaySeconds: 60,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    5,
	}
}

// buildReadinessProbe builds the readiness probe for the Memgraph container
func buildReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromString(boltPortName),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
		FailureThreshold:    5,
	}
}
