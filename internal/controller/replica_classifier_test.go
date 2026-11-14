// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"
	"time"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

func TestClassifyReplica(t *testing.T) {
	baseTime := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	behindThreshold := 5 * time.Minute

	tests := []struct {
		name      string
		replica   memgraph.ReplicaInfo
		state     *replicaState
		now       time.Time
		want      replicaClassification
		wantState replicaState // expected state AFTER classification
	}{
		{
			name: "empty data_info first seen -> transient (channelDownSince set to now)",
			replica: memgraph.ReplicaInfo{
				Name:     "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationTransient,
			wantState: replicaState{channelDownSince: baseTime},
		},
		{
			name: "empty data_info still within 30s -> transient",
			replica: memgraph.ReplicaInfo{
				Name:     "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{},
			},
			state:     &replicaState{channelDownSince: baseTime},
			now:       baseTime.Add(20 * time.Second),
			want:      classificationTransient,
			wantState: replicaState{channelDownSince: baseTime},
		},
		{
			name: "empty data_info past 30s -> DataChannelDown",
			replica: memgraph.ReplicaInfo{
				Name:     "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{},
			},
			state:     &replicaState{channelDownSince: baseTime},
			now:       baseTime.Add(31 * time.Second),
			want:      classificationDataChannelDown,
			wantState: replicaState{channelDownSince: baseTime},
		},
		{
			name: "data_info populated clears channelDownSince",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "ready", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{channelDownSince: baseTime},
			now:       baseTime.Add(10 * time.Second),
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "ready healthy",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "ready", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "replicating healthy",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "replicating", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "recovery healthy",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "recovery", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "invalid -> Invalid (terminal)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "invalid", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationInvalid,
			wantState: replicaState{},
		},
		{
			name: "unknown status -> UnknownStatus",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "weird", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationUnknownStatus,
			wantState: replicaState{},
		},
		{
			name: "uppercase/quoted status normalized (READY -> healthy)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: `"READY"`, Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "behind > 0 first seen -> Behind (sets behindSince)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "replicating", Behind: 10, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationBehind,
			wantState: replicaState{behindSince: baseTime},
		},
		{
			name: "behind > 0 within threshold -> Behind",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "replicating", Behind: 10, Timestamp: 1},
				},
			},
			state:     &replicaState{behindSince: baseTime},
			now:       baseTime.Add(4 * time.Minute),
			want:      classificationBehind,
			wantState: replicaState{behindSince: baseTime},
		},
		{
			name: "behind > 0 past threshold -> BehindTooLong",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "replicating", Behind: 10, Timestamp: 1},
				},
			},
			state:     &replicaState{behindSince: baseTime},
			now:       baseTime.Add(5*time.Minute + time.Second),
			want:      classificationBehindTooLong,
			wantState: replicaState{behindSince: baseTime},
		},
		{
			name: "caught up clears behindSince",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "ready", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{behindSince: baseTime},
			now:       baseTime.Add(time.Minute),
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "channel-down clears behindSince",
			replica: memgraph.ReplicaInfo{
				Name:     "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{},
			},
			state:     &replicaState{behindSince: baseTime},
			now:       baseTime.Add(10 * time.Second),
			want:      classificationTransient,
			wantState: replicaState{channelDownSince: baseTime.Add(10 * time.Second)},
		},
		{
			name: "negative behind ignored (not Behind)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"memgraph": {Status: "recovery", Behind: -8, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "invalid wins over behind in multi-db",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDBInfo{
					"db_a": {Status: "replicating", Behind: 999, Timestamp: 1},
					"db_b": {Status: "invalid", Behind: 0, Timestamp: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationInvalid,
			wantState: replicaState{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyReplica(tt.replica, tt.state, tt.now, behindThreshold, behindThreshold)
			if got != tt.want {
				t.Errorf("classifyReplica() = %v, want %v", got, tt.want)
			}
			if !tt.state.behindSince.Equal(tt.wantState.behindSince) {
				t.Errorf("state.behindSince = %v, want %v", tt.state.behindSince, tt.wantState.behindSince)
			}
			if !tt.state.channelDownSince.Equal(tt.wantState.channelDownSince) {
				t.Errorf("state.channelDownSince = %v, want %v", tt.state.channelDownSince, tt.wantState.channelDownSince)
			}
		})
	}
}

func TestClassificationIsHealthy(t *testing.T) {
	healthy := []replicaClassification{classificationHealthy, classificationBehind, classificationTransient}
	unhealthy := []replicaClassification{classificationDataChannelDown, classificationInvalid, classificationUnknownStatus, classificationBehindTooLong, classificationStuckRecovery, classificationDiverged}

	for _, c := range healthy {
		if !c.isHealthy() {
			t.Errorf("%v should be healthy", c)
		}
	}
	for _, c := range unhealthy {
		if c.isHealthy() {
			t.Errorf("%v should be unhealthy", c)
		}
	}
}

