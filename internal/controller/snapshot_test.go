// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestBuildSnapshotCronJob(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 3,
			Image:    "memgraph/memgraph:2.21.0",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled:  true,
				Schedule: "0 */6 * * *", // Every 6 hours
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	// Verify basic properties
	if cronJob.Name != "test-cluster-snapshot" {
		t.Errorf("expected name test-cluster-snapshot, got %s", cronJob.Name)
	}

	if cronJob.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", cronJob.Namespace)
	}

	if cronJob.Spec.Schedule != "0 */6 * * *" {
		t.Errorf("expected schedule '0 */6 * * *', got %s", cronJob.Spec.Schedule)
	}

	// Verify init containers (create-snapshot)
	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container without S3, got %d", len(initContainers))
	}

	if initContainers[0].Name != "create-snapshot" {
		t.Errorf("expected init container name 'create-snapshot', got %s", initContainers[0].Name)
	}

	if initContainers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected image 'memgraph/memgraph:2.21.0', got %s", initContainers[0].Image)
	}

	// Verify main container (complete)
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Name != "complete" {
		t.Errorf("expected container name 'complete', got %s", containers[0].Name)
	}
}

func TestBuildSnapshotCronJobWithS3(t *testing.T) {
	secretRef := &corev1.LocalObjectReference{Name: "s3-credentials"}
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 3,
			Image:    "memgraph/memgraph:2.21.0",
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled:  true,
				Schedule: "*/15 * * * *",
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled:   true,
					Bucket:    "my-backup-bucket",
					Region:    "us-west-2",
					Prefix:    "memgraph/backups",
					SecretRef: secretRef,
				},
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	// Verify init containers (should have 2: create-snapshot and copy-snapshots)
	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 2 {
		t.Fatalf("expected 2 init containers with S3, got %d", len(initContainers))
	}

	if initContainers[0].Name != "create-snapshot" {
		t.Errorf("expected first init container name 'create-snapshot', got %s", initContainers[0].Name)
	}

	if initContainers[1].Name != "copy-snapshots" {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}

	// Verify main container is s3-upload
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Name != "s3-upload" {
		t.Errorf("expected container name 's3-upload', got %s", containers[0].Name)
	}

	if containers[0].Image != defaultAWSCLIImage {
		t.Errorf("expected image '%s', got %s", defaultAWSCLIImage, containers[0].Image)
	}

	// Verify S3 environment variables are set
	envVars := containers[0].Env

	// Check for AWS credentials env vars
	var hasAccessKey, hasSecretKey, hasRegion bool
	for _, env := range envVars {
		if env.Name == "AWS_ACCESS_KEY_ID" {
			hasAccessKey = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Error("AWS_ACCESS_KEY_ID should be from secret")
			}
		}
		if env.Name == "AWS_SECRET_ACCESS_KEY" {
			hasSecretKey = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Error("AWS_SECRET_ACCESS_KEY should be from secret")
			}
		}
		if env.Name == "AWS_REGION" && env.Value == "us-west-2" {
			hasRegion = true
		}
	}

	if !hasAccessKey {
		t.Error("expected AWS_ACCESS_KEY_ID env var")
	}
	if !hasSecretKey {
		t.Error("expected AWS_SECRET_ACCESS_KEY env var")
	}
	if !hasRegion {
		t.Error("expected AWS_REGION env var with value us-west-2")
	}

	// Verify shared volume exists
	volumes := cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes
	var hasSnapshotDataVolume bool
	for _, vol := range volumes {
		if vol.Name == snapshotDataVolume {
			hasSnapshotDataVolume = true
			if vol.EmptyDir == nil {
				t.Error("expected snapshot-data volume to be EmptyDir")
			}
		}
	}
	if !hasSnapshotDataVolume {
		t.Error("expected snapshot-data volume")
	}
}

func TestBuildSnapshotCronJobDefaults(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
				// No schedule specified - should use default
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	// Verify default schedule
	if cronJob.Spec.Schedule != "*/15 * * * *" {
		t.Errorf("expected default schedule '*/15 * * * *', got %s", cronJob.Spec.Schedule)
	}

	// Verify default image for init container
	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}

	if initContainers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected default image 'memgraph/memgraph:2.21.0', got %s", initContainers[0].Image)
	}
}

func TestBuildSnapshotInitContainers(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
			},
		},
	}

	initContainers := buildSnapshotInitContainers(cluster, "memgraph/memgraph:2.21.0")

	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container without S3, got %d", len(initContainers))
	}

	// Verify the create-snapshot container command references write service
	args := initContainers[0].Args
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}

	// Command should contain the write service name
	if !strings.Contains(args[0], "my-cluster-write") {
		t.Error("expected command to contain 'my-cluster-write'")
	}

	// Command should contain CREATE SNAPSHOT
	if !strings.Contains(args[0], "CREATE SNAPSHOT") {
		t.Error("expected command to contain 'CREATE SNAPSHOT'")
	}
}

