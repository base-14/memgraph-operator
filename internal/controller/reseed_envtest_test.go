// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"context"
	"errors"
	"time"

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

var _ = Describe("Replica reseed", func() {
	ctx := context.Background()

	pvc := func(name string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
	}
	pod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "memgraph", Image: "memgraph/memgraph:3.7.2"}}},
		}
	}

	It("drops the replica, records the reseed, and deletes only the replica's PVC and pod", func() {
		cluster := &memgraphv1alpha1.MemgraphCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "default"},
			Spec:       memgraphv1alpha1.MemgraphClusterSpec{Replicas: 2},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		r := &MemgraphClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Log: zap.NewNop(), Recorder: record.NewFakeRecorder(100)}
		Expect(r.reconcileStatefulSet(ctx, cluster, zap.NewNop())).To(Succeed())
		for _, o := range []*corev1.PersistentVolumeClaim{pvc("data-rs-0"), pvc("data-rs-1")} {
			Expect(k8sClient.Create(ctx, o)).To(Succeed())
		}
		for _, o := range []*corev1.Pod{pod("rs-0"), pod("rs-1")} {
			Expect(k8sClient.Create(ctx, o)).To(Succeed())
		}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, cluster)
			_ = k8sClient.Delete(ctx, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "default"}})
			_ = k8sClient.Delete(ctx, pod("rs-0"))
			_ = k8sClient.Delete(ctx, pvc("data-rs-0"))
		})

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "rs", Namespace: "default"}, sts)).To(Succeed())

		var dropped []string
		now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
		err := r.executeReseed(ctx, cluster, sts, "rs-0", "rs-1", now, func(ctx context.Context, main, replica string) error {
			dropped = append(dropped, main+"/"+replica)
			return nil
		}, zap.NewNop())
		Expect(err).NotTo(HaveOccurred())
		Expect(dropped).To(Equal([]string{"rs-0/rs_1"}))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "rs", Namespace: "default"}, sts)).To(Succeed())
		Expect(sts.Annotations).To(HaveKeyWithValue(reseedAnnotation("rs-1"), now.Format(time.RFC3339)))

		Eventually(func() bool {
			p := &corev1.Pod{}
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: "rs-1", Namespace: "default"}, p))
		}).Should(BeTrue())

		claim := &corev1.PersistentVolumeClaim{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "data-rs-1", Namespace: "default"}, claim)
		Expect(apierrors.IsNotFound(err) || claim.DeletionTimestamp != nil).To(BeTrue(), "replica PVC must be deleted or terminating")

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "rs-0", Namespace: "default"}, &corev1.Pod{})).To(Succeed())
		main := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data-rs-0", Namespace: "default"}, main)).To(Succeed())
		Expect(main.DeletionTimestamp).To(BeNil())
	})

	It("refuses to touch the main", func() {
		r := &MemgraphClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Log: zap.NewNop(), Recorder: record.NewFakeRecorder(10)}
		cluster := &memgraphv1alpha1.MemgraphCluster{ObjectMeta: metav1.ObjectMeta{Name: "rs2", Namespace: "default"}}
		err := r.executeReseed(ctx, cluster, &appsv1.StatefulSet{}, "rs2-0", "rs2-0", time.Now(), func(context.Context, string, string) error {
			return errors.New("must not be called")
		}, zap.NewNop())
		Expect(err).To(HaveOccurred())
	})

	It("does not delete anything when dropping the replica fails", func() {
		Expect(k8sClient.Create(ctx, pvc("data-rs3-1"))).To(Succeed())
		Expect(k8sClient.Create(ctx, pod("rs3-1"))).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, pod("rs3-1"))
			_ = k8sClient.Delete(ctx, pvc("data-rs3-1"))
		})
		r := &MemgraphClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Log: zap.NewNop(), Recorder: record.NewFakeRecorder(10)}
		cluster := &memgraphv1alpha1.MemgraphCluster{ObjectMeta: metav1.ObjectMeta{Name: "rs3", Namespace: "default"}}
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "rs3", Namespace: "default"}}
		err := r.executeReseed(ctx, cluster, sts, "rs3-0", "rs3-1", time.Now(), func(context.Context, string, string) error {
			return errors.New("exec failed")
		}, zap.NewNop())
		Expect(err).To(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "rs3-1", Namespace: "default"}, &corev1.Pod{})).To(Succeed())
		claim := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data-rs3-1", Namespace: "default"}, claim)).To(Succeed())
		Expect(claim.DeletionTimestamp).To(BeNil())
	})
})
