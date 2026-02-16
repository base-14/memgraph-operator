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

const (
	testContainerNameCopySnapshots = "copy-snapshots"
	testContainerNameRcloneUpload  = "rclone-upload"
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
				Schedule: "0 */6 * * *",
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	if cronJob.Name != "test-cluster-snapshot" {
		t.Errorf("expected name test-cluster-snapshot, got %s", cronJob.Name)
	}

	if cronJob.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", cronJob.Namespace)
	}

	if cronJob.Spec.Schedule != "0 */6 * * *" {
		t.Errorf("expected schedule '0 */6 * * *', got %s", cronJob.Spec.Schedule)
	}

	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container without remote backup, got %d", len(initContainers))
	}

	if initContainers[0].Name != "create-snapshot" {
		t.Errorf("expected init container name 'create-snapshot', got %s", initContainers[0].Name)
	}

	if initContainers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected image 'memgraph/memgraph:2.21.0', got %s", initContainers[0].Image)
	}

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
				Enabled:            true,
				Schedule:           "*/15 * * * *",
				ServiceAccountName: "memgraph-sa",
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

	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 2 {
		t.Fatalf("expected 2 init containers with S3, got %d", len(initContainers))
	}

	if initContainers[0].Name != "create-snapshot" {
		t.Errorf("expected first init container name 'create-snapshot', got %s", initContainers[0].Name)
	}

	if initContainers[1].Name != testContainerNameCopySnapshots {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}

	// Verify main container is rclone-upload
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Name != testContainerNameRcloneUpload {
		t.Errorf("expected container name 'rclone-upload', got %s", containers[0].Name)
	}

	if containers[0].Image != defaultRcloneImage {
		t.Errorf("expected image '%s', got %s", defaultRcloneImage, containers[0].Image)
	}

	// Verify S3 environment variables are set
	envVars := containers[0].Env
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

	// Verify service account name
	saName := cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName
	if saName != "memgraph-sa" {
		t.Errorf("expected serviceAccountName 'memgraph-sa', got %s", saName)
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

func TestBuildSnapshotCronJobWithGCS(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Replicas: 2,
			Image:    "memgraph/memgraph:3.7.2",
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled:            true,
				Schedule:           "0 * * * *",
				ServiceAccountName: "memgraph-sa",
				GCS: &memgraphv1alpha1.GCSBackupSpec{
					Enabled: true,
					Bucket:  "my-gcs-bucket",
					Prefix:  "kb/snapshots",
				},
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	// Verify init containers (should have 2: create-snapshot and copy-snapshots)
	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 2 {
		t.Fatalf("expected 2 init containers with GCS, got %d", len(initContainers))
	}

	if initContainers[1].Name != testContainerNameCopySnapshots {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}

	// Verify main container is rclone-upload
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Name != testContainerNameRcloneUpload {
		t.Errorf("expected container name 'rclone-upload', got %s", containers[0].Name)
	}

	if containers[0].Image != defaultRcloneImage {
		t.Errorf("expected image '%s', got %s", defaultRcloneImage, containers[0].Image)
	}

	// GCS uses Workload Identity - no env vars needed
	if len(containers[0].Env) != 0 {
		t.Errorf("expected 0 env vars for GCS (uses Workload Identity), got %d", len(containers[0].Env))
	}

	// Verify rclone command uses GCS backend
	if !strings.Contains(containers[0].Args[0], ":gcs:my-gcs-bucket") {
		t.Error("expected command to contain ':gcs:my-gcs-bucket'")
	}

	if !strings.Contains(containers[0].Args[0], "--gcs-env-auth") {
		t.Error("expected command to contain '--gcs-env-auth'")
	}

	if !strings.Contains(containers[0].Args[0], "kb/snapshots") {
		t.Error("expected command to contain 'kb/snapshots' prefix")
	}

	// Verify service account name
	saName := cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName
	if saName != "memgraph-sa" {
		t.Errorf("expected serviceAccountName 'memgraph-sa', got %s", saName)
	}

	// Verify shared volume
	volumes := cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes
	var hasSnapshotDataVolume bool
	for _, vol := range volumes {
		if vol.Name == snapshotDataVolume {
			hasSnapshotDataVolume = true
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
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	if cronJob.Spec.Schedule != "*/15 * * * *" {
		t.Errorf("expected default schedule '*/15 * * * *', got %s", cronJob.Spec.Schedule)
	}

	initContainers := cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}

	if initContainers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected default image 'memgraph/memgraph:2.21.0', got %s", initContainers[0].Image)
	}
}

func TestBuildSnapshotCronJobServiceAccountName(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled:            true,
				ServiceAccountName: "custom-sa",
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	saName := cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName
	if saName != "custom-sa" {
		t.Errorf("expected serviceAccountName 'custom-sa', got %s", saName)
	}
}

func TestBuildSnapshotCronJobNoServiceAccountName(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
			},
		},
	}

	cronJob := buildSnapshotCronJob(cluster)

	saName := cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName
	if saName != "" {
		t.Errorf("expected empty serviceAccountName, got %s", saName)
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
		t.Fatalf("expected 1 init container without remote backup, got %d", len(initContainers))
	}

	args := initContainers[0].Args
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}

	if !strings.Contains(args[0], "my-cluster-write") {
		t.Error("expected command to contain 'my-cluster-write'")
	}

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

	if initContainers[1].Name != testContainerNameCopySnapshots {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}

	if initContainers[1].Image != "bitnami/kubectl:latest" {
		t.Errorf("expected image 'bitnami/kubectl:latest', got %s", initContainers[1].Image)
	}

	if len(initContainers[1].VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(initContainers[1].VolumeMounts))
	}

	if initContainers[1].VolumeMounts[0].Name != snapshotDataVolume {
		t.Errorf("expected volume mount name '%s', got %s", snapshotDataVolume, initContainers[1].VolumeMounts[0].Name)
	}
}

