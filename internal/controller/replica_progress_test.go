// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"testing"
	"time"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

type replicaSample struct {
	status string
	behind int64
	ts     int64
}

func runHealthChecks(samples []replicaSample) (reseedAt int, classes []replicaClassification) {
	base := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	st := &replicaState{}
	reseedAt = -1
	for i, s := range samples {
		now := base.Add(time.Duration(i) * 30 * time.Second)
		r := memgraph.ReplicaInfo{
			Name:     "axi_scout_kb_graph_0",
			DataInfo: map[string]memgraph.ReplicaDBInfo{"memgraph": {Status: s.status, Behind: s.behind, Timestamp: s.ts}},
		}
		class := classifyReplica(r, st, now, 5*time.Minute, 5*time.Minute)
		classes = append(classes, class)
		if reseedAt < 0 && shouldReseed(class, st, now, 5*time.Minute) {
			reseedAt = i
		}
	}
	return reseedAt, classes
}

func TestStreamingReplicaIsNeverReseeded(t *testing.T) {
	t.Run("axi on uks1: flapping invalid and negative behind while ts advances", func(t *testing.T) {
		pattern := []replicaSample{
			{"recovery", -13, 0}, {"invalid", -3, 0}, {"recovery", -15, 0}, {"recovery", -41, 0}, {"invalid", -3, 0},
		}
		var samples []replicaSample
		ts := int64(25697199)
		for i := 0; i < 24; i++ {
			s := pattern[i%len(pattern)]
			ts += 150
			s.ts = ts
			samples = append(samples, s)
		}
		reseedAt, classes := runHealthChecks(samples)
		if reseedAt >= 0 {
			t.Fatalf("streaming replica re-seeded at check %d (%v); ts advanced on every check", reseedAt, classes[reseedAt])
		}
		sawUnhealthy := false
		for _, c := range classes {
			if !c.isHealthy() {
				sawUnhealthy = true
			}
		}
		if !sawUnhealthy {
			t.Error("a flapping replica must still be reported unhealthy so it is taken out of the read service")
		}
	})

	t.Run("negative behind for ten minutes while ts advances", func(t *testing.T) {
		var samples []replicaSample
		for i := 0; i < 20; i++ {
			samples = append(samples, replicaSample{"ready", -24, int64(1000 + i*10)})
		}
		if reseedAt, classes := runHealthChecks(samples); reseedAt >= 0 {
			t.Fatalf("re-seeded at check %d (%v)", reseedAt, classes[reseedAt])
		}
	})

	t.Run("recovery for ten minutes while ts advances", func(t *testing.T) {
		var samples []replicaSample
		for i := 0; i < 20; i++ {
			samples = append(samples, replicaSample{"recovery", 0, int64(1000 + i*10)})
		}
		if reseedAt, classes := runHealthChecks(samples); reseedAt >= 0 {
			t.Fatalf("re-seeded at check %d (%v)", reseedAt, classes[reseedAt])
		}
	})
}

func TestFrozenReplicaIsReseeded(t *testing.T) {
	frozen := func(status string, behind int64, checks int) []replicaSample {
		var s []replicaSample
		for i := 0; i < checks; i++ {
			s = append(s, replicaSample{status, behind, 5000})
		}
		return s
	}

	tests := []struct {
		name    string
		samples []replicaSample
	}{
		{"invalid with ts stuck", frozen("invalid", 0, 14)},
		{"negative behind with ts stuck (diverged)", frozen("ready", -24, 14)},
		{"recovery with ts stuck", frozen("recovery", 0, 14)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reseedAt, classes := runHealthChecks(tt.samples)
			if reseedAt < 0 {
				t.Fatalf("frozen replica never re-seeded, classes %v", classes)
			}
			if reseedAt < 10 {
				t.Errorf("re-seeded at check %d (%s), before 5 minutes without progress", reseedAt, time.Duration(reseedAt)*30*time.Second)
			}
		})
	}

	t.Run("progress restarts the clock", func(t *testing.T) {
		samples := frozen("invalid", 0, 8)
		samples = append(samples, replicaSample{"invalid", 0, 5001})
		samples = append(samples, frozen("invalid", 0, 8)...)
		for i := 9; i < len(samples); i++ {
			samples[i].ts = 5001
		}
		if reseedAt, _ := runHealthChecks(samples); reseedAt >= 0 {
			t.Errorf("re-seeded at check %d although ts moved at check 8 and no stretch reached 5 minutes", reseedAt)
		}
	})
}
