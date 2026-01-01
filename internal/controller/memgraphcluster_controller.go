// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)

const (
	// Requeue intervals
	requeueAfterShort = 10 * time.Second
	requeueAfterLong  = 30 * time.Second
	// Error backoff - prevent tight retry loops when operations fail
	requeueAfterError    = 30 * time.Second
	requeueAfterErrorMax = 5 * time.Minute

	// Finalizer name
	finalizerName = "memgraph.base14.io/finalizer"
)

// MemgraphClusterReconciler reconciles a MemgraphCluster object
type MemgraphClusterReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	Config             *rest.Config
	Log                *zap.Logger
	replicationManager *ReplicationManager
	metrics            *MetricsRecorder
}

// +kubebuilder:rbac:groups=memgraph.base14.io,resources=memgraphclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=memgraph.base14.io,resources=memgraphclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=memgraph.base14.io,resources=memgraphclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch

// Reconcile is the main reconciliation loop
func (r *MemgraphClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.With(
		zap.String("cluster", req.Name),
		zap.String("namespace", req.Namespace),
	)
	startTime := time.Now()

	// Initialize metrics recorder if not already done
	if r.metrics == nil {
		r.metrics = NewMetricsRecorder()
	}

	// Fetch the MemgraphCluster instance
	cluster := &memgraphv1alpha1.MemgraphCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// Clean up metrics for deleted cluster
			r.metrics.DeleteClusterMetrics(req.Name, req.Namespace)
			return ctrl.Result{}, nil
		}
		log.Error("failed to get MemgraphCluster", zap.Error(err))
		r.metrics.RecordReconcileOperation(req.Name, req.Namespace, "error")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cluster)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		controllerutil.AddFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if needed
	if cluster.Status.Phase == "" {
		cluster.Status.Phase = memgraphv1alpha1.ClusterPhasePending
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonClusterCreated,
			"Cluster created, starting initialization")
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Track previous phase for event emission
	previousPhase := cluster.Status.Phase

	// Reconcile resources
	result, err := r.reconcileResources(ctx, cluster, log)

	// Record metrics
	duration := time.Since(startTime).Seconds()
	r.metrics.RecordReconcileDuration(cluster.Name, cluster.Namespace, duration)

	if err != nil {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReconcileError, err.Error())
		r.metrics.RecordReconcileOperation(cluster.Name, cluster.Namespace, "error")
		return result, err
	}

	// Record cluster metrics
	r.metrics.RecordReconcileOperation(cluster.Name, cluster.Namespace, "success")
	r.metrics.RecordClusterPhase(cluster.Name, cluster.Namespace, string(cluster.Status.Phase))

	// Emit phase transition events
	r.emitPhaseTransitionEvents(cluster, previousPhase)

	replicas := cluster.Spec.Replicas
	if replicas == 0 {
		replicas = 3
	}
	r.metrics.RecordClusterInstances(cluster.Name, cluster.Namespace,
		cluster.Status.ReadyInstances, replicas, cluster.Status.RegisteredReplicas)

	// Record validation metrics if available
	if cluster.Status.Validation != nil {
		passed := cluster.Status.Validation.AllReplicasHealthy
		r.metrics.RecordReplicationHealth(cluster.Name, cluster.Namespace,
			cluster.Status.Validation.ReplicationLagMs, passed)

		if cluster.Status.Validation.LastConnectivityTest != nil {
			r.metrics.RecordValidation(cluster.Name, cluster.Namespace,
				float64(cluster.Status.Validation.LastConnectivityTest.Unix()), passed)
		}

		// Record per-instance health
		for _, inst := range cluster.Status.Validation.Instances {
			r.metrics.RecordInstanceHealth(cluster.Name, cluster.Namespace, inst.Name, inst.Role, inst.Healthy)
		}
	}

	// Record snapshot metrics if available
	if cluster.Status.LastSnapshotTime != nil {
		r.metrics.RecordSnapshotSuccess(cluster.Name, cluster.Namespace,
			float64(cluster.Status.LastSnapshotTime.Unix()))
	}

	return result, nil
}

