// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
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

	// Verify container
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Name != "snapshot" {
		t.Errorf("expected container name 'snapshot', got %s", containers[0].Name)
	}

	if containers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected image 'memgraph/memgraph:2.21.0', got %s", containers[0].Image)
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

	// Verify S3 environment variables are set
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

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

	// Verify default image
	containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if containers[0].Image != "memgraph/memgraph:2.21.0" {
		t.Errorf("expected default image 'memgraph/memgraph:2.21.0', got %s", containers[0].Image)
	}
}

func TestBuildSnapshotCommand(t *testing.T) {
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

	cmd := buildSnapshotCommand(cluster)

	// Verify command contains the write service
	if !contains(cmd, "my-cluster-write") {
		t.Error("expected command to contain 'my-cluster-write'")
	}

	// Verify command creates snapshot
	if !contains(cmd, "CREATE SNAPSHOT") {
		t.Error("expected command to contain 'CREATE SNAPSHOT'")
	}
}

func TestBuildS3BackupCommand(t *testing.T) {
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

	cmd := buildS3BackupCommand(cluster)

	// Verify bucket is in command
	if !contains(cmd, "backup-bucket") {
		t.Error("expected command to contain 'backup-bucket'")
	}

	// Verify aws s3 cp command
	if !contains(cmd, "aws s3 cp") {
		t.Error("expected command to contain 'aws s3 cp'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
