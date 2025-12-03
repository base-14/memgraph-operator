// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

const (
	// Default AWS CLI image for S3 uploads
	defaultAWSCLIImage = "amazon/aws-cli:latest"

	// Shared volume name for snapshot data between containers
	snapshotDataVolume = "snapshot-data"
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
func (sm *SnapshotManager) TriggerSnapshot(ctx context.Context, namespace, podName string, log *zap.Logger) error {
	log.Info("triggering snapshot", zap.String("pod", podName))

	if err := sm.mgClient.CreateSnapshot(ctx, namespace, podName); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	log.Info("snapshot created successfully", zap.String("pod", podName))
	return nil
}

// buildSnapshotCronJob creates a CronJob for scheduled snapshots
func buildSnapshotCronJob(cluster *memgraphv1alpha1.MemgraphCluster) *batchv1.CronJob {
	name := cluster.Name + "-snapshot"
	schedule := cluster.Spec.Snapshot.Schedule
	if schedule == "" {
		schedule = "*/15 * * * *" // Default: every 15 minutes
	}

	// Use the same image as memgraph for the snapshot job
	memgraphImage := cluster.Spec.Image
	if memgraphImage == "" {
		memgraphImage = defaultMemgraphImage
	}

	// Determine history limits
	successfulJobsHistoryLimit := int32(3)
	failedJobsHistoryLimit := int32(1)

	// Security context
	var runAsUser int64 = 999
	var runAsGroup int64 = 999

	// Build init containers (snapshot creation)
	initContainers := buildSnapshotInitContainers(cluster, memgraphImage)

	// Build main containers
	containers := buildSnapshotMainContainers(cluster)

	// Build volumes
	volumes := buildSnapshotVolumes(cluster)

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
							InitContainers: initContainers,
							Containers:     containers,
							Volumes:        volumes,
						},
					},
				},
			},
		},
	}
}

// buildSnapshotInitContainers builds init containers for snapshot creation and data copy
func buildSnapshotInitContainers(cluster *memgraphv1alpha1.MemgraphCluster, memgraphImage string) []corev1.Container {
	writeService := writeServiceName(cluster)

	// Init container 1: Create snapshot via mgconsole
	snapshotCmd := fmt.Sprintf(`
set -e
echo "Creating snapshot on %s..."
echo "CREATE SNAPSHOT;" | mgconsole --host %s --port 7687 --use-ssl=false --no-history
echo "Snapshot created successfully at $(date)"
`, writeService, writeService)

	initContainers := []corev1.Container{
		{
			Name:    "create-snapshot",
			Image:   memgraphImage,
			Command: []string{"/bin/bash", "-c"},
			Args:    []string{snapshotCmd},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		},
	}

	// Init container 2: Copy snapshot files to shared volume (if S3 enabled)
	if cluster.Spec.Snapshot.S3 != nil && cluster.Spec.Snapshot.S3.Enabled {
		// Use bitnami/kubectl for copying files from the main pod
		copyCmd := fmt.Sprintf(`
set -e
echo "Copying snapshot files from MAIN pod..."

# Find the current write instance
WRITE_POD=$(kubectl get svc %s -n %s -o jsonpath='{.spec.selector.statefulset\.kubernetes\.io/pod-name}' 2>/dev/null || echo "")
if [ -z "$WRITE_POD" ]; then
  # Fallback: get first ready pod
  WRITE_POD=$(kubectl get pods -n %s -l app.kubernetes.io/instance=%s --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')
fi

echo "MAIN pod: ${WRITE_POD}"

# Copy snapshot files
mkdir -p /snapshot-data/snapshots
kubectl exec ${WRITE_POD} -n %s -c memgraph -- tar -cf - -C /var/lib/memgraph/snapshots . 2>/dev/null | tar -xf - -C /snapshot-data/snapshots/ || echo "Warning: Could not copy snapshots"

# Write timestamp
echo "$(date +%%Y%%m%%d-%%H%%M%%S)" > /snapshot-data/timestamp
echo "Snapshot files copied successfully"
ls -la /snapshot-data/snapshots/
`, writeService, cluster.Namespace, cluster.Namespace, cluster.Name, cluster.Namespace)

		initContainers = append(initContainers, corev1.Container{
			Name:    "copy-snapshots",
			Image:   "bitnami/kubectl:latest",
			Command: []string{"/bin/sh", "-c"},
			Args:    []string{copyCmd},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      snapshotDataVolume,
					MountPath: "/snapshot-data",
				},
			},
		})
	}

	return initContainers
}

// buildSnapshotMainContainers builds the main containers for the snapshot job
func buildSnapshotMainContainers(cluster *memgraphv1alpha1.MemgraphCluster) []corev1.Container {
	// If S3 is enabled, main container uploads to S3
	if cluster.Spec.Snapshot.S3 != nil && cluster.Spec.Snapshot.S3.Enabled {
		return []corev1.Container{buildS3UploadContainer(cluster)}
	}

	// Otherwise, just a completion container
	return []corev1.Container{
		{
			Name:    "complete",
			Image:   "busybox:1.36",
			Command: []string{"/bin/sh", "-c"},
			Args:    []string{"echo 'Snapshot job completed successfully'"},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		},
	}
}

