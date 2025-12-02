// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"testing"
)

func TestParseShowReplicasOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []ReplicaInfo
	}{
		{
			name:     "Empty output",
			output:   "",
			expected: nil,
		},
		{
			name:     "Header only",
			output:   "+------+------+------+------+--------+\n| name | host | port | mode | status |\n+------+------+------+------+--------+\n",
			expected: nil,
		},
		{
			name: "Single replica",
			output: `+------------------+----------------------------------------+-------+-------+-------------+
| name             | host                                   | port  | mode  | status      |
+------------------+----------------------------------------+-------+-------+-------------+
| cluster_replica1 | cluster-1.cluster-headless.default.svc | 10000 | ASYNC | ready       |
+------------------+----------------------------------------+-------+-------+-------------+`,
			expected: []ReplicaInfo{
				{
					Name:   "cluster_replica1",
					Host:   "cluster-1.cluster-headless.default.svc",
					Mode:   "ASYNC",
					Status: "ready",
				},
			},
		},
		{
			name: "Multiple replicas",
			output: `+------------------+----------------------------------------+-------+-------+-------------+
| name             | host                                   | port  | mode  | status      |
+------------------+----------------------------------------+-------+-------+-------------+
| cluster_0        | cluster-0.cluster-headless.default.svc | 10000 | ASYNC | ready       |
| cluster_1        | cluster-1.cluster-headless.default.svc | 10000 | SYNC  | replicating |
| cluster_2        | cluster-2.cluster-headless.default.svc | 10000 | ASYNC | recovery    |
+------------------+----------------------------------------+-------+-------+-------------+`,
			expected: []ReplicaInfo{
				{Name: "cluster_0", Host: "cluster-0.cluster-headless.default.svc", Mode: "ASYNC", Status: "ready"},
				{Name: "cluster_1", Host: "cluster-1.cluster-headless.default.svc", Mode: "SYNC", Status: "replicating"},
				{Name: "cluster_2", Host: "cluster-2.cluster-headless.default.svc", Mode: "ASYNC", Status: "recovery"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseShowReplicasOutput(tt.output)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d replicas, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i].Name != expected.Name {
					t.Errorf("replica[%d].Name = %s, want %s", i, result[i].Name, expected.Name)
				}
				if result[i].Host != expected.Host {
					t.Errorf("replica[%d].Host = %s, want %s", i, result[i].Host, expected.Host)
				}
				if result[i].Mode != expected.Mode {
					t.Errorf("replica[%d].Mode = %s, want %s", i, result[i].Mode, expected.Mode)
				}
				if result[i].Status != expected.Status {
					t.Errorf("replica[%d].Status = %s, want %s", i, result[i].Status, expected.Status)
				}
			}
		})
	}
}
