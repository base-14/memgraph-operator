// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"testing"
)

func TestParseMemoryValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "plain number",
			input:    "1024",
			expected: 1024,
		},
		{
			name:     "bytes",
			input:    "1024 B",
			expected: 1024,
		},
		{
			name:     "bytes lowercase",
			input:    "512 bytes",
			expected: 512,
		},
		{
			name:     "kilobytes",
			input:    "1 KB",
			expected: 1024,
		},
		{
			name:     "kibibytes",
			input:    "2 KiB",
			expected: 2048,
		},
		{
			name:     "megabytes",
			input:    "1 MB",
			expected: 1024 * 1024,
		},
		{
			name:     "mibibytes",
			input:    "1 MiB",
			expected: 1024 * 1024,
		},
		{
			name:     "gigabytes",
			input:    "1 GB",
			expected: 1024 * 1024 * 1024,
		},
		{
			name:     "gibibytes",
			input:    "2 GiB",
			expected: 2 * 1024 * 1024 * 1024,
		},
		{
			name:     "terabytes",
			input:    "1 TB",
			expected: 1024 * 1024 * 1024 * 1024,
		},
		{
			name:     "tibibytes",
			input:    "1 TiB",
			expected: 1024 * 1024 * 1024 * 1024,
		},
		{
			name:     "decimal megabytes",
			input:    "1.5 MiB",
			expected: int64(1.5 * 1024 * 1024),
		},
		{
			name:     "decimal gigabytes",
			input:    "2.5 GiB",
			expected: int64(2.5 * 1024 * 1024 * 1024),
		},
		{
			name:     "whitespace handling",
			input:    "  512 MiB  ",
			expected: 512 * 1024 * 1024,
		},
		{
			name:     "invalid string",
			input:    "not a number",
			expected: 0,
		},
		{
			name:     "number without unit",
			input:    "1024.5",
			expected: 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemoryValue(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemoryValue(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseStorageInfoOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected *StorageInfo
	}{
		{
			name:     "empty output",
			output:   "",
			expected: &StorageInfo{},
		},
		{
			name: "full storage info",
			output: `+---------------------------+--------------------+
| storage info              | value              |
+---------------------------+--------------------+
| name                      | default            |
| vertex_count              | 1000               |
| edge_count                | 5000               |
| average_degree            | 10.5               |
| memory_res                | 512 MiB            |
| peak_memory_res           | 1 GiB              |
| disk_usage                | 256 MiB            |
| memory_tracked            | 128 MiB            |
| allocation_limit          | 2 GiB              |
| unreleased_delta_objects  | 50                 |
| storage_mode              | IN_MEMORY          |
| global_isolation_level    | SNAPSHOT           |
+---------------------------+--------------------+`,
			expected: &StorageInfo{
				Name:                   "default",
				VertexCount:            1000,
				EdgeCount:              5000,
				AverageDegree:          10.5,
				MemoryRes:              512 * 1024 * 1024,
				PeakMemoryRes:          1024 * 1024 * 1024,
				DiskUsage:              256 * 1024 * 1024,
				MemoryTracked:          128 * 1024 * 1024,
				AllocationLimit:        2 * 1024 * 1024 * 1024,
				UnreleasedDeltaObjects: 50,
				StorageMode:            "IN_MEMORY",
				IsolationLevel:         "SNAPSHOT",
			},
		},
		{
			name: "partial storage info",
			output: `+---------------------------+--------------------+
| storage info              | value              |
+---------------------------+--------------------+
| name                      | test               |
| vertex_count              | 100                |
| edge_count                | 200                |
+---------------------------+--------------------+`,
			expected: &StorageInfo{
				Name:        "test",
				VertexCount: 100,
				EdgeCount:   200,
			},
		},
		{
			name: "with plain number memory values",
			output: `+---------------------------+--------------------+
| storage info              | value              |
+---------------------------+--------------------+
| memory_res                | 1048576            |
| disk_usage                | 2097152            |
+---------------------------+--------------------+`,
			expected: &StorageInfo{
				MemoryRes: 1048576,
				DiskUsage: 2097152,
			},
		},
		{
			name: "with quoted keys and values (actual Memgraph format)",
			output: `+--------------------------------+----------------------------------------+
| storage info                   | value                                  |
+--------------------------------+----------------------------------------+
| "name"                         | "memgraph"                             |
| "vertex_count"                 | 500                                    |
| "edge_count"                   | 1500                                   |
| "average_degree"               | 6.0                                    |
| "memory_res"                   | "43.16MiB"                             |
| "peak_memory_res"              | "100MiB"                               |
| "disk_usage"                   | "10MiB"                                |
| "memory_tracked"               | "8.52MiB"                              |
| "allocation_limit"             | "58.55GiB"                             |
| "unreleased_delta_objects"     | 10                                     |
| "storage_mode"                 | "IN_MEMORY_TRANSACTIONAL"              |
| "global_isolation_level"       | "SNAPSHOT_ISOLATION"                   |
+--------------------------------+----------------------------------------+`,
			expected: &StorageInfo{
				Name:                   "memgraph",
				VertexCount:            500,
				EdgeCount:              1500,
				AverageDegree:          6.0,
				MemoryRes:              45256540, // 43.16 MiB
				PeakMemoryRes:          100 * 1024 * 1024,
				DiskUsage:              10 * 1024 * 1024,
				MemoryTracked:          8933867,     // 8.52 MiB
				AllocationLimit:        62867583795, // 58.55 GiB
				UnreleasedDeltaObjects: 10,
				StorageMode:            "IN_MEMORY_TRANSACTIONAL",
				IsolationLevel:         "SNAPSHOT_ISOLATION",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStorageInfoOutput(tt.output)

			if result.Name != tt.expected.Name {
				t.Errorf("Name = %s, want %s", result.Name, tt.expected.Name)
			}
			if result.VertexCount != tt.expected.VertexCount {
				t.Errorf("VertexCount = %d, want %d", result.VertexCount, tt.expected.VertexCount)
			}
			if result.EdgeCount != tt.expected.EdgeCount {
				t.Errorf("EdgeCount = %d, want %d", result.EdgeCount, tt.expected.EdgeCount)
			}
			if result.AverageDegree != tt.expected.AverageDegree {
				t.Errorf("AverageDegree = %f, want %f", result.AverageDegree, tt.expected.AverageDegree)
			}
			if result.MemoryRes != tt.expected.MemoryRes {
				t.Errorf("MemoryRes = %d, want %d", result.MemoryRes, tt.expected.MemoryRes)
			}
			if result.PeakMemoryRes != tt.expected.PeakMemoryRes {
				t.Errorf("PeakMemoryRes = %d, want %d", result.PeakMemoryRes, tt.expected.PeakMemoryRes)
			}
			if result.DiskUsage != tt.expected.DiskUsage {
				t.Errorf("DiskUsage = %d, want %d", result.DiskUsage, tt.expected.DiskUsage)
			}
			if result.MemoryTracked != tt.expected.MemoryTracked {
				t.Errorf("MemoryTracked = %d, want %d", result.MemoryTracked, tt.expected.MemoryTracked)
			}
			if result.AllocationLimit != tt.expected.AllocationLimit {
				t.Errorf("AllocationLimit = %d, want %d", result.AllocationLimit, tt.expected.AllocationLimit)
			}
			if result.UnreleasedDeltaObjects != tt.expected.UnreleasedDeltaObjects {
				t.Errorf("UnreleasedDeltaObjects = %d, want %d", result.UnreleasedDeltaObjects, tt.expected.UnreleasedDeltaObjects)
			}
			if result.StorageMode != tt.expected.StorageMode {
				t.Errorf("StorageMode = %s, want %s", result.StorageMode, tt.expected.StorageMode)
			}
			if result.IsolationLevel != tt.expected.IsolationLevel {
				t.Errorf("IsolationLevel = %s, want %s", result.IsolationLevel, tt.expected.IsolationLevel)
			}
		})
	}
}

func TestParseMemoryValueEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{
			name:     "just whitespace",
			input:    "   ",
			expected: 0,
		},
		{
			name:     "unknown unit",
			input:    "100 UNKNOWN",
			expected: 100,
		},
		{
			name:     "negative number",
			input:    "-100",
			expected: -100,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
		},
		{
			name:     "large plain number",
			input:    "9999999999",
			expected: 9999999999,
		},
		{
			name:     "decimal number with leading zero",
			input:    "0.5 GB",
			expected: int64(0.5 * 1024 * 1024 * 1024),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemoryValue(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemoryValue(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

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
			name:     "Header only (legacy)",
			output:   "+------+------+------+------+--------+\n| name | host | port | mode | status |\n+------+------+------+------+--------+\n",
			expected: nil,
		},
		{
			name: "Legacy format single replica",
			output: `+------------------+----------------------------------------+-------+-------+-------------+
| name             | host                                   | port  | mode  | status      |
+------------------+----------------------------------------+-------+-------+-------------+
| cluster_replica1 | cluster-1.cluster-headless.default.svc | 10000 | ASYNC | ready       |
+------------------+----------------------------------------+-------+-------+-------------+`,
			expected: []ReplicaInfo{
				{
					Name:     "cluster_replica1",
					Host:     "cluster-1.cluster-headless.default.svc",
					Port:     10000,
					SyncMode: "ASYNC",
					Status:   "ready",
				},
			},
		},
		{
			name: "Legacy format multiple replicas",
			output: `+------------------+----------------------------------------+-------+-------+-------------+
| name             | host                                   | port  | mode  | status      |
+------------------+----------------------------------------+-------+-------+-------------+
| cluster_0        | cluster-0.cluster-headless.default.svc | 10000 | ASYNC | ready       |
| cluster_1        | cluster-1.cluster-headless.default.svc | 10000 | SYNC  | replicating |
| cluster_2        | cluster-2.cluster-headless.default.svc | 10000 | ASYNC | recovery    |
+------------------+----------------------------------------+-------+-------+-------------+`,
			expected: []ReplicaInfo{
				{Name: "cluster_0", Host: "cluster-0.cluster-headless.default.svc", Port: 10000, SyncMode: "ASYNC", Status: "ready"},
				{Name: "cluster_1", Host: "cluster-1.cluster-headless.default.svc", Port: 10000, SyncMode: "SYNC", Status: "replicating"},
				{Name: "cluster_2", Host: "cluster-2.cluster-headless.default.svc", Port: 10000, SyncMode: "ASYNC", Status: "recovery"},
			},
		},
		{
			name: "Current format healthy replica",
			output: `+-------------+------------------+-----------+-------------+--------------------------------------------------+
| name        | socket_address   | sync_mode | system_info | data_info                                        |
+-------------+------------------+-----------+-------------+--------------------------------------------------+
| "replica_1" | "host-1.svc:10000" | "async" | Null        | {memgraph: {behind: 0, status: "ready", ts: 42}} |
+-------------+------------------+-----------+-------------+--------------------------------------------------+`,
			expected: []ReplicaInfo{
				{
					Name:          "replica_1",
					SocketAddress: "host-1.svc:10000",
					Host:          "host-1.svc",
					Port:          10000,
					SyncMode:      "async",
					Status:        ReplicaStatusReady,
					Behind:        0,
					Timestamp:     42,
					DataInfo:      map[string]ReplicaDBInfo{"memgraph": {Behind: 0, Status: "ready", Timestamp: 42}},
				},
			},
		},
		{
			name:   "Current format empty data_info means invalid (desynced replica shape)",
			output: `| "my_cluster_graph_0" | "my-cluster-graph-0.my-cluster-graph-hl.tenant-ns.svc.cluster.local:10000" | "async" | Null | {} |`,
			expected: []ReplicaInfo{
				{
					Name:          "my_cluster_graph_0",
					SocketAddress: "my-cluster-graph-0.my-cluster-graph-hl.tenant-ns.svc.cluster.local:10000",
					Host:          "my-cluster-graph-0.my-cluster-graph-hl.tenant-ns.svc.cluster.local",
					Port:          10000,
					SyncMode:      "async",
					Status:        ReplicaStatusInvalid,
					Behind:        0,
					Timestamp:     0,
					DataInfo:      nil,
				},
			},
		},
		{
			name:   "Current format Null data_info means invalid",
			output: `| "replica_1" | "host-1.svc:10000" | "sync" | Null | Null |`,
			expected: []ReplicaInfo{
				{
					Name:          "replica_1",
					SocketAddress: "host-1.svc:10000",
					Host:          "host-1.svc",
					Port:          10000,
					SyncMode:      "sync",
					Status:        ReplicaStatusInvalid,
				},
			},
		},
		{
			name:   "Current format invalid replica with negative behind",
			output: `| "replica_0" | "host-0.svc:10000" | "sync" | Null | {memgraph: {behind: -14984497, status: "invalid", ts: 0}} |`,
			expected: []ReplicaInfo{
				{
					Name:          "replica_0",
					SocketAddress: "host-0.svc:10000",
					Host:          "host-0.svc",
					Port:          10000,
					SyncMode:      "sync",
					Status:        ReplicaStatusInvalid,
					Behind:        -14984497,
					Timestamp:     0,
					DataInfo:      map[string]ReplicaDBInfo{"memgraph": {Behind: -14984497, Status: "invalid", Timestamp: 0}},
				},
			},
		},
		{
			name:   "Current format multi-database worst status wins",
			output: `| "replica_1" | "host-1.svc:10000" | "async" | Null | {memgraph: {behind: 0, status: "ready", ts: 42}, otherdb: {behind: 5, status: "recovery", ts: 10}} |`,
			expected: []ReplicaInfo{
				{
					Name:          "replica_1",
					SocketAddress: "host-1.svc:10000",
					Host:          "host-1.svc",
					Port:          10000,
					SyncMode:      "async",
					Status:        ReplicaStatusRecovery,
					Behind:        5,
					Timestamp:     42,
					DataInfo: map[string]ReplicaDBInfo{
						"memgraph": {Behind: 0, Status: "ready", Timestamp: 42},
						"otherdb":  {Behind: 5, Status: "recovery", Timestamp: 10},
					},
				},
			},
		},
		{
			name:     "Line starting with + (border)",
			output:   "+------+------+------+------+--------+",
			expected: nil,
		},
		{
			name:     "Malformed line - not enough columns",
			output:   "| a | b |",
			expected: nil,
		},
		{
			name:     "Empty name after parsing",
			output:   "| | host | port | mode | status |",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseShowReplicasOutput(tt.output)

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d replicas, got %d (%+v)", len(tt.expected), len(result), result)
			}

			for i, expected := range tt.expected {
				got := result[i]
				if got.Name != expected.Name {
					t.Errorf("replica[%d].Name = %s, want %s", i, got.Name, expected.Name)
				}
				if got.Host != expected.Host {
					t.Errorf("replica[%d].Host = %s, want %s", i, got.Host, expected.Host)
				}
				if got.Port != expected.Port {
					t.Errorf("replica[%d].Port = %d, want %d", i, got.Port, expected.Port)
				}
				if got.SyncMode != expected.SyncMode {
					t.Errorf("replica[%d].SyncMode = %s, want %s", i, got.SyncMode, expected.SyncMode)
				}
				if got.Status != expected.Status {
					t.Errorf("replica[%d].Status = %s, want %s", i, got.Status, expected.Status)
				}
				if got.Behind != expected.Behind {
					t.Errorf("replica[%d].Behind = %d, want %d", i, got.Behind, expected.Behind)
				}
				if got.Timestamp != expected.Timestamp {
					t.Errorf("replica[%d].Timestamp = %d, want %d", i, got.Timestamp, expected.Timestamp)
				}
				if len(got.DataInfo) != len(expected.DataInfo) {
					t.Errorf("replica[%d].DataInfo has %d entries, want %d", i, len(got.DataInfo), len(expected.DataInfo))
				}
				for db, expectedDB := range expected.DataInfo {
					if got.DataInfo[db] != expectedDB {
						t.Errorf("replica[%d].DataInfo[%s] = %+v, want %+v", i, db, got.DataInfo[db], expectedDB)
					}
				}
			}
		})
	}
}