// handleDeletion handles the cleanup when a MemgraphCluster is deleted
func (r *MemgraphClusterReconciler) handleDeletion(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(cluster, finalizerName) {
		// TODO: Trigger final snapshot before deletion if configured

		// Remove finalizer
		controllerutil.RemoveFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileResources reconciles all resources for the cluster
func (r *MemgraphClusterReconciler) reconcileResources(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) (ctrl.Result, error) {
	// 1. Reconcile headless service (must exist before StatefulSet)
	if err := r.reconcileHeadlessService(ctx, cluster, log); err != nil {
		return ctrl.Result{}, err
	}

	// 2. Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, cluster, log); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Get pod status and determine roles
	pods, err := r.getClusterPods(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 4. Determine write instance (first ready pod if none assigned)
	writeInstance := r.determineWriteInstance(cluster, pods)

	// 5. Reconcile write service
	if err := r.reconcileWriteService(ctx, cluster, writeInstance, log); err != nil {
		return ctrl.Result{}, err
	}

	// 6. Reconcile read service
	if err := r.reconcileReadService(ctx, cluster, log); err != nil {
		return ctrl.Result{}, err
	}

	// 7. Configure replication if we have a write instance and pods are ready
	var registeredReplicas int32
	var replicationError error
	if writeInstance != "" && len(pods) > 1 {
		if err := r.ensureReplicationManager(); err != nil {
			log.Error("failed to create replication manager", zap.Error(err))
			replicationError = err
		} else {
			if err := r.replicationManager.ConfigureReplication(ctx, cluster, pods, writeInstance, log); err != nil {
				log.Error("failed to configure replication", zap.Error(err))
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicationError,
					fmt.Sprintf("Failed to configure replication: %v", err))
				replicationError = err
			} else {
				health, err := r.replicationManager.CheckReplicationHealth(ctx, cluster, writeInstance, log)
				if err == nil && health != nil {
					registeredReplicas = health.TotalReplicas
				}
			}
		}
	}

	// 8. Reconcile snapshot CronJob if enabled
	if cluster.Spec.Snapshot.Enabled {
		if err := r.reconcileSnapshotCronJob(ctx, cluster, log); err != nil {
			log.Error("failed to reconcile snapshot CronJob", zap.Error(err))
		}
	}

	// 9. Update snapshot status
	if err := r.updateSnapshotStatus(ctx, cluster); err != nil {
		log.Error("failed to update snapshot status", zap.Error(err))
	}

	// 10. Run validation tests (only when cluster is running)
	if cluster.Status.Phase == memgraphv1alpha1.ClusterPhaseRunning {
		if err := r.reconcileValidation(ctx, cluster, pods, writeInstance, log); err != nil {
			log.Error("validation failed", zap.Error(err))
		}
	}

	// 11. Collect storage metrics from all running pods
	r.collectStorageMetrics(ctx, cluster, pods, writeInstance, log)

	// 12. Update status
	if err := r.updateStatus(ctx, cluster, pods, writeInstance, registeredReplicas); err != nil {
		return ctrl.Result{}, err
	}

	// Check if we need to requeue for pending operations
	if cluster.Status.Phase != memgraphv1alpha1.ClusterPhaseRunning {
		log.Info("cluster not yet running, requeueing",
			zap.String("phase", string(cluster.Status.Phase)),
			zap.Duration("requeueAfter", requeueAfterShort))
		return ctrl.Result{RequeueAfter: requeueAfterShort}, nil
	}

	// If there was a replication error, use longer backoff to prevent overwhelming Memgraph
	if replicationError != nil {
		log.Info("replication error occurred, using longer backoff",
			zap.Duration("requeueAfter", requeueAfterError),
			zap.Error(replicationError))
		return ctrl.Result{RequeueAfter: requeueAfterError}, nil
	}

	// Requeue for periodic health checks
	return ctrl.Result{RequeueAfter: requeueAfterLong}, nil
}

// ensureReplicationManager creates the replication manager if not already created
func (r *MemgraphClusterReconciler) ensureReplicationManager() error {
	if r.replicationManager != nil {
		return nil
	}

	if r.Config == nil {
		return fmt.Errorf("rest config is not set")
	}

	mgClient, err := memgraph.NewClient(r.Config)
	if err != nil {
		return fmt.Errorf("failed to create memgraph client: %w", err)
	}

	r.replicationManager = NewReplicationManager(mgClient, r.Recorder)
	return nil
}

// reconcileHeadlessService ensures the headless service exists
func (r *MemgraphClusterReconciler) reconcileHeadlessService(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) error {
	desired := buildHeadlessService(cluster)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating headless service",
				zap.String("service", desired.Name))
			return r.Create(ctx, desired)
		}
		return err
	}

	return nil
}

