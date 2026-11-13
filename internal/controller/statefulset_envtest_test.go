// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

var _ = Describe("MemgraphCluster extraArgs", func() {
	ctx := context.Background()

	newCluster := func(name string, extra []string) *memgraphv1alpha1.MemgraphCluster {
		return &memgraphv1alpha1.MemgraphCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: memgraphv1alpha1.MemgraphClusterSpec{
				Replicas: 2,
				Config:   memgraphv1alpha1.MemgraphConfig{ExtraArgs: extra},
			},
		}
	}

	DescribeTable("rejects flags the operator must control",
		func(arg string) {
			err := k8sClient.Create(ctx, newCluster("reserved-flag", []string{arg}))
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected an Invalid error, got %v", err)
			Expect(err.Error()).To(ContainSubstring("extraArgs"))
		},
		Entry("data directory", "--data-directory=/tmp"),
		Entry("bolt port", "--bolt-port=7688"),
		Entry("bolt address", "--bolt-address=127.0.0.1"),
		Entry("coordinator id", "--coordinator-id=1"),
		Entry("management port", "--management-port=10000"),
		Entry("missing leading dashes", "log-file="),
		Entry("separate value token", "/var/lib/memgraph/memgraph.log"),
	)

	It("accepts regular Memgraph flags", func() {
		cluster := newCluster("accepted-flags", []string{"--log-file=", "--telemetry-enabled=false", "--storage-snapshot-retention-count=2"})
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
	})

	It("applies extraArgs changes to an existing StatefulSet", func() {
		cluster := newCluster("args-drift", nil)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, cluster)
			_ = k8sClient.Delete(ctx, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}})
		})

		r := &MemgraphClusterReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Log:      zap.NewNop(),
			Recorder: record.NewFakeRecorder(100),
		}
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())

		sts := &appsv1.StatefulSet{}
		key := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-file=/var/lib/memgraph/memgraph.log"))

		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		cluster.Spec.Config.ExtraArgs = []string{"--log-file=", "--telemetry-enabled=false"}
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())

		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		args := sts.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElements("--log-file=", "--telemetry-enabled=false"))
		Expect(args).NotTo(ContainElement("--log-file=/var/lib/memgraph/memgraph.log"))
		Expect(sts.Spec.Template.Spec.Containers[0].Lifecycle).To(BeNil())
	})

	It("applies resources and scheduling changes and then stays stable", func() {
		cluster := newCluster("resources-drift", nil)
		cluster.Spec.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, cluster)
			_ = k8sClient.Delete(ctx, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}})
		})

		r := &MemgraphClusterReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Log:      zap.NewNop(),
			Recorder: record.NewFakeRecorder(100),
		}
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())

		key := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		cluster.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("2Gi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("4Gi")},
		}
		cluster.Spec.Tolerations = []corev1.Toleration{
			{Key: "kubernetes.io/arch", Operator: corev1.TolerationOpEqual, Value: "arm64", Effect: corev1.TaintEffectNoSchedule},
		}
		cluster.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "linux"}
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		podSpec := sts.Spec.Template.Spec
		limit := podSpec.Containers[0].Resources.Limits[corev1.ResourceMemory]
		Expect(limit.String()).To(Equal("4Gi"))
		Expect(podSpec.Tolerations).To(HaveLen(1))
		Expect(podSpec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/os", "linux"))

		version := sts.ResourceVersion
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		Expect(sts.ResourceVersion).To(Equal(version), "a reconcile with no spec change must not update the StatefulSet")
	})
})
