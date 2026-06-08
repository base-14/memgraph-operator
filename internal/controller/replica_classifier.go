// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"strings"
	"time"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

// replicaClassification is the outcome of evaluating a single replica's state
// against the operator's health policy.
type replicaClassification int

const (
	classificationHealthy         replicaClassification = iota // ready/recovery/replicating, caught up
	classificationBehind                                       // behind > 0 but within behindAlertThreshold
	classificationTransient                                    // data_info empty for < channelDownGracePeriod
	classificationDataChannelDown                              // data_info empty for >= channelDownGracePeriod
	classificationInvalid                                      // any DB status == "invalid"
	classificationUnknownStatus                                // any DB status not in known set
	classificationBehindTooLong                                // behind > 0 longer than behindAlertThreshold
)

// channelDownGracePeriod is how long an empty data_info must persist before
// being flagged as DataChannelDown. Memgraph normally populates data_info
// within a few seconds of registration.
const channelDownGracePeriod = 30 * time.Second

// isHealthy reports whether a classification counts toward HealthyReplicas.
// Transient and Behind are "in-progress" warnings, not failures.
func (c replicaClassification) isHealthy() bool {
	switch c {
	case classificationHealthy, classificationBehind, classificationTransient:
		return true
	default:
		return false
	}
}

// replicaState holds per-replica timers that span health checks.
// The zero value means "no timer started".
type replicaState struct {
	behindSince      time.Time // when did the replica first have behind > 0 without recovery
	channelDownSince time.Time // when did the replica first have empty data_info
}

// classifyReplica evaluates one replica against per-replica state.
// It mutates state in-place (start/clear timers) and returns the classification.
//
// Policy (from spec 2026-05-25-replica-health-detection-design):
//   - empty DataInfo
//   - first seen / within grace -> Transient
//   - persisted past grace      -> DataChannelDown
//   - any DB status == "invalid"  -> Invalid (terminal, returned immediately)
//   - any DB status unknown       -> UnknownStatus (defensive)
//   - any DB Behind > 0
//   - first seen / within threshold -> Behind
//   - persisted past threshold      -> BehindTooLong
//   - else                        -> Healthy
//
// Negative Behind values are ignored entirely (they indicate replica-side
// divergence, which is a separate concern parked for a follow-up).
func classifyReplica(replica memgraph.ReplicaInfo, state *replicaState, now time.Time, behindThreshold time.Duration) replicaClassification {
	if len(replica.DataInfo) == 0 {
		if state.channelDownSince.IsZero() {
			state.channelDownSince = now
		}
		// Channel is down → lag is undefined; clear the behind timer so the
		// memgraph_replica_behind_seconds gauge does not keep growing.
		state.behindSince = time.Time{}
		if now.Sub(state.channelDownSince) > channelDownGracePeriod {
			return classificationDataChannelDown
		}
		return classificationTransient
	}
	state.channelDownSince = time.Time{}

	// First pass: check for terminal "invalid" status without mutating state.
	for _, db := range replica.DataInfo {
		if dbStatus(db) == memgraph.ReplicaStatusInvalid {
			return classificationInvalid
		}
	}

	worst := classificationHealthy
	anyBehind := false

	for _, db := range replica.DataInfo {
		switch dbStatus(db) {
		case memgraph.ReplicaStatusReady, memgraph.ReplicaStatusRecovery, memgraph.ReplicaStatusReplicating:
			// known good
		default:
			worst = classificationUnknownStatus
		}

		if db.Behind > 0 {
			anyBehind = true
			if state.behindSince.IsZero() {
				state.behindSince = now
			}
			if now.Sub(state.behindSince) > behindThreshold {
				return classificationBehindTooLong
			}
			if worst == classificationHealthy {
				worst = classificationBehind
			}
		}
	}

	if !anyBehind {
		state.behindSince = time.Time{}
	}

	return worst
}

// dbStatus normalizes a per-database status the same way summarizeDataInfo does
// in the memgraph client, so classification is robust to casing/quoting.
func dbStatus(db memgraph.ReplicaDBInfo) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(db.Status), "\""))
}