// reconcileStatefulSet ensures the StatefulSet exists and is configured correctly
func (r *MemgraphClusterReconciler) reconcileStatefulSet(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) error {
	desired := buildStatefulSet(cluster)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating StatefulSet",
				zap.String("statefulset", desired.Name),
				zap.Int32("replicas", *desired.Spec.Replicas))
			r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonCreatingStatefulSet,
				fmt.Sprintf("Creating StatefulSet %s with %d replicas", desired.Name, *desired.Spec.Replicas))
			return r.Create(ctx, desired)
		}
		return err
	}

	// Check if update is needed
	needsUpdate := false

	// Check replica count change
	if *existing.Spec.Replicas != *desired.Spec.Replicas {
		log.Info("scaling StatefulSet",
			zap.String("statefulset", existing.Name),
			zap.Int32("currentReplicas", *existing.Spec.Replicas),
			zap.Int32("desiredReplicas", *desired.Spec.Replicas))
		existing.Spec.Replicas = desired.Spec.Replicas
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonScalingStatefulSet,
			fmt.Sprintf("Scaling StatefulSet %s to %d replicas", existing.Name, *desired.Spec.Replicas))
		needsUpdate = true
	}

	// Check image change
	existingImage := existing.Spec.Template.Spec.Containers[0].Image
	desiredImage := desired.Spec.Template.Spec.Containers[0].Image
	if existingImage != desiredImage {
		log.Info("updating StatefulSet image",
			zap.String("statefulset", existing.Name),
			zap.String("currentImage", existingImage),
			zap.String("desiredImage", desiredImage))
		existing.Spec.Template.Spec.Containers[0].Image = desiredImage
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonUpdatingImage,
			fmt.Sprintf("Updating StatefulSet %s image to %s", existing.Name, desiredImage))
		needsUpdate = true
	}

	if needsUpdate {
		return r.Update(ctx, existing)
	}

	return nil
}

// reconcileWriteService ensures the write service exists and points to the correct pod
func (r *MemgraphClusterReconciler) reconcileWriteService(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, writeInstance string, log *zap.Logger) error {
	desired := buildWriteService(cluster, writeInstance)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating write service",
				zap.String("service", desired.Name),
				zap.String("writeInstance", writeInstance))
			return r.Create(ctx, desired)
		}
		return err
	}

	// Check if selector needs update (write instance changed)
	currentWriteInstance := existing.Spec.Selector["statefulset.kubernetes.io/pod-name"]
	if currentWriteInstance != writeInstance && writeInstance != "" {
		log.Info("updating write service selector",
			zap.String("service", existing.Name),
			zap.String("previousInstance", currentWriteInstance),
			zap.String("newInstance", writeInstance))
		existing.Spec.Selector = desired.Spec.Selector
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonUpdatedWriteService,
			fmt.Sprintf("Write service now pointing to %s", writeInstance))
		return r.Update(ctx, existing)
	}

	return nil
}

// reconcileReadService ensures the read service exists
func (r *MemgraphClusterReconciler) reconcileReadService(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, log *zap.Logger) error {
	desired := buildReadService(cluster)
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating read service",
				zap.String("service", desired.Name))
			return r.Create(ctx, desired)
		}
		return err
	}

	return nil
}

