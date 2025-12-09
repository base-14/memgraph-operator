// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestReplicationManager_getReplicaName(t *testing.T) {
	rm := &ReplicationManager{}

	tests := []struct {
		podName  string
		expected string
	}{
		{"my-cluster-0", "my_cluster_0"},
		{"my-cluster-1", "my_cluster_1"},
		{"test-pod", "test_pod"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.podName, func(t *testing.T) {
			result := rm.getReplicaName(tt.podName)
			if result != tt.expected {
				t.Errorf("getReplicaName(%s) = %s, want %s", tt.podName, result, tt.expected)
			}
		})
	}
}

func TestGetPodFQDN(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected string
	}{
		{
			name:    "default suffix",
			podName: "my-cluster-0",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			},
			expected: "my-cluster-0.my-cluster-hl.default.svc.cluster.local",
		},
		{
			name:    "custom suffix",
			podName: "prod-db-1",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "prod-db", Namespace: "production"},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
						HeadlessSuffix: "-headless",
					},
				},
			},
			expected: "prod-db-1.prod-db-headless.production.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPodFQDN(tt.podName, tt.cluster)
			if result != tt.expected {
				t.Errorf("getPodFQDN(%s, cluster) = %s, want %s",
					tt.podName, result, tt.expected)
			}
		})
	}
}

func TestReplicationManager_getReadyPodsSorted(t *testing.T) {
	rm := &ReplicationManager{}

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		},
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
				Phase: corev1.PodPending,
			},
		},
	}

	readyPods := rm.getReadyPodsSorted(pods)

	if len(readyPods) != 2 {
		t.Errorf("expected 2 ready pods, got %d", len(readyPods))
	}

	if readyPods[0].Name != "cluster-0" {
		t.Errorf("expected first pod to be cluster-0, got %s", readyPods[0].Name)
	}

	if readyPods[1].Name != "cluster-2" {
		t.Errorf("expected second pod to be cluster-2, got %s", readyPods[1].Name)
	}
}

func TestNewReplicationManager(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	rm := NewReplicationManager(nil, fakeRecorder)

	if rm == nil {
		t.Error("NewReplicationManager returned nil")
	}

	if rm.Client() != nil {
		t.Error("Expected nil client")
	}

	if rm.recorder != fakeRecorder {
		t.Error("Recorder not set correctly")
	}
}

func TestReplicationManager_HandleMainFailover(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	rm := &ReplicationManager{
		recorder: fakeRecorder,
	}

	tests := []struct {
		name         string
		cluster      *memgraphv1alpha1.MemgraphCluster
		pods         []corev1.Pod
		failedMain   string
		expectedMain string
		expectError  bool
	}{
		{
			name: "HA disabled should error",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       memgraphv1alpha1.MemgraphClusterSpec{},
			},
			pods:        []corev1.Pod{},
			failedMain:  "test-0",
			expectError: true,
		},
		{
			name: "No healthy candidates should error",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						Enabled: true,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-0"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			failedMain:  "test-0",
			expectError: true,
		},
		{
			name: "Should promote first healthy replica",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						Enabled: true,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-0"},
					Status:     corev1.PodStatus{Phase: corev1.PodPending},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			failedMain:   "test-0",
			expectedMain: "test-1",
			expectError:  false,
		},
		{
			name: "Should promote preferred main if available",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					HighAvailability: &memgraphv1alpha1.HighAvailabilitySpec{
						Enabled:       true,
						PreferredMain: "test-2",
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-0"},
					Status:     corev1.PodStatus{Phase: corev1.PodPending},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-1"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-2"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			failedMain:   "test-0",
			expectedMain: "test-2",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			log := zap.NewNop()
			newMain, err := rm.HandleMainFailover(ctx, tt.cluster, tt.pods, tt.failedMain, log)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if newMain != tt.expectedMain {
					t.Errorf("expected new main %s, got %s", tt.expectedMain, newMain)
				}
			}
		})
	}
}
