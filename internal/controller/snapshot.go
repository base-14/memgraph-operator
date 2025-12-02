// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

// SnapshotManager handles Memgraph snapshot operations
type SnapshotManager struct {
	mgClient *memgraph.Client
}

// NewSnapshotManager creates a new SnapshotManager
func NewSnapshotManager(mgClient *memgraph.Client) *SnapshotManager {
	return &SnapshotManager{
		mgClient: mgClient,
	}
}

// TriggerSnapshot triggers a snapshot on the main instance
func (sm *SnapshotManager) TriggerSnapshot(ctx context.Context, namespace, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Triggering snapshot", "pod", podName)

	if err := sm.mgClient.CreateSnapshot(ctx, namespace, podName); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	logger.Info("Snapshot created successfully", "pod", podName)
	return nil
}

// buildSnapshotCronJob creates a CronJob for scheduled snapshots
func buildSnapshotCronJob(cluster *memgraphv1alpha1.MemgraphCluster) *batchv1.CronJob {
	name := cluster.Name + "-snapshot"
	schedule := cluster.Spec.Snapshot.Schedule
	if schedule == "" {
		schedule = "*/15 * * * *" // Default: every 15 minutes
	}

	// Build the snapshot command
	snapshotCmd := buildSnapshotCommand(cluster)

	// Use the same image as memgraph for the snapshot job
	image := cluster.Spec.Image
	if image == "" {
		image = defaultMemgraphImage
	}

	// Determine history limits
	successfulJobsHistoryLimit := int32(3)
	failedJobsHistoryLimit := int32(1)

	// Security context
	var runAsUser int64 = 999
	var runAsGroup int64 = 999

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    labelsForCluster(cluster),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &successfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &failedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsForCluster(cluster),
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labelsForCluster(cluster),
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							SecurityContext: &corev1.PodSecurityContext{
								RunAsUser:    &runAsUser,
								RunAsGroup:   &runAsGroup,
								RunAsNonRoot: ptr(true),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Containers: []corev1.Container{
								{
									Name:    "snapshot",
									Image:   image,
									Command: []string{"/bin/bash", "-c"},
									Args:    []string{snapshotCmd},
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: ptr(false),
										ReadOnlyRootFilesystem:   ptr(true),
										Capabilities: &corev1.Capabilities{
											Drop: []corev1.Capability{"ALL"},
										},
									},
									Env: buildSnapshotEnv(cluster),
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildSnapshotCommand builds the command for creating snapshots
func buildSnapshotCommand(cluster *memgraphv1alpha1.MemgraphCluster) string {
	// Connect to the write service and create a snapshot
	writeService := cluster.Name + "-write"

	cmd := fmt.Sprintf(`
set -e
echo "Creating snapshot on %s..."
echo "CREATE SNAPSHOT;" | mgconsole --host %s --port 7687 --use-ssl=false --no-history
echo "Snapshot created successfully at $(date)"
`, writeService, writeService)

	// Add S3 backup if enabled
	if cluster.Spec.Snapshot.S3 != nil && cluster.Spec.Snapshot.S3.Enabled {
		cmd += buildS3BackupCommand(cluster)
	}

	return cmd
}

// buildS3BackupCommand builds the command for S3 backup
func buildS3BackupCommand(cluster *memgraphv1alpha1.MemgraphCluster) string {
	s3 := cluster.Spec.Snapshot.S3
	prefix := s3.Prefix
	if prefix == "" {
		prefix = "memgraph/snapshots"
	}

	// The data path in Memgraph container
	dataPath := "/var/lib/memgraph"
	timestamp := "$(date +%Y%m%d-%H%M%S)"
	backupPath := fmt.Sprintf("s3://%s/%s/%s/%s", s3.Bucket, prefix, cluster.Name, timestamp)

	cmd := fmt.Sprintf(`
echo "Backing up data to S3: %s"

# Configure AWS CLI if endpoint is specified
%s

# Upload snapshot files
aws s3 cp %s/snapshots/ %s/snapshots/ --recursive || echo "Warning: snapshot upload failed"
aws s3 cp %s/wal/ %s/wal/ --recursive || echo "Warning: WAL upload failed"

echo "S3 backup completed at $(date)"
`, backupPath, buildS3EndpointConfig(s3), dataPath, backupPath, dataPath, backupPath)

	return cmd
}

// buildS3EndpointConfig builds AWS CLI endpoint configuration
func buildS3EndpointConfig(s3 *memgraphv1alpha1.S3BackupSpec) string {
	if s3.Endpoint == "" {
		return ""
	}
	return fmt.Sprintf(`export AWS_ENDPOINT_URL="%s"`, s3.Endpoint)
}

// buildSnapshotEnv builds environment variables for the snapshot job
func buildSnapshotEnv(cluster *memgraphv1alpha1.MemgraphCluster) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	// Add S3 credentials if configured
	if cluster.Spec.Snapshot.S3 != nil && cluster.Spec.Snapshot.S3.SecretRef != nil {
		envVars = append(envVars,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *cluster.Spec.Snapshot.S3.SecretRef,
						Key:                  "access-key-id",
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *cluster.Spec.Snapshot.S3.SecretRef,
						Key:                  "secret-access-key",
					},
				},
			},
		)

		if cluster.Spec.Snapshot.S3.Region != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AWS_REGION",
				Value: cluster.Spec.Snapshot.S3.Region,
			})
		}
	}

	return envVars
}

