// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithReady(name string, ready bool) corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}},
		},
	}
}

func TestStaleBackupScheduleRunsOncePerInterval(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	s := newStaleBackupSchedule(func() time.Time { return now })

	if !s.due("ns/a") {
		t.Fatal("first check must be due")
	}
	if s.due("ns/a") {
		t.Fatal("second check right after must not be due")
	}
	if !s.due("ns/b") {
		t.Fatal("another cluster must be due independently")
	}
	now = now.Add(staleBackupCleanupInterval - time.Second)
	if s.due("ns/a") {
		t.Fatal("must not be due before the interval has passed")
	}
	now = now.Add(time.Second)
	if !s.due("ns/a") {
		t.Fatal("must be due once the interval has passed")
	}
}

func TestStaleBackupPods(t *testing.T) {
	pods := []corev1.Pod{podWithReady("g-1", true), podWithReady("g-0", true), podWithReady("g-2", false)}

	if got := staleBackupPods(pods, false); len(got) != 0 {
		t.Errorf("staleBackupPods(streaming=false) = %v, want none: a replica may be recovering", got)
	}
	if got, want := staleBackupPods(pods, true), []string{"g-0", "g-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("staleBackupPods(streaming=true) = %v, want %v (ready pods only, sorted)", got, want)
	}
}