func TestReplicaInfoIsHealthy(t *testing.T) {
	tests := []struct {
		status  string
		healthy bool
	}{
		{ReplicaStatusReady, true},
		{ReplicaStatusReplicating, true},
		{ReplicaStatusRecovery, false},
		{ReplicaStatusInvalid, false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			r := ReplicaInfo{Status: tt.status}
			if r.IsHealthy() != tt.healthy {
				t.Errorf("IsHealthy() with status %q = %v, want %v", tt.status, r.IsHealthy(), tt.healthy)
			}
		})
	}
}

func TestReplicaInfoDataInfoPresent(t *testing.T) {
	withData := ReplicaInfo{DataInfo: map[string]ReplicaDBInfo{"memgraph": {Status: "ready"}}}
	if !withData.DataInfoPresent() {
		t.Error("DataInfoPresent() = false for non-empty DataInfo, want true")
	}

	empty := ReplicaInfo{}
	if empty.DataInfoPresent() {
		t.Error("DataInfoPresent() = true for nil DataInfo, want false")
	}
}

func TestParseDataInfo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]ReplicaDBInfo
	}{
		{"Empty braces", "{}", nil},
		{"Null", "Null", nil},
		{"Empty string", "", nil},
		{"Garbage", "not a map", nil},
		{
			name:     "Single database",
			input:    `{memgraph: {behind: 0, status: "ready", ts: 2}}`,
			expected: map[string]ReplicaDBInfo{"memgraph": {Behind: 0, Status: "ready", Timestamp: 2}},
		},
		{
			name:     "Quoted wrapper",
			input:    `"{memgraph: {behind: 3, status: "replicating", ts: 7}}"`,
			expected: map[string]ReplicaDBInfo{"memgraph": {Behind: 3, Status: "replicating", Timestamp: 7}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDataInfo(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("parseDataInfo(%q) has %d entries, want %d", tt.input, len(result), len(tt.expected))
			}
			for db, expectedDB := range tt.expected {
				if result[db] != expectedDB {
					t.Errorf("parseDataInfo(%q)[%s] = %+v, want %+v", tt.input, db, result[db], expectedDB)
				}
			}
		})
	}
}
