// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

// Default service name suffixes
const (
	defaultHeadlessSuffix = "-hl"
	defaultWriteSuffix    = "-write"
	defaultReadSuffix     = "-read"
)

// Service name helpers
func headlessServiceName(cluster *memgraphv1alpha1.MemgraphCluster) string {
	suffix := defaultHeadlessSuffix
	if cluster.Spec.ServiceNames != nil && cluster.Spec.ServiceNames.HeadlessSuffix != "" {
		suffix = cluster.Spec.ServiceNames.HeadlessSuffix
	}
	return cluster.Name + suffix
}

func writeServiceName(cluster *memgraphv1alpha1.MemgraphCluster) string {
	suffix := defaultWriteSuffix
	if cluster.Spec.ServiceNames != nil && cluster.Spec.ServiceNames.WriteSuffix != "" {
		suffix = cluster.Spec.ServiceNames.WriteSuffix
	}
	return cluster.Name + suffix
}

func readServiceName(cluster *memgraphv1alpha1.MemgraphCluster) string {
	suffix := defaultReadSuffix
	if cluster.Spec.ServiceNames != nil && cluster.Spec.ServiceNames.ReadSuffix != "" {
		suffix = cluster.Spec.ServiceNames.ReadSuffix
	}
	return cluster.Name + suffix
}

// buildHeadlessService creates the headless service for StatefulSet DNS
func buildHeadlessService(cluster *memgraphv1alpha1.MemgraphCluster) *corev1.Service {
	labels := labelsForCluster(cluster)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       boltPortName,
					Port:       boltPort,
					TargetPort: intstr.FromInt(boltPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       replicationPortName,
					Port:       replicationPort,
					TargetPort: intstr.FromInt(replicationPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}
}

// buildWriteService creates the ClusterIP service that points to the current MAIN instance
// The selector will be updated dynamically to point to the current write pod
func buildWriteService(cluster *memgraphv1alpha1.MemgraphCluster, writeInstanceName string) *corev1.Service {
	labels := labelsForCluster(cluster)

	// Selector to target the specific write pod
	selector := labelsForCluster(cluster)
	if writeInstanceName != "" {
		selector["statefulset.kubernetes.io/pod-name"] = writeInstanceName
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      writeServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       boltPortName,
					Port:       boltPort,
					TargetPort: intstr.FromInt(boltPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: selector,
		},
	}
}

// buildReadService creates the ClusterIP service that load-balances across all REPLICA instances
func buildReadService(cluster *memgraphv1alpha1.MemgraphCluster) *corev1.Service {
	labels := labelsForCluster(cluster)

	// Initially selects all pods; we'll use an annotation or label to mark read pods
	// For now, this selects all pods - reads should work on any pod
	selector := labelsForCluster(cluster)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      readServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       boltPortName,
					Port:       boltPort,
					TargetPort: intstr.FromInt(boltPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: selector,
		},
	}
}
