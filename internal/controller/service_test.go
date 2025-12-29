// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestHeadlessServiceName(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected string
	}{
		{
			name: "default suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			expected: "test-cluster-hl",
		},
		{
			name: "custom suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
						HeadlessSuffix: "-headless",
					},
				},
			},
			expected: "test-cluster-headless",
		},
		{
			name: "empty custom suffix uses default",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
						HeadlessSuffix: "",
					},
				},
			},
			expected: "test-cluster-hl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := headlessServiceName(tt.cluster)
			if got != tt.expected {
				t.Errorf("headlessServiceName() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestWriteServiceName(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected string
	}{
		{
			name: "default suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			expected: "test-cluster-write",
		},
		{
			name: "custom suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
						WriteSuffix: "-primary",
					},
				},
			},
			expected: "test-cluster-primary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeServiceName(tt.cluster)
			if got != tt.expected {
				t.Errorf("writeServiceName() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestReadServiceName(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *memgraphv1alpha1.MemgraphCluster
		expected string
	}{
		{
			name: "default suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			expected: "test-cluster-read",
		},
		{
			name: "custom suffix",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{
					ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
						ReadSuffix: "-replica",
					},
				},
			},
			expected: "test-cluster-replica",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readServiceName(tt.cluster)
			if got != tt.expected {
				t.Errorf("readServiceName() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestBuildHeadlessService(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{},
	}

	svc := buildHeadlessService(cluster)

	// Verify name
	if svc.Name != "test-cluster-hl" {
		t.Errorf("Name = %s, want test-cluster-hl", svc.Name)
	}

	// Verify namespace
	if svc.Namespace != metav1.NamespaceDefault {
		t.Errorf("Namespace = %s, want %s", svc.Namespace, metav1.NamespaceDefault)
	}

	// Verify it's headless
	if svc.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Errorf("ClusterIP = %s, want None", svc.Spec.ClusterIP)
	}

	// Verify PublishNotReadyAddresses
	if !svc.Spec.PublishNotReadyAddresses {
		t.Error("PublishNotReadyAddresses should be true")
	}

	// Verify ports
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("Ports count = %d, want 2", len(svc.Spec.Ports))
	}

	foundBolt := false
	foundReplication := false
	for _, port := range svc.Spec.Ports {
		if port.Name == boltPortName && port.Port == boltPort {
			foundBolt = true
			if port.Protocol != corev1.ProtocolTCP {
				t.Errorf("Bolt port protocol = %s, want TCP", port.Protocol)
			}
		}
		if port.Name == replicationPortName && port.Port == replicationPort {
			foundReplication = true
			if port.Protocol != corev1.ProtocolTCP {
				t.Errorf("Replication port protocol = %s, want TCP", port.Protocol)
			}
		}
	}

	if !foundBolt {
		t.Error("Bolt port not found")
	}
	if !foundReplication {
		t.Error("Replication port not found")
	}

	// Verify labels
	if svc.Labels["app.kubernetes.io/name"] != labelAppValue {
		t.Errorf("Label app.kubernetes.io/name = %s, want %s", svc.Labels["app.kubernetes.io/name"], labelAppValue)
	}

	// Verify selector
	if svc.Spec.Selector["app.kubernetes.io/name"] != labelAppValue {
		t.Errorf("Selector app.kubernetes.io/name = %s, want %s", svc.Spec.Selector["app.kubernetes.io/name"], labelAppValue)
	}
}

