// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const (
	defaultReseedAfter   = 5 * time.Minute
	reseedCooldown       = time.Hour
	reseedAnnotationBase = "memgraph.base14.io/reseeded-"
	serveReadsLabel      = "memgraph.base14.io/serve-reads"
	serveReadsYes        = "true"
	serveReadsNo         = "false"
)

type reseedDecision struct {
	pod  string
	skip string
}

func reseedAnnotation(pod string) string {
	return reseedAnnotationBase + pod
}

func reseedAfter(cluster *memgraphv1alpha1.MemgraphCluster) time.Duration {
	if d := cluster.Spec.Replication.ReseedAfter; d != nil && d.Duration > 0 {
		return d.Duration
	}
	return defaultReseedAfter
}

func autoReseedEnabled(cluster *memgraphv1alpha1.MemgraphCluster) bool {
	return cluster.Spec.Replication.AutoReseed == nil || *cluster.Spec.Replication.AutoReseed
}

func replicationStreamingHealthy(replicas, registered, healthy int32) bool {
	expected := replicas - 1
	return expected >= 0 && registered >= expected && healthy >= registered
}

func planReseed(cluster *memgraphv1alpha1.MemgraphCluster, sts *appsv1.StatefulSet, pods []corev1.Pod, main string, candidates []string, now time.Time) reseedDecision {
	if len(candidates) == 0 {
		return reseedDecision{}
	}
	if !autoReseedEnabled(cluster) {
		return reseedDecision{skip: "autoReseed is disabled"}
	}
	if sts == nil || sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		return reseedDecision{skip: "StatefulSet rollout in progress"}
	}
	if main == "" {
		return reseedDecision{skip: "no main instance"}
	}

	byName := make(map[string]*corev1.Pod, len(pods))
	for i := range pods {
		byName[pods[i].Name] = &pods[i]
	}
	if p, ok := byName[main]; !ok || !isPodReady(p) {
		return reseedDecision{skip: fmt.Sprintf("main %s is not ready", main)}
	}

	podNames := make([]string, 0, len(candidates))
	for _, c := range candidates {
		podNames = append(podNames, strings.ReplaceAll(c, "_", "-"))
	}
	sort.Strings(podNames)

	skip := ""
	for _, pod := range podNames {
		if pod == main {
			skip = fmt.Sprintf("%s is the main", pod)
			continue
		}
		if _, ok := byName[pod]; !ok {
			skip = fmt.Sprintf("pod %s not found", pod)
			continue
		}
		if raw, ok := sts.Annotations[reseedAnnotation(pod)]; ok {
			if last, err := time.Parse(time.RFC3339, raw); err == nil && now.Sub(last) < reseedCooldown {
				skip = fmt.Sprintf("%s was re-seeded at %s; waiting %s between re-seeds", pod, raw, reseedCooldown)
				continue
			}
		}
		return reseedDecision{pod: pod}
	}
	return reseedDecision{skip: skip}
}

type dropReplicaFunc func(ctx context.Context, main, replica string) error

func (r *MemgraphClusterReconciler) executeReseed(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, sts *appsv1.StatefulSet, main, pod string, now time.Time, drop dropReplicaFunc, log *zap.Logger) error {
	if pod == "" || pod == main {
		return fmt.Errorf("refusing to re-seed %q: it is the main or empty", pod)
	}
	replicaName := strings.ReplaceAll(pod, "-", "_")

	if err := drop(ctx, main, replicaName); err != nil {
		return fmt.Errorf("drop replica %s on %s: %w", replicaName, main, err)
	}

	patch := client.MergeFrom(sts.DeepCopy())
	if sts.Annotations == nil {
		sts.Annotations = map[string]string{}
	}
	sts.Annotations[reseedAnnotation(pod)] = now.Format(time.RFC3339)
	if err := r.Patch(ctx, sts, patch); err != nil {
		return fmt.Errorf("record re-seed on StatefulSet: %w", err)
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: dataVolumeName + "-" + pod, Namespace: cluster.Namespace}}
	if err := r.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete PVC %s: %w", pvc.Name, err)
	}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pod, Namespace: cluster.Namespace}}
	if err := r.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete pod %s: %w", pod, err)
	}

	log.Info("re-seeding replica: dropped from main, deleted its PVC and pod",
		zap.String("replica", pod), zap.String("main", main))
	return nil
}

func (r *MemgraphClusterReconciler) reseedRecorder() record.EventRecorder {
	if r.ReseedRecorder != nil {
		return r.ReseedRecorder
	}
	return r.Recorder
}

func (r *MemgraphClusterReconciler) reconcileReseed(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, main string, log *zap.Logger) {
	candidates := r.replicationManager.ReseedCandidates(cluster)
	if len(candidates) == 0 {
		return
	}
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, sts); err != nil {
		log.Error("re-seed: cannot read StatefulSet", zap.Error(err))
		return
	}
	now := time.Now()
	decision := planReseed(cluster, sts, pods, main, candidates, now)
	if decision.pod == "" {
		if decision.skip != "" {
			log.Info("re-seed skipped", zap.Strings("candidates", candidates), zap.String("reason", decision.skip))
			r.reseedRecorder().Event(cluster, corev1.EventTypeWarning, EventReasonReplicaReseedSkipped,
				fmt.Sprintf("Frozen replica(s) %s not re-seeded: %s", strings.Join(candidates, ", "), decision.skip))
		}
		return
	}

	r.reseedRecorder().Event(cluster, corev1.EventTypeWarning, EventReasonReplicaReseeding,
		fmt.Sprintf("Re-seeding frozen replica %s from main %s: dropping it, deleting its PVC and pod", decision.pod, main))
	drop := func(ctx context.Context, m, replica string) error {
		return r.replicationManager.DropReplica(ctx, cluster.Namespace, m, replica)
	}
	if err := r.executeReseed(ctx, cluster, sts, main, decision.pod, now, drop, log); err != nil {
		log.Error("re-seed failed", zap.String("replica", decision.pod), zap.Error(err))
		r.reseedRecorder().Event(cluster, corev1.EventTypeWarning, EventReasonReplicaReseedFailed,
			fmt.Sprintf("Re-seeding %s failed: %v", decision.pod, err))
		return
	}
	r.replicationManager.ResetReplica(cluster, strings.ReplaceAll(decision.pod, "-", "_"))
}

func desiredServeReads(pods []corev1.Pod, main string, healthyByPod map[string]bool, healthKnown bool) map[string]string {
	out := map[string]string{}
	if main == "" {
		return out
	}
	for _, pod := range pods {
		switch {
		case pod.Name == main:
			out[pod.Name] = serveReadsYes
		case healthKnown:
			if healthyByPod[pod.Name] {
				out[pod.Name] = serveReadsYes
			} else {
				out[pod.Name] = serveReadsNo
			}
		}
	}
	return out
}

func (r *MemgraphClusterReconciler) applyServeReads(ctx context.Context, pods []corev1.Pod, desired map[string]string) error {
	for i := range pods {
		pod := &pods[i]
		want, ok := desired[pod.Name]
		if !ok || pod.Labels[serveReadsLabel] == want {
			continue
		}
		patch := client.MergeFrom(pod.DeepCopy())
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		pod.Labels[serveReadsLabel] = want
		if err := r.Patch(ctx, pod, patch); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("label pod %s: %w", pod.Name, err)
		}
	}
	return nil
}
