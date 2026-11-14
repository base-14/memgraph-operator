// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func readyPod(name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

func TestPlanReseed(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	f := false
	tr := true

	cluster := func(auto *bool) *memgraphv1alpha1.MemgraphCluster {
		return &memgraphv1alpha1.MemgraphCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
			Spec: memgraphv1alpha1.MemgraphClusterSpec{
				Replicas:    2,
				Replication: memgraphv1alpha1.ReplicationSpec{AutoReseed: auto},
			},
		}
	}
	sts := func(current, update string, annotations map[string]string) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns", Annotations: annotations},
			Status:     appsv1.StatefulSetStatus{CurrentRevision: current, UpdateRevision: update},
		}
	}
	pods := []corev1.Pod{readyPod("c-0"), readyPod("c-1")}

	tests := []struct {
		name       string
		cluster    *memgraphv1alpha1.MemgraphCluster
		sts        *appsv1.StatefulSet
		pods       []corev1.Pod
		main       string
		candidates []string
		wantPod    string
	}{
		{"enabled by default", cluster(nil), sts("r1", "r1", nil), pods, "c-0", []string{"c_1"}, "c-1"},
		{"explicitly enabled", cluster(&tr), sts("r1", "r1", nil), pods, "c-0", []string{"c_1"}, "c-1"},
		{"disabled", cluster(&f), sts("r1", "r1", nil), pods, "c-0", []string{"c_1"}, ""},
		{"no candidates", cluster(nil), sts("r1", "r1", nil), pods, "c-0", nil, ""},
		{"rollout in progress", cluster(nil), sts("r1", "r2", nil), pods, "c-0", []string{"c_1"}, ""},
		{"no main", cluster(nil), sts("r1", "r1", nil), pods, "", []string{"c_1"}, ""},
		{"main not ready", cluster(nil), sts("r1", "r1", nil), []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "c-0"}}, readyPod("c-1")}, "c-0", []string{"c_1"}, ""},
		{"candidate is the main", cluster(nil), sts("r1", "r1", nil), pods, "c-1", []string{"c_1"}, ""},
		{"candidate pod missing", cluster(nil), sts("r1", "r1", nil), []corev1.Pod{readyPod("c-0")}, "c-0", []string{"c_1"}, ""},
		{"recent reseed in cooldown", cluster(nil), sts("r1", "r1", map[string]string{reseedAnnotation("c-1"): now.Add(-10 * time.Minute).Format(time.RFC3339)}), pods, "c-0", []string{"c_1"}, ""},
		{"old reseed outside cooldown", cluster(nil), sts("r1", "r1", map[string]string{reseedAnnotation("c-1"): now.Add(-2 * time.Hour).Format(time.RFC3339)}), pods, "c-0", []string{"c_1"}, "c-1"},
		{"unparseable cooldown annotation does not block", cluster(nil), sts("r1", "r1", map[string]string{reseedAnnotation("c-1"): "garbage"}), pods, "c-0", []string{"c_1"}, "c-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planReseed(tt.cluster, tt.sts, tt.pods, tt.main, tt.candidates, now)
			if got.pod != tt.wantPod {
				t.Errorf("planReseed() pod = %q (skip=%q), want %q", got.pod, got.skip, tt.wantPod)
			}
			if got.pod == "" && len(tt.candidates) > 0 && got.skip == "" {
				t.Errorf("a skipped candidate must carry a reason")
			}
		})
	}
}

func TestPlanReseedPicksOnePodPerPass(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 3},
	}
	sts := &appsv1.StatefulSet{Status: appsv1.StatefulSetStatus{CurrentRevision: "r", UpdateRevision: "r"}}
	pods := []corev1.Pod{readyPod("c-0"), readyPod("c-1"), readyPod("c-2")}
	got := planReseed(cluster, sts, pods, "c-0", []string{"c_2", "c_1"}, now)
	if got.pod != "c-1" {
		t.Errorf("pod = %q, want c-1 (lowest ordinal first)", got.pod)
	}
}

func TestReseedAfter(t *testing.T) {
	c := &memgraphv1alpha1.MemgraphCluster{}
	if got := reseedAfter(c); got != 5*time.Minute {
		t.Errorf("default reseedAfter = %v, want 5m", got)
	}
	c.Spec.Replication.ReseedAfter = &metav1.Duration{Duration: 15 * time.Minute}
	if got := reseedAfter(c); got != 15*time.Minute {
		t.Errorf("reseedAfter = %v, want 15m", got)
	}
}

func TestDesiredServeReads(t *testing.T) {
	pods := []corev1.Pod{readyPod("c-0"), readyPod("c-1"), readyPod("c-2")}

	t.Run("main and streaming replicas serve reads, frozen replicas do not", func(t *testing.T) {
		got := desiredServeReads(pods, "c-0", map[string]bool{"c-1": true, "c-2": false}, true)
		want := map[string]string{"c-0": "true", "c-1": "true", "c-2": "false"}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
			}
		}
	})

	t.Run("replica missing from health is excluded", func(t *testing.T) {
		got := desiredServeReads(pods, "c-0", map[string]bool{"c-1": true}, true)
		if got["c-2"] != "false" {
			t.Errorf("c-2 = %q, want false", got["c-2"])
		}
	})

	t.Run("unknown health only labels the main", func(t *testing.T) {
		got := desiredServeReads(pods, "c-0", nil, false)
		if len(got) != 1 || got["c-0"] != serveReadsYes {
			t.Errorf("got %v, want only c-0=true", got)
		}
	})

	t.Run("no main labels nothing", func(t *testing.T) {
		if got := desiredServeReads(pods, "", map[string]bool{"c-1": true}, true); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestReadServiceSelectsOnlyServingPods(t *testing.T) {
	svc := buildReadService(&memgraphv1alpha1.MemgraphCluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}})
	if svc.Spec.Selector[serveReadsLabel] != serveReadsYes {
		t.Errorf("read service selector = %v, want %s=true", svc.Spec.Selector, serveReadsLabel)
	}
}

func TestReplicationStreamingHealthy(t *testing.T) {
	tests := []struct {
		name                          string
		replicas, registered, healthy int32
		want                          bool
	}{
		{"single instance", 1, 0, 0, true},
		{"all streaming", 2, 1, 1, true},
		{"registered but frozen", 2, 1, 0, false},
		{"not registered", 2, 0, 0, false},
		{"three nodes one frozen", 3, 2, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replicationStreamingHealthy(tt.replicas, tt.registered, tt.healthy); got != tt.want {
				t.Errorf("replicationStreamingHealthy(%d,%d,%d) = %v, want %v", tt.replicas, tt.registered, tt.healthy, got, tt.want)
			}
		})
	}
}

func TestReseedEventsUseTheirOwnRecorder(t *testing.T) {
	main := record.NewFakeRecorder(1)
	reseed := record.NewFakeRecorder(1)
	r := &MemgraphClusterReconciler{Recorder: main, ReseedRecorder: reseed}
	if r.reseedRecorder() != reseed {
		t.Error("re-seed events must go to ReseedRecorder so other warnings cannot rate-limit them away")
	}
	r.ReseedRecorder = nil
	if r.reseedRecorder() != main {
		t.Error("without a ReseedRecorder, fall back to Recorder")
	}
}
