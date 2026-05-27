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

func TestParseShowReplicasCSV(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    []ReplicaInfo
		wantErr bool
	}{
		{
			name:    "empty output",
			output:  "",
			want:    nil,
			wantErr: false,
		},
		{
			name: "header only",
			output: `"name","socket_address","sync_mode","system_info","data_info"
`,
			want:    nil,
			wantErr: false,
		},
		{
			name: "single replica with empty data_info",
			output: `"name","socket_address","sync_mode","system_info","data_info"
"replica_0","host-0.svc:10000","async","null","{}"
`,
			want: []ReplicaInfo{
				{
					Name:     "replica_0",
					Host:     "host-0.svc:10000",
					Mode:     "async",
					DataInfo: map[string]ReplicaDatabaseStatus{},
				},
			},
		},
		{
			name: "single replica with memgraph db in recovery, negative behind",
			output: `"name","socket_address","sync_mode","system_info","data_info"
"replica_1","host-1.svc:10000","async","null","{memgraph: {behind: -8, status: ""recovery"", ts: 27335187}}"
`,
			want: []ReplicaInfo{
				{
					Name: "replica_1",
					Host: "host-1.svc:10000",
					Mode: "async",
					DataInfo: map[string]ReplicaDatabaseStatus{
						"memgraph": {Status: "recovery", Behind: -8, Ts: 27335187},
					},
				},
			},
		},
		{
			name: "single replica ready, behind zero",
			output: `"name","socket_address","sync_mode","system_info","data_info"
"replica_2","host-2.svc:10000","sync","null","{memgraph: {behind: 0, status: ""ready"", ts: 42}}"
`,
			want: []ReplicaInfo{
				{
					Name: "replica_2",
					Host: "host-2.svc:10000",
					Mode: "sync",
					DataInfo: map[string]ReplicaDatabaseStatus{
						"memgraph": {Status: "ready", Behind: 0, Ts: 42},
					},
				},
			},
		},
		{
			name: "single replica invalid status",
			output: `"name","socket_address","sync_mode","system_info","data_info"
"replica_3","host-3.svc:10000","async","null","{memgraph: {behind: 5, status: ""invalid"", ts: 100}}"
`,
			want: []ReplicaInfo{
				{
					Name: "replica_3",
					Host: "host-3.svc:10000",
					Mode: "async",
					DataInfo: map[string]ReplicaDatabaseStatus{
						"memgraph": {Status: "invalid", Behind: 5, Ts: 100},
					},
				},
			},
		},
		{
			name: "multiple replicas mixed states",
			output: `"name","socket_address","sync_mode","system_info","data_info"
"replica_0","host-0.svc:10000","async","null","{}"
"replica_1","host-1.svc:10000","async","null","{memgraph: {behind: 0, status: ""ready"", ts: 100}}"
"replica_2","host-2.svc:10000","strict_sync","null","{memgraph: {behind: 3, status: ""replicating"", ts: 97}}"
`,
			want: []ReplicaInfo{
				{
					Name:     "replica_0",
					Host:     "host-0.svc:10000",
					Mode:     "async",
					DataInfo: map[string]ReplicaDatabaseStatus{},
				},
				{
					Name: "replica_1",
					Host: "host-1.svc:10000",
					Mode: "async",
					DataInfo: map[string]ReplicaDatabaseStatus{
						"memgraph": {Status: "ready", Behind: 0, Ts: 100},
					},
				},
				{
					Name: "replica_2",
					Host: "host-2.svc:10000",
					Mode: "strict_sync",
					DataInfo: map[string]ReplicaDatabaseStatus{
						"memgraph": {Status: "replicating", Behind: 3, Ts: 97},
					},
				},
			},
		},
		{
			name: "malformed csv returns error",
			output: `"name","socket_address"
"unterminated
`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseShowReplicasCSV(tt.output)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseShowReplicasCSV() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d replicas, want %d (got=%#v)", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].Name != w.Name {
					t.Errorf("[%d].Name = %q, want %q", i, got[i].Name, w.Name)
				}
				if got[i].Host != w.Host {
					t.Errorf("[%d].Host = %q, want %q", i, got[i].Host, w.Host)
				}
				if got[i].Mode != w.Mode {
					t.Errorf("[%d].Mode = %q, want %q", i, got[i].Mode, w.Mode)
				}
				if len(got[i].DataInfo) != len(w.DataInfo) {
					t.Errorf("[%d].DataInfo size = %d, want %d", i, len(got[i].DataInfo), len(w.DataInfo))
				}
				for db, ws := range w.DataInfo {
					gs, ok := got[i].DataInfo[db]
					if !ok {
						t.Errorf("[%d].DataInfo[%q] missing", i, db)
						continue
					}
					if gs.Status != ws.Status {
						t.Errorf("[%d].DataInfo[%q].Status = %q, want %q", i, db, gs.Status, ws.Status)
					}
					if gs.Behind != ws.Behind {
						t.Errorf("[%d].DataInfo[%q].Behind = %d, want %d", i, db, gs.Behind, ws.Behind)
					}
					if gs.Ts != ws.Ts {
						t.Errorf("[%d].DataInfo[%q].Ts = %d, want %d", i, db, gs.Ts, ws.Ts)
					}
				}
			}
		})
	}
}
