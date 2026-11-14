// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func clusterWithMemory(memoryLimitMB int32, podLimit string) *memgraphv1alpha1.MemgraphCluster {
	c := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			Config: memgraphv1alpha1.MemgraphConfig{MemoryLimit: memoryLimitMB},
		},
	}
	if podLimit != "" {
		c.Spec.Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(podLimit)}
	}
	return c
}

func TestEffectiveMemoryLimitMB(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected int32
	}{
		{"configured value is used as is", clusterWithMemory(3000, "2Gi"), 3000},
		{"configured value without pod limit", clusterWithMemory(1200, ""), 1200},
		{"unset derives 80% of a 2Gi pod", clusterWithMemory(0, "2Gi"), 1638},
		{"unset derives 80% of a 256Mi pod", clusterWithMemory(0, "256Mi"), 204},
		{"unset derives 80% of a 4Gi pod", clusterWithMemory(0, "4Gi"), 3276},
		{"unset without pod limit falls back to the default", clusterWithMemory(0, ""), defaultMemoryLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveMemoryLimitMB(tt.cluster); got != tt.expected {
				t.Errorf("effectiveMemoryLimitMB() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestMemoryLimitCheck(t *testing.T) {
	tests := []struct {
		name        string
		cluster     *memgraphv1alpha1.MemgraphCluster
		fits        bool
		wantCeiling int32
	}{
		{"3000MB in a 2Gi pod does not fit", clusterWithMemory(3000, "2Gi"), false, 1638},
		{"exactly the ceiling fits", clusterWithMemory(1638, "2Gi"), true, 1638},
		{"one MB above the ceiling does not fit", clusterWithMemory(1639, "2Gi"), false, 1638},
		{"3000MB in a 256Mi pod does not fit", clusterWithMemory(3000, "256Mi"), false, 204},
		{"3500MB in a 4Gi pod does not fit", clusterWithMemory(3500, "4Gi"), false, 3276},
		{"derived value always fits", clusterWithMemory(0, "2Gi"), true, 1638},
		{"no pod limit has nothing to compare", clusterWithMemory(3000, ""), true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkMemoryLimit(tt.cluster)
			if got.fits != tt.fits || got.ceilingMB != tt.wantCeiling {
				t.Errorf("checkMemoryLimit() = %+v, want fits=%v ceiling=%d", got, tt.fits, tt.wantCeiling)
			}
		})
	}
}

func TestBuildMemgraphArgsMemoryLimit(t *testing.T) {
	t.Run("a configured limit above the pod limit is never lowered", func(t *testing.T) {
		args := buildMemgraphArgs(clusterWithMemory(3000, "2Gi"))
		if !containsArg(args, "--memory-limit=3000") {
			t.Errorf("args = %v, want --memory-limit=3000 kept as configured", args)
		}
	})
	t.Run("an unset limit is derived from the pod limit", func(t *testing.T) {
		args := buildMemgraphArgs(clusterWithMemory(0, "2Gi"))
		if !containsArg(args, "--memory-limit=1638") {
			t.Errorf("args = %v, want --memory-limit=1638", args)
		}
	})
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestSetMemoryLimitCondition(t *testing.T) {
	cluster := clusterWithMemory(3000, "2Gi")

	if !setMemoryLimitCondition(cluster) {
		t.Fatal("first detection of a limit above the pod limit must report a transition so one event is sent")
	}
	cond := meta.FindStatusCondition(cluster.Status.Conditions, memgraphv1alpha1.ConditionTypeMemoryLimitWithinPodLimit)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != EventReasonMemoryLimitAbovePodLimit {
		t.Fatalf("condition = %+v, want False with reason %s", cond, EventReasonMemoryLimitAbovePodLimit)
	}
	if setMemoryLimitCondition(cluster) {
		t.Error("an unchanged mismatch must not report a transition again, or every reconcile sends an event")
	}

	cluster.Spec.Config.MemoryLimit = 1600
	if setMemoryLimitCondition(cluster) {
		t.Error("going back to a fitting limit is not a warning transition")
	}
	cond = meta.FindStatusCondition(cluster.Status.Conditions, memgraphv1alpha1.ConditionTypeMemoryLimitWithinPodLimit)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("condition = %+v, want True after fixing the limit", cond)
	}
}
