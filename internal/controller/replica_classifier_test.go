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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "ready", Behind: 0, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "ready", Behind: 0, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "replicating", Behind: 0, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "recovery", Behind: 0, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "invalid", Behind: 0, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "weird", Behind: 0, Ts: 1},
				},
			},
			state:     &replicaState{},
			now:       baseTime,
			want:      classificationUnknownStatus,
			wantState: replicaState{},
		},
		{
			name: "behind > 0 first seen -> Behind (sets behindSince)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "replicating", Behind: 10, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "replicating", Behind: 10, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "replicating", Behind: 10, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "ready", Behind: 0, Ts: 1},
				},
			},
			state:     &replicaState{behindSince: baseTime},
			now:       baseTime.Add(time.Minute),
			want:      classificationHealthy,
			wantState: replicaState{},
		},
		{
			name: "negative behind ignored (not Behind)",
			replica: memgraph.ReplicaInfo{
				Name: "r0",
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"memgraph": {Status: "recovery", Behind: -8, Ts: 1},
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
				DataInfo: map[string]memgraph.ReplicaDatabaseStatus{
					"db_a": {Status: "replicating", Behind: 999, Ts: 1},
					"db_b": {Status: "invalid", Behind: 0, Ts: 1},
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
			got := classifyReplica(tt.replica, tt.state, tt.now, behindThreshold)
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
	unhealthy := []replicaClassification{classificationDataChannelDown, classificationInvalid, classificationUnknownStatus, classificationBehindTooLong}

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
