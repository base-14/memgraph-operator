// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

const (
	// Label keys
	labelApp       = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelComponent = "app.kubernetes.io/component"
	labelManagedBy = "app.kubernetes.io/managed-by"

	// Label values
	labelAppValue       = "memgraph"
	labelManagedByValue = "memgraph-operator"
	labelComponentValue = "database"
)

// labelsForCluster returns the standard labels for all resources in a cluster
func labelsForCluster(cluster *memgraphv1alpha1.MemgraphCluster) map[string]string {
	return map[string]string{
		labelApp:       labelAppValue,
		labelInstance:  cluster.Name,
		labelComponent: labelComponentValue,
		labelManagedBy: labelManagedByValue,
	}
}