func TestBuildSnapshotInitContainersWithS3(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled: true,
					Bucket:  "backup-bucket",
				},
			},
		},
	}

	initContainers := buildSnapshotInitContainers(cluster, "memgraph/memgraph:2.21.0")

	if len(initContainers) != 2 {
		t.Fatalf("expected 2 init containers with S3, got %d", len(initContainers))
	}

	// Second container should be copy-snapshots using bitnami/kubectl
	if initContainers[1].Name != "copy-snapshots" {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}

	if initContainers[1].Image != "bitnami/kubectl:latest" {
		t.Errorf("expected image 'bitnami/kubectl:latest', got %s", initContainers[1].Image)
	}

	// Verify volume mount
	if len(initContainers[1].VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(initContainers[1].VolumeMounts))
	}

	if initContainers[1].VolumeMounts[0].Name != snapshotDataVolume {
		t.Errorf("expected volume mount name '%s', got %s", snapshotDataVolume, initContainers[1].VolumeMounts[0].Name)
	}
}

func TestBuildS3UploadContainer(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled:  true,
					Bucket:   "backup-bucket",
					Prefix:   "memgraph/snapshots",
					Endpoint: "https://minio.local:9000",
				},
			},
		},
	}

	container := buildS3UploadContainer(cluster)

	// Verify container name and image
	if container.Name != "s3-upload" {
		t.Errorf("expected container name 's3-upload', got %s", container.Name)
	}

	if container.Image != defaultAWSCLIImage {
		t.Errorf("expected image '%s', got %s", defaultAWSCLIImage, container.Image)
	}

	// Verify command contains bucket
	if len(container.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(container.Args))
	}

	if !strings.Contains(container.Args[0], "backup-bucket") {
		t.Error("expected command to contain 'backup-bucket'")
	}

	// Verify aws s3 cp command
	if !strings.Contains(container.Args[0], "aws s3 cp") {
		t.Error("expected command to contain 'aws s3 cp'")
	}

	// Verify volume mount
	if len(container.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(container.VolumeMounts))
	}

	if container.VolumeMounts[0].Name != snapshotDataVolume {
		t.Errorf("expected volume mount name '%s', got %s", snapshotDataVolume, container.VolumeMounts[0].Name)
	}
}

func TestBuildS3EndpointConfig(t *testing.T) {
	tests := []struct {
		name     string
		s3       *memgraphv1alpha1.S3BackupSpec
		expected string
	}{
		{
			name:     "no endpoint",
			s3:       &memgraphv1alpha1.S3BackupSpec{},
			expected: "",
		},
		{
			name: "with endpoint",
			s3: &memgraphv1alpha1.S3BackupSpec{
				Endpoint: "https://minio.local:9000",
			},
			expected: `export AWS_ENDPOINT_URL="https://minio.local:9000"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildS3EndpointConfig(tt.s3)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildS3Env(t *testing.T) {
	secretRef := &corev1.LocalObjectReference{Name: "s3-creds"}
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled:   true,
					Region:    "us-east-1",
					SecretRef: secretRef,
				},
			},
		},
	}

	envVars := buildS3Env(cluster)

	// Should have 3 env vars: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION
	if len(envVars) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(envVars))
	}

	envMap := make(map[string]corev1.EnvVar)
	for _, env := range envVars {
		envMap[env.Name] = env
	}

	// Check AWS_ACCESS_KEY_ID
	if env, ok := envMap["AWS_ACCESS_KEY_ID"]; !ok {
		t.Error("expected AWS_ACCESS_KEY_ID")
	} else if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
		t.Error("AWS_ACCESS_KEY_ID should be from secret")
	} else if env.ValueFrom.SecretKeyRef.Key != "access-key-id" {
		t.Errorf("expected key 'access-key-id', got %s", env.ValueFrom.SecretKeyRef.Key)
	}

	// Check AWS_SECRET_ACCESS_KEY
	if env, ok := envMap["AWS_SECRET_ACCESS_KEY"]; !ok {
		t.Error("expected AWS_SECRET_ACCESS_KEY")
	} else if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
		t.Error("AWS_SECRET_ACCESS_KEY should be from secret")
	} else if env.ValueFrom.SecretKeyRef.Key != "secret-access-key" {
		t.Errorf("expected key 'secret-access-key', got %s", env.ValueFrom.SecretKeyRef.Key)
	}

	// Check AWS_REGION
	if env, ok := envMap["AWS_REGION"]; !ok {
		t.Error("expected AWS_REGION")
	} else if env.Value != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %s", env.Value)
	}
}

func TestBuildSnapshotVolumes(t *testing.T) {
	tests := []struct {
		name            string
		cluster         *memgraphv1alpha1.MemgraphCluster
		expectedVolumes int
	}{
		{
			name: "no S3 - no volumes",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
					},
				},
			},
			expectedVolumes: 0,
		},
		{
			name: "with S3 - has snapshot-data volume",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						S3: &memgraphv1alpha1.S3BackupSpec{
							Enabled: true,
							Bucket:  "test-bucket",
						},
					},
				},
			},
			expectedVolumes: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volumes := buildSnapshotVolumes(tt.cluster)
			if len(volumes) != tt.expectedVolumes {
				t.Errorf("expected %d volumes, got %d", tt.expectedVolumes, len(volumes))
			}

			if tt.expectedVolumes > 0 {
				if volumes[0].Name != snapshotDataVolume {
					t.Errorf("expected volume name '%s', got %s", snapshotDataVolume, volumes[0].Name)
				}
				if volumes[0].EmptyDir == nil {
					t.Error("expected EmptyDir volume source")
				}
			}
		})
	}
}