func TestBuildWriteService(t *testing.T) {
	tests := []struct {
		name              string
		cluster           *memgraphv1alpha1.MemgraphCluster
		writeInstanceName string
		expectPodSelector bool
	}{
		{
			name: "with write instance",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			writeInstanceName: "test-cluster-0",
			expectPodSelector: true,
		},
		{
			name: "without write instance",
			cluster: &memgraphv1alpha1.MemgraphCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: memgraphv1alpha1.MemgraphClusterSpec{},
			},
			writeInstanceName: "",
			expectPodSelector: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := buildWriteService(tt.cluster, tt.writeInstanceName)

			// Verify name
			if svc.Name != "test-cluster-write" {
				t.Errorf("Name = %s, want test-cluster-write", svc.Name)
			}

			// Verify namespace
			if svc.Namespace != "default" {
				t.Errorf("Namespace = %s, want default", svc.Namespace)
			}

			// Verify type is ClusterIP
			if svc.Spec.Type != corev1.ServiceTypeClusterIP {
				t.Errorf("Type = %s, want ClusterIP", svc.Spec.Type)
			}

			// Verify ports
			if len(svc.Spec.Ports) != 1 {
				t.Fatalf("Ports count = %d, want 1", len(svc.Spec.Ports))
			}

			if svc.Spec.Ports[0].Name != boltPortName || svc.Spec.Ports[0].Port != boltPort {
				t.Errorf("Port = %s:%d, want %s:%d", svc.Spec.Ports[0].Name, svc.Spec.Ports[0].Port, boltPortName, boltPort)
			}

			// Verify pod selector
			podName, hasPodSelector := svc.Spec.Selector["statefulset.kubernetes.io/pod-name"]
			if tt.expectPodSelector {
				if !hasPodSelector {
					t.Error("Expected pod name selector but not found")
				}
				if podName != tt.writeInstanceName {
					t.Errorf("Pod name selector = %s, want %s", podName, tt.writeInstanceName)
				}
			} else {
				if hasPodSelector {
					t.Error("Did not expect pod name selector")
				}
			}
		})
	}
}

func TestBuildReadService(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{},
	}

	svc := buildReadService(cluster)

	// Verify name
	if svc.Name != "test-cluster-read" {
		t.Errorf("Name = %s, want test-cluster-read", svc.Name)
	}

	// Verify namespace
	if svc.Namespace != metav1.NamespaceDefault {
		t.Errorf("Namespace = %s, want %s", svc.Namespace, metav1.NamespaceDefault)
	}

	// Verify type is ClusterIP
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %s, want ClusterIP", svc.Spec.Type)
	}

	// Verify ports
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("Ports count = %d, want 1", len(svc.Spec.Ports))
	}

	if svc.Spec.Ports[0].Name != boltPortName || svc.Spec.Ports[0].Port != boltPort {
		t.Errorf("Port = %s:%d, want %s:%d", svc.Spec.Ports[0].Name, svc.Spec.Ports[0].Port, boltPortName, boltPort)
	}

	// Verify labels
	if svc.Labels["app.kubernetes.io/name"] != "memgraph" {
		t.Errorf("Label app.kubernetes.io/name = %s, want memgraph", svc.Labels["app.kubernetes.io/name"])
	}

	// Verify selector (should select all pods)
	if svc.Spec.Selector["app.kubernetes.io/name"] != "memgraph" {
		t.Errorf("Selector app.kubernetes.io/name = %s, want memgraph", svc.Spec.Selector["app.kubernetes.io/name"])
	}

	// Should NOT have pod-name selector (load balances all)
	if _, hasPodSelector := svc.Spec.Selector["statefulset.kubernetes.io/pod-name"]; hasPodSelector {
		t.Error("Read service should not have pod-name selector")
	}
}

func TestBuildServicesWithCustomNames(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "custom-ns",
		},
		Spec: memgraphv1alpha1.MemgraphClusterSpec{
			ServiceNames: &memgraphv1alpha1.ServiceNamesSpec{
				HeadlessSuffix: "-headless",
				WriteSuffix:    "-primary",
				ReadSuffix:     "-secondary",
			},
		},
	}

	headlessSvc := buildHeadlessService(cluster)
	if headlessSvc.Name != "my-cluster-headless" {
		t.Errorf("Headless service name = %s, want my-cluster-headless", headlessSvc.Name)
	}

	writeSvc := buildWriteService(cluster, "my-cluster-0")
	if writeSvc.Name != "my-cluster-primary" {
		t.Errorf("Write service name = %s, want my-cluster-primary", writeSvc.Name)
	}

	readSvc := buildReadService(cluster)
	if readSvc.Name != "my-cluster-secondary" {
		t.Errorf("Read service name = %s, want my-cluster-secondary", readSvc.Name)
	}
}
