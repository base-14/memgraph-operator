// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const staleBackupCleanupInterval = 5 * time.Minute

type staleBackupSchedule struct {
	mu   sync.Mutex
	now  func() time.Time
	last map[string]time.Time
}

func newStaleBackupSchedule(now func() time.Time) *staleBackupSchedule {
	return &staleBackupSchedule{now: now, last: map[string]time.Time{}}
}

func (s *staleBackupSchedule) due(cluster string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if last, ok := s.last[cluster]; ok && now.Sub(last) < staleBackupCleanupInterval {
		return false
	}
	s.last[cluster] = now
	return true
}

func staleBackupPods(pods []corev1.Pod, streaming bool) []string {
	if !streaming {
		return nil
	}
	var names []string
	for i := range pods {
		if isPodReady(&pods[i]) {
			names = append(names, pods[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

// reconcileStaleBackups removes the .old snapshot and WAL directories that a
// replica recovery leaves behind. It only runs while every replica is streaming,
// so no recovery is in progress when the directories are deleted.
func (r *MemgraphClusterReconciler) reconcileStaleBackups(ctx context.Context, cluster *memgraphv1alpha1.MemgraphCluster, pods []corev1.Pod, streaming bool, log *zap.Logger) {
	if r.replicationManager == nil {
		return
	}
	targets := staleBackupPods(pods, streaming)
	if len(targets) == 0 {
		return
	}
	if r.staleBackups == nil {
		r.staleBackups = newStaleBackupSchedule(time.Now)
	}
	if !r.staleBackups.due(cluster.Namespace + "/" + cluster.Name) {
		return
	}
	client := r.replicationManager.Client()
	for _, pod := range targets {
		freed, err := client.RemoveStaleBackups(ctx, cluster.Namespace, pod, dataVolumePath)
		if err != nil {
			log.Warn("failed to remove stale snapshot and WAL backups", zap.String("pod", pod), zap.Error(err))
			continue
		}
		if freed == 0 {
			continue
		}
		log.Info("removed stale snapshot and WAL backups", zap.String("pod", pod), zap.Int64("freedBytes", freed))
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonRemovedStaleBackups,
			fmt.Sprintf("Removed snapshot and WAL backups left in .old by a replica recovery on %s, freeing %.1f MiB", pod, float64(freed)/(1<<20)))
	}
}