// buildS3UploadContainer builds the S3 upload container
func buildS3UploadContainer(cluster *memgraphv1alpha1.MemgraphCluster) corev1.Container {
	s3 := cluster.Spec.Snapshot.S3
	prefix := s3.Prefix
	if prefix == "" {
		prefix = "memgraph/snapshots"
	}

	// Build S3 upload command
	s3Cmd := fmt.Sprintf(`
set -e

TIMESTAMP=$(cat /snapshot-data/timestamp)
BACKUP_PATH="s3://%s/%s/%s/${TIMESTAMP}"

echo "Uploading snapshot to ${BACKUP_PATH}..."

# Configure endpoint if specified
%s

# Upload to S3
if [ -d "/snapshot-data/snapshots" ] && [ "$(ls -A /snapshot-data/snapshots 2>/dev/null)" ]; then
  aws s3 cp /snapshot-data/snapshots/ ${BACKUP_PATH}/snapshots/ --recursive
  echo "Snapshot uploaded successfully to ${BACKUP_PATH}"
else
  echo "No snapshot files found to upload"
  exit 1
fi

echo "S3 backup completed at $(date)"
`, s3.Bucket, prefix, cluster.Name, buildS3EndpointConfig(s3))

	return corev1.Container{
		Name:    "s3-upload",
		Image:   defaultAWSCLIImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{s3Cmd},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		Env: buildS3Env(cluster),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      snapshotDataVolume,
				MountPath: "/snapshot-data",
			},
		},
	}
}

// buildSnapshotVolumes builds the volumes for the snapshot job
func buildSnapshotVolumes(cluster *memgraphv1alpha1.MemgraphCluster) []corev1.Volume {
	var volumes []corev1.Volume

	// Add shared volume if S3 is enabled
	if cluster.Spec.Snapshot.S3 != nil && cluster.Spec.Snapshot.S3.Enabled {
		volumes = append(volumes, corev1.Volume{
			Name: snapshotDataVolume,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	return volumes
}

// buildS3EndpointConfig builds AWS CLI endpoint configuration
func buildS3EndpointConfig(s3 *memgraphv1alpha1.S3BackupSpec) string {
	if s3.Endpoint == "" {
		return ""
	}
	return fmt.Sprintf(`export AWS_ENDPOINT_URL="%s"`, s3.Endpoint)
}

// buildS3Env builds environment variables for the S3 upload container
func buildS3Env(cluster *memgraphv1alpha1.MemgraphCluster) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	if cluster.Spec.Snapshot.S3 == nil {
		return envVars
	}

	s3 := cluster.Spec.Snapshot.S3

	// Add S3 credentials if configured
	if s3.SecretRef != nil {
		envVars = append(envVars,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *s3.SecretRef,
						Key:                  "access-key-id",
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *s3.SecretRef,
						Key:                  "secret-access-key",
					},
				},
			},
		)
	}

	if s3.Region != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "AWS_REGION",
			Value: s3.Region,
		})
	}

	return envVars
}

// reconcileSnapshotCronJob ensures the snapshot CronJob exists and is configured correctly
func (r *MemgraphClusterReconciler) reconcileSnapshotCronJob(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) error {
	// If snapshots are not enabled, ensure CronJob doesn't exist
	if !cluster.Spec.Snapshot.Enabled {
		return r.deleteSnapshotCronJob(ctx, cluster, log)
	}

	desired := buildSnapshotCronJob(cluster)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating snapshot CronJob",
				zap.String("cronjob", desired.Name),
				zap.String("schedule", cluster.Spec.Snapshot.Schedule))
			if err := r.Create(ctx, desired); err != nil {
				return err
			}
			r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonSnapshotCronJobCreated,
				fmt.Sprintf("Snapshot CronJob created with schedule: %s", cluster.Spec.Snapshot.Schedule))
			return nil
		}
		return err
	}

	// Update if schedule or S3 config changed
	needsUpdate := existing.Spec.Schedule != desired.Spec.Schedule ||
		len(existing.Spec.JobTemplate.Spec.Template.Spec.Containers) != len(desired.Spec.JobTemplate.Spec.Template.Spec.Containers)

	if needsUpdate {
		log.Info("updating snapshot CronJob",
			zap.String("cronjob", existing.Name),
			zap.String("oldSchedule", existing.Spec.Schedule),
			zap.String("newSchedule", desired.Spec.Schedule))
		existing.Spec = desired.Spec
		if err := r.Update(ctx, existing); err != nil {
			return err
		}
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonSnapshotCronJobUpdated,
			fmt.Sprintf("Snapshot schedule updated to: %s", desired.Spec.Schedule))
		return nil
	}

	return nil
}

// deleteSnapshotCronJob deletes the snapshot CronJob if it exists
func (r *MemgraphClusterReconciler) deleteSnapshotCronJob(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) error {
	cronJob := &batchv1.CronJob{}
	name := cluster.Name + "-snapshot"
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, cronJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil // Already doesn't exist
		}
		return err
	}

	log.Info("deleting snapshot CronJob", zap.String("cronjob", name))
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
