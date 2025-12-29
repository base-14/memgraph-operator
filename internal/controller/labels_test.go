// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

func TestLabelConstants(t *testing.T) {
	// Verify label key constants
	if labelApp != "app.kubernetes.io/name" {
		t.Errorf("labelApp = %s, want app.kubernetes.io/name", labelApp)
	}
	if labelInstance != "app.kubernetes.io/instance" {
		t.Errorf("labelInstance = %s, want app.kubernetes.io/instance", labelInstance)
	}
	if labelComponent != "app.kubernetes.io/component" {
		t.Errorf("labelComponent = %s, want app.kubernetes.io/component", labelComponent)
	}
	if labelManagedBy != "app.kubernetes.io/managed-by" {
		t.Errorf("labelManagedBy = %s, want app.kubernetes.io/managed-by", labelManagedBy)
	}

	// Verify label value constants
	// Using string literals intentionally to test the constant values
	if labelAppValue != "memgraph" { //nolint:goconst // testing constant value
		t.Errorf("labelAppValue = %s, want memgraph", labelAppValue)
	}
	if labelManagedByValue != "memgraph-operator" { //nolint:goconst // testing constant value
		t.Errorf("labelManagedByValue = %s, want memgraph-operator", labelManagedByValue)
	}
	if labelComponentValue != "database" { //nolint:goconst // testing constant value
		t.Errorf("labelComponentValue = %s, want database", labelComponentValue)
	}
}

func TestLabelsForClusterDetails(t *testing.T) {
	cluster := &memgraphv1alpha1.MemgraphCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "production",
		},
	}

	labels := labelsForCluster(cluster)

	// Should have exactly 4 labels
	if len(labels) != 4 {
		t.Errorf("Expected 4 labels, got %d", len(labels))
	}

	// Verify component label
	if labels[labelComponent] != labelComponentValue {
		t.Errorf("Component label = %s, want %s", labels[labelComponent], labelComponentValue)
	}
}