// reconcileSnapshotCronJob ensures the snapshot CronJob exists and is configured correctly
func (r *MemgraphClusterReconciler) reconcileSnapshotCronJob(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster) error {
	logger := log.FromContext(ctx)

	// If snapshots are not enabled, ensure CronJob doesn't exist
	if !cluster.Spec.Snapshot.Enabled {
		return r.deleteSnapshotCronJob(ctx, cluster)
	}

	desired := buildSnapshotCronJob(cluster)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Creating snapshot CronJob", "name", desired.Name, "schedule", cluster.Spec.Snapshot.Schedule)
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update if schedule changed
	if existing.Spec.Schedule != desired.Spec.Schedule {
		logger.Info("Updating snapshot CronJob schedule",
			"name", existing.Name,
			"oldSchedule", existing.Spec.Schedule,
			"newSchedule", desired.Spec.Schedule)
		existing.Spec.Schedule = desired.Spec.Schedule
		return r.Update(ctx, existing)
	}

	return nil
}

// deleteSnapshotCronJob deletes the snapshot CronJob if it exists
func (r *MemgraphClusterReconciler) deleteSnapshotCronJob(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster) error {
	logger := log.FromContext(ctx)

	cronJob := &batchv1.CronJob{}
	name := cluster.Name + "-snapshot"
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, cronJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil // Already doesn't exist
		}
		return err
	}

	logger.Info("Deleting snapshot CronJob", "name", name)
	return r.Delete(ctx, cronJob)
}

// updateSnapshotStatus updates the snapshot-related status fields
func (r *MemgraphClusterReconciler) updateSnapshotStatus(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster) error {
	// Check for recently completed snapshot jobs
	jobList := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(labelsForCluster(cluster)),
	}

	if err := r.List(ctx, jobList, listOpts...); err != nil {
		return err
	}

	// Find the most recent successful job
	var latestSuccess *time.Time
	for _, job := range jobList.Items {
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				completionTime := job.Status.CompletionTime
				if completionTime != nil {
					t := completionTime.Time
					if latestSuccess == nil || t.After(*latestSuccess) {
						latestSuccess = &t
					}
				}
			}
		}
	}

	if latestSuccess != nil {
		cluster.Status.LastSnapshotTime = &metav1.Time{Time: *latestSuccess}
	}

	return nil
}

// Helper to create pointer to bool
func ptr[T any](v T) *T {
	return &v
}