func TestClassifyReplicaFrozen(t *testing.T) {
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	frozenAfter := 5 * time.Minute
	behindThreshold := 5 * time.Minute
	replica := func(status string, behind int64) memgraph.ReplicaInfo {
		return memgraph.ReplicaInfo{
			Name:     "r0",
			DataInfo: map[string]memgraph.ReplicaDBInfo{"memgraph": {Status: status, Behind: behind, Timestamp: 1}},
		}
	}

	t.Run("recovery first seen is healthy and starts the timer", func(t *testing.T) {
		st := &replicaState{}
		if got := classifyReplica(replica("recovery", 0), st, base, behindThreshold, frozenAfter); got != classificationHealthy {
			t.Errorf("got %v, want healthy", got)
		}
		if !st.recoverySince.Equal(base) {
			t.Errorf("recoverySince = %v, want %v", st.recoverySince, base)
		}
	})

	t.Run("recovery longer than frozenAfter is stuck", func(t *testing.T) {
		st := &replicaState{recoverySince: base}
		if got := classifyReplica(replica("recovery", 0), st, base.Add(frozenAfter+time.Second), behindThreshold, frozenAfter); got != classificationStuckRecovery {
			t.Errorf("got %v, want stuck recovery", got)
		}
	})

	t.Run("ready clears recovery and divergence timers", func(t *testing.T) {
		st := &replicaState{recoverySince: base, divergedSince: base, invalidSince: base}
		if got := classifyReplica(replica("ready", 0), st, base.Add(time.Minute), behindThreshold, frozenAfter); got != classificationHealthy {
			t.Errorf("got %v, want healthy", got)
		}
		if !st.recoverySince.IsZero() || !st.divergedSince.IsZero() || !st.invalidSince.IsZero() {
			t.Errorf("timers not cleared: %+v", st)
		}
	})

	t.Run("negative behind first seen is not unhealthy yet", func(t *testing.T) {
		st := &replicaState{}
		if got := classifyReplica(replica("ready", -24), st, base, behindThreshold, frozenAfter); !got.isHealthy() {
			t.Errorf("got %v, want a healthy classification", got)
		}
		if !st.divergedSince.Equal(base) {
			t.Errorf("divergedSince = %v, want %v", st.divergedSince, base)
		}
	})

	t.Run("negative behind longer than frozenAfter is diverged", func(t *testing.T) {
		st := &replicaState{divergedSince: base}
		if got := classifyReplica(replica("ready", -24), st, base.Add(frozenAfter+time.Second), behindThreshold, frozenAfter); got != classificationDiverged {
			t.Errorf("got %v, want diverged", got)
		}
	})

	t.Run("invalid starts the invalid timer", func(t *testing.T) {
		st := &replicaState{}
		if got := classifyReplica(replica("invalid", 0), st, base, behindThreshold, frozenAfter); got != classificationInvalid {
			t.Errorf("got %v, want invalid", got)
		}
		if !st.invalidSince.Equal(base) {
			t.Errorf("invalidSince = %v, want %v", st.invalidSince, base)
		}
	})
}

func TestShouldReseed(t *testing.T) {
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	after := 5 * time.Minute

	tests := []struct {
		name  string
		class replicaClassification
		state replicaState
		now   time.Time
		want  bool
	}{
		{"channel down for less than reseedAfter", classificationDataChannelDown, replicaState{channelDownSince: base}, base.Add(4 * time.Minute), false},
		{"channel down for reseedAfter", classificationDataChannelDown, replicaState{channelDownSince: base}, base.Add(after), true},
		{"invalid for reseedAfter", classificationInvalid, replicaState{invalidSince: base}, base.Add(after), true},
		{"invalid just now", classificationInvalid, replicaState{invalidSince: base}, base.Add(time.Minute), false},
		{"stuck recovery", classificationStuckRecovery, replicaState{recoverySince: base}, base.Add(after + time.Second), true},
		{"diverged", classificationDiverged, replicaState{divergedSince: base}, base.Add(after + time.Second), true},
		{"healthy never", classificationHealthy, replicaState{}, base.Add(time.Hour), false},
		{"behind too long is still streaming", classificationBehindTooLong, replicaState{behindSince: base}, base.Add(time.Hour), false},
		{"unknown status is not reseeded", classificationUnknownStatus, replicaState{}, base.Add(time.Hour), false},
		{"transient is not reseeded", classificationTransient, replicaState{channelDownSince: base}, base.Add(10 * time.Second), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := tt.state
			if got := shouldReseed(tt.class, &st, tt.now, after); got != tt.want {
				t.Errorf("shouldReseed() = %v, want %v", got, tt.want)
			}
		})
	}
}