func TestBuildSnapshotInitContainersWithGCS(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
				GCS: &memgraphv1alpha1.GCSBackupSpec{
					Enabled: true,
					Bucket:  "gcs-bucket",
				},
			},
		},
	}

	initContainers := buildSnapshotInitContainers(cluster, "memgraph/memgraph:2.21.0")

	if len(initContainers) != 2 {
		t.Fatalf("expected 2 init containers with GCS, got %d", len(initContainers))
	}

	if initContainers[1].Name != testContainerNameCopySnapshots {
		t.Errorf("expected second init container name 'copy-snapshots', got %s", initContainers[1].Name)
	}
}

func TestBuildRcloneUploadContainerS3(t *testing.T) {
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
					Region:   "us-west-2",
					Prefix:   "memgraph/snapshots",
					Endpoint: "https://minio.local:9000",
				},
			},
		},
	}

	container := buildRcloneUploadContainer(cluster, "s3")

	if container.Name != testContainerNameRcloneUpload {
		t.Errorf("expected container name 'rclone-upload', got %s", container.Name)
	}

	if container.Image != defaultRcloneImage {
		t.Errorf("expected image '%s', got %s", defaultRcloneImage, container.Image)
	}

	if len(container.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(container.Args))
	}

	if !strings.Contains(container.Args[0], "backup-bucket") {
		t.Error("expected command to contain 'backup-bucket'")
	}

	if !strings.Contains(container.Args[0], "rclone copy") {
		t.Error("expected command to contain 'rclone copy'")
	}

	if !strings.Contains(container.Args[0], "--s3-endpoint https://minio.local:9000") {
		t.Error("expected command to contain '--s3-endpoint https://minio.local:9000'")
	}

	if !strings.Contains(container.Args[0], "--s3-region us-west-2") {
		t.Error("expected command to contain '--s3-region us-west-2'")
	}

	if len(container.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(container.VolumeMounts))
	}

	if container.VolumeMounts[0].Name != snapshotDataVolume {
		t.Errorf("expected volume mount name '%s', got %s", snapshotDataVolume, container.VolumeMounts[0].Name)
	}
}

func TestBuildRcloneUploadContainerGCS(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				GCS: &memgraphv1alpha1.GCSBackupSpec{
					Enabled: true,
					Bucket:  "gcs-backup-bucket",
					Prefix:  "kb/snapshots",
				},
			},
		},
	}

	container := buildRcloneUploadContainer(cluster, "gcs")

	if container.Name != testContainerNameRcloneUpload {
		t.Errorf("expected container name 'rclone-upload', got %s", container.Name)
	}

	if container.Image != defaultRcloneImage {
		t.Errorf("expected image '%s', got %s", defaultRcloneImage, container.Image)
	}

	if !strings.Contains(container.Args[0], ":gcs:gcs-backup-bucket") {
		t.Error("expected command to contain ':gcs:gcs-backup-bucket'")
	}

	if !strings.Contains(container.Args[0], "--gcs-env-auth") {
		t.Error("expected command to contain '--gcs-env-auth'")
	}

	if !strings.Contains(container.Args[0], "kb/snapshots") {
		t.Error("expected command to contain 'kb/snapshots' prefix")
	}

	// GCS should have no env vars (uses Workload Identity)
	if len(container.Env) != 0 {
		t.Errorf("expected 0 env vars for GCS, got %d", len(container.Env))
	}
}

func TestBuildRcloneS3CommandDefaultPrefix(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled: true,
					Bucket:  "backup-bucket",
				},
			},
		},
	}

	cmd := buildRcloneS3Command(cluster)

	if !strings.Contains(cmd, "memgraph/snapshots") {
		t.Error("expected default prefix 'memgraph/snapshots' in command")
	}
}