// getClusterPods returns all pods belonging to the cluster
func (r *MemgraphClusterReconciler) getClusterPods(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(labelsForCluster(cluster)),
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// determineWriteInstance determines which pod should be the MAIN (write) instance
func (r *MemgraphClusterReconciler) determineWriteInstance(cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod) string {
	// If we already have a write instance and it's still ready, keep it
	if cluster.Status.WriteInstance != "" {
		for _, pod := range pods {
			if pod.Name == cluster.Status.WriteInstance && isPodReady(&pod) {
				return cluster.Status.WriteInstance
			}
		}
	}

	// Check if HA config specifies a preferred main
	if cluster.Spec.HighAvailability != nil && cluster.Spec.HighAvailability.PreferredMain != "" {
		for _, pod := range pods {
			if pod.Name == cluster.Spec.HighAvailability.PreferredMain && isPodReady(&pod) {
				return cluster.Spec.HighAvailability.PreferredMain
			}
		}
	}

	// Otherwise, pick the first ready pod
	for _, pod := range pods {
		if isPodReady(&pod) {
			return pod.Name
		}
	}

	return ""
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// updateStatus updates the cluster status
func (r *MemgraphClusterReconciler) updateStatus(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, writeInstance string, registeredReplicas int32) error {
	// Count ready instances
	var readyCount int32
	var readInstances []string

	for _, pod := range pods {
		if isPodReady(&pod) {
			readyCount++
			if pod.Name != writeInstance {
				readInstances = append(readInstances, pod.Name)
			}
		}
	}

	// Determine phase
	replicas := cluster.Spec.Replicas
	if replicas == 0 {
		replicas = 3
	}

	var phase memgraphv1alpha1.ClusterPhase
	switch {
	case readyCount == 0:
		phase = memgraphv1alpha1.ClusterPhasePending
	case readyCount < replicas:
		phase = memgraphv1alpha1.ClusterPhaseInitializing
	default:
		phase = memgraphv1alpha1.ClusterPhaseRunning
	}

	// Update status fields
	cluster.Status.Phase = phase
	cluster.Status.WriteInstance = writeInstance
	cluster.Status.ReadInstances = readInstances
	cluster.Status.ReadyInstances = readyCount
	cluster.Status.RegisteredReplicas = registeredReplicas

	// Update conditions
	r.updateConditions(cluster, writeInstance, readyCount, replicas, registeredReplicas)

	return r.Status().Update(ctx, cluster)
}

// updateConditions updates the status conditions
func (r *MemgraphClusterReconciler) updateConditions(cluster *memgraphv1alpha1.MemgraphCluster, writeInstance string, readyCount, replicas, registeredReplicas int32) {
	now := metav1.Now()

	// MainAvailable condition
	mainAvailable := writeInstance != ""
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               memgraphv1alpha1.ConditionTypeMainAvailable,
		Status:             conditionStatus(mainAvailable),
		LastTransitionTime: now,
		Reason:             conditionReason(mainAvailable, "MainAvailable", "MainUnavailable"),
		Message:            conditionMessage(mainAvailable, fmt.Sprintf("Main instance: %s", writeInstance), "No main instance available"),
	})

	// ReplicasReady condition
	replicasReady := readyCount >= replicas
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               memgraphv1alpha1.ConditionTypeReplicasReady,
		Status:             conditionStatus(replicasReady),
		LastTransitionTime: now,
		Reason:             conditionReason(replicasReady, "AllReplicasReady", "ReplicasNotReady"),
		Message:            fmt.Sprintf("%d/%d instances ready", readyCount, replicas),
	})

	// ReplicationHealthy condition
	expectedReplicas := replicas - 1 // excluding main
	replicationHealthy := registeredReplicas >= expectedReplicas && expectedReplicas >= 0
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               memgraphv1alpha1.ConditionTypeReplicationHealthy,
		Status:             conditionStatus(replicationHealthy),
		LastTransitionTime: now,
		Reason:             conditionReason(replicationHealthy, "ReplicationHealthy", "ReplicationUnhealthy"),
		Message:            fmt.Sprintf("%d/%d replicas registered with main", registeredReplicas, expectedReplicas),
	})

	// Ready condition (overall)
	ready := mainAvailable && replicasReady && replicationHealthy
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               memgraphv1alpha1.ConditionTypeReady,
		Status:             conditionStatus(ready),
		LastTransitionTime: now,
		Reason:             conditionReason(ready, "ClusterReady", "ClusterNotReady"),
		Message:            conditionMessage(ready, "Cluster is fully operational", "Cluster is not ready"),
	})
}

func conditionStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func conditionReason(ok bool, trueReason, falseReason string) string {
	if ok {
		return trueReason
	}
	return falseReason
}

func conditionMessage(ok bool, trueMsg, falseMsg string) string {
	if ok {
		return trueMsg
	}
	return falseMsg
}

// collectStorageMetrics collects storage metrics from all running pods
func (r *MemgraphClusterReconciler) collectStorageMetrics(
	ctx context.Context,
	cluster *memgraphv1alpha1.MemgraphCluster,
	pods []corev1.Pod,
	writeInstance string,
	log *zap.Logger,
) {
	if r.replicationManager == nil || r.replicationManager.Client() == nil {
		return
	}

	mgClient := r.replicationManager.Client()
	for _, pod := range pods {
		if !isPodReady(&pod) {
			continue
		}

		role := "replica"
		if pod.Name == writeInstance {
			role = "main"
		}

		info, err := mgClient.GetStorageInfo(ctx, cluster.Namespace, pod.Name)
		if err != nil {
			log.Debug("failed to collect storage metrics",
				zap.String("pod", pod.Name),
				zap.Error(err))
			continue
		}

		r.metrics.RecordStorageInfo(cluster.Name, cluster.Namespace, pod.Name, role, info)
	}
}

// emitPhaseTransitionEvents emits events when cluster phase changes
func (r *MemgraphClusterReconciler) emitPhaseTransitionEvents(
	cluster *memgraphv1alpha1.MemgraphCluster,
	previousPhase memgraphv1alpha1.ClusterPhase,
) {
	currentPhase := cluster.Status.Phase
	if currentPhase == previousPhase {
		return
	}

	switch {
	case currentPhase == memgraphv1alpha1.ClusterPhaseRunning:
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonClusterReady,
			fmt.Sprintf("Cluster is ready with %d instances", cluster.Status.ReadyInstances))
	case previousPhase == memgraphv1alpha1.ClusterPhaseRunning &&
		currentPhase == memgraphv1alpha1.ClusterPhaseInitializing:
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonClusterDegraded,
			fmt.Sprintf("Cluster degraded: %d instances ready", cluster.Status.ReadyInstances))
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MemgraphClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&memgraphv1alpha1.MemgraphCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&batchv1.CronJob{}).
		Named("memgraphcluster").
		Complete(r)
}
