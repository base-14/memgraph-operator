// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "not running",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "running but not ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "running with no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{},
				},
			},
			expected: false,
		},
		{
			name: "running with other conditions but not ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "failed pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodReady(tt.pod); got != tt.expected {
				t.Errorf("isPodReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConditionStatus(t *testing.T) {
	tests := []struct {
		name     string
		ok       bool
		expected metav1.ConditionStatus
	}{
		{
			name:     "true",
			ok:       true,
			expected: metav1.ConditionTrue,
		},
		{
			name:     "false",
			ok:       false,
			expected: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conditionStatus(tt.ok); got != tt.expected {
				t.Errorf("conditionStatus() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConditionReason(t *testing.T) {
	tests := []struct {
		name        string
		ok          bool
		trueReason  string
		falseReason string
		expected    string
	}{
		{
			name:        "true condition",
			ok:          true,
			trueReason:  "Ready",
			falseReason: "NotReady",
			expected:    "Ready",
		},
		{
			name:        "false condition",
			ok:          false,
			trueReason:  "Ready",
			falseReason: "NotReady",
			expected:    "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conditionReason(tt.ok, tt.trueReason, tt.falseReason); got != tt.expected {
				t.Errorf("conditionReason() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConditionMessage(t *testing.T) {
	tests := []struct {
		name     string
		ok       bool
		trueMsg  string
		falseMsg string
		expected string
	}{
		{
			name:     "true message",
			ok:       true,
			trueMsg:  "All systems go",
			falseMsg: "Systems not ready",
			expected: "All systems go",
		},
		{
			name:     "false message",
			ok:       false,
			trueMsg:  "All systems go",
			falseMsg: "Systems not ready",
			expected: "Systems not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conditionMessage(tt.ok, tt.trueMsg, tt.falseMsg); got != tt.expected {
				t.Errorf("conditionMessage() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetermineWriteInstance(t *testing.T) {
	reconciler := &MemgraphClusterReconciler{}

	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		pods     []corev1.Pod
		expected string
	}{
		{
			name: "keep existing write instance when ready",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-0",
		},
		{
			name: "wait for current main when it is not ready and HA is disabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 2},
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "wait for current main while its pod is being recreated and HA is disabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 2},
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "re-select main when the recorded main was scaled away",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 2},
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-2",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-0",
		},
		{
			name: "fail over to a ready pod when current is not ready and HA is enabled",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					Replicas:         2,
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{Enabled: true},
				},
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-1",
		},
		{
			name: "use preferred main from HA config",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						PreferredMain: "cluster-2",
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-2",
		},
		{
			name: "ignore preferred main if not ready",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						PreferredMain: "cluster-2",
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
			},
			expected: "cluster-0",
		},
		{
			name:    "no pods ready",
			cluster: &memgraphv1alpha1.MemgraphCluster{},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
			},
			expected: "",
		},
		{
			name:     "empty pod list",
			cluster:  &memgraphv1alpha1.MemgraphCluster{},
			pods:     []corev1.Pod{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.determineWriteInstance(tt.cluster, tt.pods)
			if got != tt.expected {
				t.Errorf("determineWriteInstance() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLabelsForCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster *memgraphv1alpha1.MemgraphCluster
	}{
		{
			name: "basic cluster",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
			},
		},
		{
			name: "cluster with special characters in name",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster-123",
					Namespace: "production",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := labelsForCluster(tt.cluster)

			// Verify required labels exist
			if labels["app.kubernetes.io/name"] != labelAppValue {
				t.Errorf("app.kubernetes.io/name = %s, want %s", labels["app.kubernetes.io/name"], labelAppValue)
			}

			if labels["app.kubernetes.io/instance"] != tt.cluster.Name {
				t.Errorf("app.kubernetes.io/instance = %s, want %s", labels["app.kubernetes.io/instance"], tt.cluster.Name)
			}

			if labels["app.kubernetes.io/managed-by"] != "memgraph-operator" {
				t.Errorf("app.kubernetes.io/managed-by = %s, want memgraph-operator", labels["app.kubernetes.io/managed-by"])
			}
		})
	}
}

func TestIsPodReadyEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "multiple conditions with ready true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			name: "succeeded pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
		{
			name: "unknown phase",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodUnknown,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodReady(tt.pod); got != tt.expected {
				t.Errorf("isPodReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetermineWriteInstanceEdgeCases(t *testing.T) {
	reconciler := &MemgraphClusterReconciler{}

	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		pods     []corev1.Pod
		expected string
	}{
		{
			name: "all pods not ready",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Status: memgraphv1alpha1.MemgraphClusterStatus{
					WriteInstance: "cluster-0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
			},
			expected: "",
		},
		{
			name: "preferred main not in pod list",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						PreferredMain: "cluster-99", // Non-existent
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-0", // Falls back to first ready pod
		},
		{
			name: "nil HA config",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: nil,
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-0"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-0",
		},
		{
			name: "empty preferred main",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						PreferredMain: "", // Empty
						Enabled:       true,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: "cluster-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.determineWriteInstance(tt.cluster, tt.pods)
			if got != tt.expected {
				t.Errorf("determineWriteInstance() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWriteInstanceForStatus(t *testing.T) {
	cluster := func(ha *memgraphv1alpha1.HighAvailabilitySpec, recorded string) *memgraphv1alpha1.MemgraphCluster {
		return &memgraphv1alpha1.MemgraphCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 2, HighAvailability: ha},
			Status:     memgraphv1alpha1.MemgraphClusterStatus{WriteInstance: recorded},
		}
	}

	tests := []struct {
		name          string
		cluster       *memgraphv1alpha1.MemgraphCluster
		writeInstance string
		expected      string
	}{
		{"available main is recorded", cluster(nil, "cluster-0"), "cluster-0", "cluster-0"},
		{"first main is recorded", cluster(nil, ""), "cluster-1", "cluster-1"},
		{"unavailable main is kept with HA disabled", cluster(nil, "cluster-0"), "", "cluster-0"},
		{"unavailable main is kept with HA explicitly disabled", cluster(&memgraphv1alpha1.HighAvailabilitySpec{Enabled: false}, "cluster-0"), "", "cluster-0"},
		{"unavailable main is cleared with HA enabled", cluster(&memgraphv1alpha1.HighAvailabilitySpec{Enabled: true}, "cluster-0"), "", ""},
		{"scaled-away main is cleared", cluster(nil, "cluster-5"), "", ""},
		{"foreign pod name is cleared", cluster(nil, "other-0"), "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := writeInstanceForStatus(tt.cluster, tt.writeInstance); got != tt.expected {
				t.Errorf("writeInstanceForStatus() = %q, want %q", got, tt.expected)
			}
		})
	}
}