func TestBuildRcloneGCSCommandDefaultPrefix(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				GCS: &memgraphv1alpha1.GCSBackupSpec{
					Enabled: true,
					Bucket:  "gcs-bucket",
				},
			},
		},
	}

	cmd := buildRcloneGCSCommand(cluster)

	if !strings.Contains(cmd, "memgraph/snapshots") {
		t.Error("expected default prefix 'memgraph/snapshots' in command")
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

	if len(envVars) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(envVars))
	}

	envMap := make(map[string]corev1.EnvVar)
	for _, env := range envVars {
		envMap[env.Name] = env
	}

	if env, ok := envMap["AWS_ACCESS_KEY_ID"]; !ok {
		t.Error("expected AWS_ACCESS_KEY_ID")
	} else if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
		t.Error("AWS_ACCESS_KEY_ID should be from secret")
	} else if env.ValueFrom.SecretKeyRef.Key != "access-key-id" {
		t.Errorf("expected key 'access-key-id', got %s", env.ValueFrom.SecretKeyRef.Key)
	}

	if env, ok := envMap["AWS_SECRET_ACCESS_KEY"]; !ok {
		t.Error("expected AWS_SECRET_ACCESS_KEY")
	} else if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
		t.Error("AWS_SECRET_ACCESS_KEY should be from secret")
	} else if env.ValueFrom.SecretKeyRef.Key != "secret-access-key" {
		t.Errorf("expected key 'secret-access-key', got %s", env.ValueFrom.SecretKeyRef.Key)
	}

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
			name: "no remote backup - no volumes",
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
		{
			name: "with GCS - has snapshot-data volume",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						GCS: &memgraphv1alpha1.GCSBackupSpec{
							Enabled: true,
							Bucket:  "gcs-bucket",
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

func TestBuildSnapshotMainContainers(t *testing.T) {
	tests := []struct {
		name              string
		cluster           *memgraphv1alpha1.MemgraphCluster
		expectedContainer string
	}{
		{
			name: "without remote backup - complete container",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
					},
				},
			},
			expectedContainer: "complete",
		},
		{
			name: "with S3 - rclone-upload container",
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
			expectedContainer: testContainerNameRcloneUpload,
		},
		{
			name: "with GCS - rclone-upload container",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						GCS: &memgraphv1alpha1.GCSBackupSpec{
							Enabled: true,
							Bucket:  "gcs-bucket",
						},
					},
				},
			},
			expectedContainer: testContainerNameRcloneUpload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containers := buildSnapshotMainContainers(tt.cluster)
			if len(containers) != 1 {
				t.Fatalf("expected 1 container, got %d", len(containers))
			}
			if containers[0].Name != tt.expectedContainer {
				t.Errorf("expected container name '%s', got %s", tt.expectedContainer, containers[0].Name)
			}
		})
	}
}

func TestIsRemoteBackupEnabled(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected bool
	}{
		{
			name: "no backup",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{Enabled: true},
				},
			},
			expected: false,
		},
		{
			name: "S3 enabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						S3:      &memgraphv1alpha1.S3BackupSpec{Enabled: true, Bucket: "b"},
					},
				},
			},
			expected: true,
		},
		{
			name: "GCS enabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						GCS:     &memgraphv1alpha1.GCSBackupSpec{Enabled: true, Bucket: "b"},
					},
				},
			},
			expected: true,
		},
		{
			name: "S3 present but disabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Snapshot: memgraphv1alpha1.SnapshotSpec{
						Enabled: true,
						S3:      &memgraphv1alpha1.S3BackupSpec{Enabled: false},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRemoteBackupEnabled(tt.cluster); got != tt.expected {
				t.Errorf("isRemoteBackupEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildS3EnvWithoutSecretRef(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				S3: &memgraphv1alpha1.S3BackupSpec{
					Enabled: true,
					Region:  "eu-west-1",
				},
			},
		},
	}

	envVars := buildS3Env(cluster)

	if len(envVars) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(envVars))
	}

	if envVars[0].Name != "AWS_REGION" {
		t.Errorf("expected AWS_REGION, got %s", envVars[0].Name)
	}

	if envVars[0].Value != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got %s", envVars[0].Value)
	}
}

func TestBuildS3EnvWithNilS3(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Snapshot: memgraphv1alpha1.SnapshotSpec{
				Enabled: true,
			},
		},
	}

	envVars := buildS3Env(cluster)

	if len(envVars) != 0 {
		t.Errorf("expected 0 env vars for nil S3, got %d", len(envVars))
	}
}

func TestPtr(t *testing.T) {
	b := true
	ptrB := ptr(b)
	if ptrB == nil || *ptrB != true {
		t.Error("ptr() for bool failed")
	}

	i := 42
	ptrI := ptr(i)
	if ptrI == nil || *ptrI != 42 {
		t.Error("ptr() for int failed")
	}

	s := "test"
	ptrS := ptr(s)
	if ptrS == nil || *ptrS != "test" {
		t.Error("ptr() for string failed")
	}
}

func TestNewSnapshotManager(t *testing.T) {
	sm := NewSnapshotManager(nil)
	if sm == nil {
		t.Error("NewSnapshotManager returned nil")
	}
}
