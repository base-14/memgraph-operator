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
			name: "pick new write instance when current is not ready",
			cluster: &memgraphv1alpha1.MemgraphCluster{
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
			if labels["app.kubernetes.io/name"] != "memgraph" {
				t.Errorf("app.kubernetes.io/name = %s, want memgraph", labels["app.kubernetes.io/name"])
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
