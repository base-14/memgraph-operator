// Copyright 2025 Base14. See LICENSE file for details.

package controller

import "testing"

func TestEventReasonConstants(t *testing.T) {
	// Verify event reason constants are defined and non-empty
	eventReasons := []struct {
		name  string
		value string
	}{
		{"EventReasonClusterCreated", EventReasonClusterCreated},
		{"EventReasonClusterReady", EventReasonClusterReady},
		{"EventReasonClusterDegraded", EventReasonClusterDegraded},
		{"EventReasonCreatingStatefulSet", EventReasonCreatingStatefulSet},
		{"EventReasonScalingStatefulSet", EventReasonScalingStatefulSet},
		{"EventReasonMainInstanceConfigured", EventReasonMainInstanceConfigured},
		{"EventReasonReplicaRegistered", EventReasonReplicaRegistered},
		{"EventReasonReplicaUnregistered", EventReasonReplicaUnregistered},
		{"EventReasonReplicationHealthy", EventReasonReplicationHealthy},
		{"EventReasonReplicationError", EventReasonReplicationError},
		{"EventReasonReplicaUnhealthy", EventReasonReplicaUnhealthy},
		{"EventReasonReplicationLagHigh", EventReasonReplicationLagHigh},
		{"EventReasonMainInstanceFailed", EventReasonMainInstanceFailed},
		{"EventReasonFailoverStarted", EventReasonFailoverStarted},
		{"EventReasonFailoverCompleted", EventReasonFailoverCompleted},
		{"EventReasonFailoverFailed", EventReasonFailoverFailed},
		{"EventReasonSnapshotCronJobCreated", EventReasonSnapshotCronJobCreated},
		{"EventReasonSnapshotCronJobUpdated", EventReasonSnapshotCronJobUpdated},
		{"EventReasonSnapshotSucceeded", EventReasonSnapshotSucceeded},
		{"EventReasonSnapshotFailed", EventReasonSnapshotFailed},
		{"EventReasonS3BackupSucceeded", EventReasonS3BackupSucceeded},
		{"EventReasonS3BackupFailed", EventReasonS3BackupFailed},
		{"EventReasonHealthCheckPassed", EventReasonHealthCheckPassed},
		{"EventReasonHealthCheckFailed", EventReasonHealthCheckFailed},
		{"EventReasonUpdatedWriteService", EventReasonUpdatedWriteService},
		{"EventReasonReconcileError", EventReasonReconcileError},
	}

	for _, er := range eventReasons {
		t.Run(er.name, func(t *testing.T) {
			if er.value == "" {
				t.Errorf("%s should not be empty", er.name)
			}
		})
	}
}
