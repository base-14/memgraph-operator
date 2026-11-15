// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func legacyClearReplicationStateInit() corev1.Container {
	return corev1.Container{
		Name:    "clear-replication-state",
		Image:   "busybox:1.36",
		Command: []string{"sh", "-c", "rm -rf /var/lib/memgraph/.internal/replication; rm -f /var/lib/memgraph/.internal/epoch_id"},
	}
}

func withAPIServerDefaults(cs []corev1.Container) []corev1.Container {
	out := make([]corev1.Container, len(cs))
	for i, c := range cs {
		c.TerminationMessagePath = "/dev/termination-log"
		c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
		out[i] = c
	}
	return out
}

func TestPodSpecDriftRemovesClearReplicationState(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}}

	t.Run("a StatefulSet built by an older operator loses the init container", func(t *testing.T) {
		existing := buildStatefulSet(cluster)
		desired := buildStatefulSet(cluster)
		legacy := []corev1.Container{existing.Spec.Template.Spec.InitContainers[0], legacyClearReplicationStateInit(), existing.Spec.Template.Spec.InitContainers[1]}
		existing.Spec.Template.Spec.InitContainers = withAPIServerDefaults(legacy)

		if !applyStatefulSetPodSpecDrift(existing, desired) {
			t.Fatal("expected drift: clear-replication-state must be removed from existing StatefulSets")
		}
		for _, c := range existing.Spec.Template.Spec.InitContainers {
			if c.Name == "clear-replication-state" {
				t.Fatal("clear-replication-state still present after drift was applied")
			}
		}
		if n := len(existing.Spec.Template.Spec.InitContainers); n != 2 {
			t.Errorf("init containers = %d, want 2", n)
		}
	})

	t.Run("fields the API server fills in are not drift", func(t *testing.T) {
		existing := buildStatefulSet(cluster)
		desired := buildStatefulSet(cluster)
		existing.Spec.Template.Spec.InitContainers = withAPIServerDefaults(existing.Spec.Template.Spec.InitContainers)
		if applyStatefulSetPodSpecDrift(existing, desired) {
			t.Error("defaulted fields must not count as drift, or every reconcile rewrites the StatefulSet")
		}
	})

	t.Run("a changed init container image is drift", func(t *testing.T) {
		existing := buildStatefulSet(cluster)
		desired := buildStatefulSet(cluster)
		existing.Spec.Template.Spec.InitContainers[1].Image = "busybox:1.30"
		if !applyStatefulSetPodSpecDrift(existing, desired) {
			t.Error("expected drift on init container image change")
		}
	})
}
