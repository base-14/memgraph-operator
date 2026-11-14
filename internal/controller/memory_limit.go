// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const memoryLimitPodPercent = 80

type memoryLimitCheck struct {
	effectiveMB int32
	podLimitMB  int32
	ceilingMB   int32
	fits        bool
}

func podMemoryLimitMB(cluster *memgraphv1alpha1.MemgraphCluster) int32 {
	q, ok := cluster.Spec.Resources.Limits[corev1.ResourceMemory]
	if !ok || q.IsZero() {
		return 0
	}
	return int32(q.Value() / (1024 * 1024))
}

func memoryCeilingMB(podLimitMB int32) int32 {
	return int32(int64(podLimitMB) * memoryLimitPodPercent / 100)
}

func effectiveMemoryLimitMB(cluster *memgraphv1alpha1.MemgraphCluster) int32 {
	if v := cluster.Spec.Config.MemoryLimit; v > 0 {
		return v
	}
	if pod := podMemoryLimitMB(cluster); pod > 0 {
		return memoryCeilingMB(pod)
	}
	return defaultMemoryLimit
}

func checkMemoryLimit(cluster *memgraphv1alpha1.MemgraphCluster) memoryLimitCheck {
	c := memoryLimitCheck{effectiveMB: effectiveMemoryLimitMB(cluster), podLimitMB: podMemoryLimitMB(cluster), fits: true}
	if c.podLimitMB == 0 {
		return c
	}
	c.ceilingMB = memoryCeilingMB(c.podLimitMB)
	c.fits = c.effectiveMB <= c.ceilingMB
	return c
}

func setMemoryLimitCondition(cluster *memgraphv1alpha1.MemgraphCluster) bool {
	c := checkMemoryLimit(cluster)
	prev := meta.FindStatusCondition(cluster.Status.Conditions, memgraphv1alpha1.ConditionTypeMemoryLimitWithinPodLimit)
	wasFalse := prev != nil && prev.Status == metav1.ConditionFalse

	cond := metav1.Condition{
		Type:               memgraphv1alpha1.ConditionTypeMemoryLimitWithinPodLimit,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "MemoryLimitWithinPodLimit",
		Message:            fmt.Sprintf("Memgraph memory limit %dMiB", c.effectiveMB),
	}
	if c.podLimitMB > 0 {
		cond.Message = fmt.Sprintf("Memgraph memory limit %dMiB is within %d%% of the pod limit (%dMiB)", c.effectiveMB, memoryLimitPodPercent, c.podLimitMB)
	}
	if !c.fits {
		cond.Status = metav1.ConditionFalse
		cond.Reason = EventReasonMemoryLimitAbovePodLimit
		cond.Message = memoryLimitWarning(c)
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, cond)
	return !c.fits && !wasFalse
}

func memoryLimitWarning(c memoryLimitCheck) string {
	return fmt.Sprintf("Memgraph may use %dMiB but the pod is killed at %dMiB, so it is OOMKilled instead of refusing writes. "+
		"Set config.memoryLimit to at most %d, or raise resources.limits.memory. "+
		"Lower it only once memory_tracked is below the new value: Memgraph will not start with more data than its limit.",
		c.effectiveMB, c.podLimitMB, c.ceilingMB)
}
