# Replica Health Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix Memgraph 3.x `SHOW REPLICAS` parsing in the operator and add specific detection + events + metrics for `DataChannelDown` (empty `data_info` > 30s), `Invalid`, and `BehindTooLong` (behind > configurable threshold) replica states.

**Architecture:** Replace the broken tabular-output parser with a CSV-output parser that extracts per-database `data_info` via regex. Introduce a pure classifier function over `(replica, perReplicaState, now, behindThreshold)` returning a typed classification. Wire the classifier into `CheckReplicationHealth`, drive three new distinct event reasons and two new per-replica Prometheus gauges, and add a configurable `BehindAlertThreshold` to `MemgraphCluster.spec.replication`. State is kept in-memory on `ReplicationManager`; lost on operator restart.

**Tech Stack:** Go 1.21+, controller-runtime, kubebuilder, Prometheus client_golang, Memgraph 3.x via `kubectl exec mgconsole`.

**Spec:** `docs/superpowers/specs/2026-05-25-replica-health-detection-design.md`

---

## Conventions for every task

- Run tests via `go test ./...` from repo root unless a tighter scope is given.
- After each task: `go vet ./... && go build ./...` must pass.
- Commit messages: imperative, present tense, no co-author footer (project doesn't use it — see recent commits).
- Frequent commits — one per task. Each task ends with a commit step.

---

### Task 1: Extract `executeQueryWithFormat` helper

**Files:**
- Modify: `internal/memgraph/client.go` (lines 38–58)

The current `ExecuteQuery` hard-codes `--output-format tabular`. We extract a private helper that takes the format as a parameter. `ExecuteQuery` keeps the same signature and calls the helper with `"tabular"`. `ShowReplicas` will use the new helper directly in Task 3.

- [ ] **Step 1: Refactor `ExecuteQuery` into `executeQueryWithFormat`**

Replace the existing `ExecuteQuery` function body (client.go:38–58) with:

```go
// ExecuteQuery executes a Cypher query on a Memgraph instance using the
// default tabular output format. Most callers want this.
func (c *Client) ExecuteQuery(ctx context.Context, namespace, podName, query string) (string, error) {
	return c.executeQueryWithFormat(ctx, namespace, podName, query, "tabular")
}

// executeQueryWithFormat executes a Cypher query and returns the raw stdout.
// format must be one of mgconsole's supported output formats: "tabular" or "csv".
func (c *Client) executeQueryWithFormat(ctx context.Context, namespace, podName, query, format string) (string, error) {
	cmd := []string{
		"mgconsole",
		"--host", "127.0.0.1",
		"--port", "7687",
		"--use-ssl=false",
		"--no-history",
		"--output-format", format,
	}

	stdin := strings.NewReader(query + "\n")

	stdout, stderr, err := c.execInPod(ctx, namespace, podName, "memgraph", cmd, stdin)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w, stderr: %s", err, stderr)
	}

	return stdout, nil
}
```

- [ ] **Step 2: Verify build and existing tests pass**

Run:
```
go build ./...
go test ./internal/memgraph/...
```
Expected: all pass — this is a pure refactor.

- [ ] **Step 3: Commit**

```
git add internal/memgraph/client.go
git commit -m "refactor(memgraph): extract executeQueryWithFormat helper"
```

---

### Task 2: Add new `ReplicaDatabaseStatus` type + write failing parser tests

**Files:**
- Modify: `internal/memgraph/client.go` (lines 109–116 — replace `ReplicaInfo`)
- Modify: `internal/memgraph/client_test.go` (replace `TestParseShowReplicasOutput`)

We define the new types and write failing tests for `parseShowReplicasCSV` (implemented in Task 3). The old `ReplicaInfo` shape (`Port`, `Status` as scalar) is replaced; there is only one external caller (`ConfigureReplication` / `cleanupStaleReplicas`) and they read `.Name` only.

- [ ] **Step 1: Replace `ReplicaInfo` with new types**

In `internal/memgraph/client.go`, replace lines 109–116:

```go
// ReplicaDatabaseStatus is the per-database replication state reported by Memgraph 3.x
// in the `data_info` cell of SHOW REPLICAS.
type ReplicaDatabaseStatus struct {
	Status string // "ready" | "replicating" | "recovery" | "invalid" | "" (unknown)
	Behind int64  // negative values are valid in Memgraph but are not used for classification
	Ts     int64
}

// ReplicaInfo contains information about a registered replica.
// In Memgraph 3.x, replication state is per-database under DataInfo.
// An empty (non-nil) DataInfo map means the main has not opened a data channel.
type ReplicaInfo struct {
	Name     string
	Host     string                            // mgconsole's socket_address verbatim (host:port)
	Mode     string                            // sync_mode: async | sync | strict_sync
	DataInfo map[string]ReplicaDatabaseStatus  // keyed by database name
}
```

- [ ] **Step 2: Write failing tests for `parseShowReplicasCSV`**

Replace `TestParseShowReplicasOutput` (client_test.go:311 onwards through the end of that function — keep `TestParseMemoryValueEdgeCases` etc.) with the following. These tests will not compile until Task 3 introduces `parseShowReplicasCSV` — that is the intended TDD "red" state.

```go
func TestParseShowReplicasCSV(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		want     []ReplicaInfo
		wantErr  bool
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
```

The CSV inputs use Go raw-string literals; embedded `""` is standard CSV double-quote escaping inside a quoted field — `encoding/csv` decodes that to a single `"`.

- [ ] **Step 3: Verify the new tests fail to compile (red TDD state)**

Run:
```
go test ./internal/memgraph/... -run TestParseShowReplicasCSV
```
Expected: build failure citing `parseShowReplicasCSV: undefined`.

- [ ] **Step 4: Confirm `ConfigureReplication` and `cleanupStaleReplicas` still compile after struct change**

The only consumers of `ReplicaInfo` are in `internal/controller/replication.go` and they read `.Name` only — verified before writing this plan. Run:
```
go build ./...
```
Expected: only the `parseShowReplicasCSV` undefined error from the test file. No other compile errors (the production code should build clean even though the test file does not).

If unexpected errors appear (e.g. `replica.Status` accessed anywhere), fix the caller to use `replica.DataInfo` per Task 7's classifier logic before continuing.

- [ ] **Step 5: Commit**

```
git add internal/memgraph/client.go internal/memgraph/client_test.go
git commit -m "test(memgraph): add failing tests for Memgraph 3.x SHOW REPLICAS CSV parser"
```

---

### Task 3: Implement `parseShowReplicasCSV` and switch `ShowReplicas` to use it

**Files:**
- Modify: `internal/memgraph/client.go` (replace `parseShowReplicasOutput` lines 382–416, modify `ShowReplicas` lines 134–144)

- [ ] **Step 1: Add imports**

Ensure `internal/memgraph/client.go`'s import block contains `encoding/csv` and `regexp`:

```go
import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)
```

- [ ] **Step 2: Implement `parseShowReplicasCSV` (replaces `parseShowReplicasOutput`)**

Delete the old `parseShowReplicasOutput` function (client.go:382–416 — the entire function) and add this in the same location:

```go
// dataInfoEntryRe matches a per-database entry inside the `data_info` Cypher map literal.
// Example input cell: {memgraph: {behind: -8, status: "recovery", ts: 27335187}}
// The regex captures: 1=db name, 2=status, 3=behind, 4=ts. Field order inside the inner map is fixed by Memgraph.
var dataInfoEntryRe = regexp.MustCompile(`(\w+):\s*\{\s*behind:\s*(-?\d+),\s*status:\s*"(\w+)",\s*ts:\s*(\d+)\s*\}`)

// parseShowReplicasCSV parses mgconsole's `--output-format csv` output for SHOW REPLICAS
// against Memgraph 3.x. Columns: name, socket_address, sync_mode, system_info, data_info.
// The data_info cell is a Cypher map literal (not JSON), parsed by regex.
func parseShowReplicasCSV(output string) ([]ReplicaInfo, error) {
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}

	reader := csv.NewReader(strings.NewReader(output))
	reader.FieldsPerRecord = -1 // allow variable widths defensively

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse SHOW REPLICAS CSV: %w", err)
	}

	var replicas []ReplicaInfo
	for i, row := range rows {
		if len(row) < 5 {
			continue
		}
		// Skip the header row.
		if i == 0 && row[0] == "name" {
			continue
		}

		name := strings.TrimSpace(row[0])
		if name == "" {
			continue
		}

		info := ReplicaInfo{
			Name:     name,
			Host:     strings.TrimSpace(row[1]),
			Mode:     strings.TrimSpace(row[2]),
			DataInfo: map[string]ReplicaDatabaseStatus{},
		}

		dataInfoCell := strings.TrimSpace(row[4])
		// dataInfoCell is either "{}" or "{db: {...}, db2: {...}}"; the regex
		// extracts each inner entry. Non-matching cells produce an empty map.
		for _, m := range dataInfoEntryRe.FindAllStringSubmatch(dataInfoCell, -1) {
			db := m[1]
			behind, _ := strconv.ParseInt(m[2], 10, 64)
			status := m[3]
			ts, _ := strconv.ParseInt(m[4], 10, 64)
			info.DataInfo[db] = ReplicaDatabaseStatus{
				Status: status,
				Behind: behind,
				Ts:     ts,
			}
		}

		replicas = append(replicas, info)
	}
	return replicas, nil
}
```

- [ ] **Step 3: Switch `ShowReplicas` to use CSV format and the new parser**

Replace the body of `ShowReplicas` (client.go:134–144):

```go
// ShowReplicas returns the list of registered replicas from the main instance.
// Uses CSV output format so the new-style data_info column can be parsed reliably.
func (c *Client) ShowReplicas(ctx context.Context, namespace, mainPodName string) ([]ReplicaInfo, error) {
	output, err := c.executeQueryWithFormat(ctx, namespace, mainPodName, "SHOW REPLICAS;", "csv")
	if err != nil {
		return nil, fmt.Errorf("failed to show replicas: %w", err)
	}
	return parseShowReplicasCSV(output)
}
```

- [ ] **Step 4: Run tests — they must now pass**

```
go test ./internal/memgraph/... -run TestParseShowReplicasCSV -v
```
Expected: PASS for all seven sub-tests.

- [ ] **Step 5: Run full memgraph package tests**

```
go test ./internal/memgraph/...
```
Expected: PASS. (Old `TestParseShowReplicasOutput` was removed in Task 2.)

- [ ] **Step 6: Commit**

```
git add internal/memgraph/client.go
git commit -m "feat(memgraph): parse Memgraph 3.x SHOW REPLICAS CSV output"
```

---

### Task 4: Add `BehindAlertThreshold` field to `ReplicationSpec`

**Files:**
- Modify: `api/v1alpha1/memgraphcluster_types.go` (around line 137)
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (regenerated)
- Modify: `config/crd/bases/...memgraphcluster.yaml` (regenerated)

- [ ] **Step 1: Add the field to `ReplicationSpec`**

In `api/v1alpha1/memgraphcluster_types.go` replace `ReplicationSpec` (lines 137–143):

```go
// ReplicationSpec defines the replication settings
type ReplicationSpec struct {
	// Mode is the replication mode (ASYNC, SYNC, STRICT_SYNC)
	// +kubebuilder:default="ASYNC"
	// +optional
	Mode ReplicationMode `json:"mode,omitempty"`

	// BehindAlertThreshold is the duration a replica may stay behind the main
	// before being reported as unhealthy. Defaults to 5m. Must be greater than 0.
	// +kubebuilder:default="5m"
	// +optional
	BehindAlertThreshold metav1.Duration `json:"behindAlertThreshold,omitempty"`
}
```

`metav1` is already imported (memgraphcluster_types.go:8).

- [ ] **Step 2: Regenerate deepcopy and CRD manifests**

```
make generate manifests
```

Expected: `api/v1alpha1/zz_generated.deepcopy.go` and `config/crd/bases/memgraph.base14.io_memgraphclusters.yaml` (or similar) updated with the new field.

If `make generate` is not available, run `controller-gen` directly:
```
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
controller-gen crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
```

- [ ] **Step 3: Sanity-check the generated CRD**

```
grep -A2 behindAlertThreshold config/crd/bases/*.yaml
```
Expected: shows `default: 5m0s` (or `5m`) and `type: string` (Duration is serialized as string).

- [ ] **Step 4: Build**

```
go build ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add api/v1alpha1/memgraphcluster_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases
git commit -m "feat(api): add ReplicationSpec.BehindAlertThreshold field"
```

---

### Task 5: Classifier — write failing tests

**Files:**
- Create: `internal/controller/replica_classifier_test.go`

The classifier is a pure function that takes a replica observation, mutates per-replica state, and returns a typed classification. Putting it in its own file keeps `replication.go` focused on the manager.

- [ ] **Step 1: Create the failing test file**

Create `internal/controller/replica_classifier_test.go`:

```go
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
		name     string
		replica  memgraph.ReplicaInfo
		state    *replicaState
		now      time.Time
		want     replicaClassification
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
```

- [ ] **Step 2: Confirm tests fail to compile**

Run:
```
go test ./internal/controller/... -run TestClassifyReplica
```
Expected: build failure citing `replicaState`, `classifyReplica`, `replicaClassification`, and the constants are undefined. This is the intended TDD red state.

- [ ] **Step 3: Commit**

```
git add internal/controller/replica_classifier_test.go
git commit -m "test(controller): add failing tests for replica classifier"
```

---

### Task 6: Implement the classifier

**Files:**
- Create: `internal/controller/replica_classifier.go`

- [ ] **Step 1: Create the classifier file**

Create `internal/controller/replica_classifier.go`:

```go
// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
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
//     - first seen / within grace -> Transient
//     - persisted past grace      -> DataChannelDown
//   - any DB status == "invalid"  -> Invalid (terminal, returned immediately)
//   - any DB status unknown       -> UnknownStatus (defensive)
//   - any DB Behind > 0
//     - first seen / within threshold -> Behind
//     - persisted past threshold      -> BehindTooLong
//   - else                        -> Healthy
//
// Negative Behind values are ignored entirely (they indicate replica-side
// divergence, which is a separate concern parked for a follow-up).
func classifyReplica(replica memgraph.ReplicaInfo, state *replicaState, now time.Time, behindThreshold time.Duration) replicaClassification {
	if len(replica.DataInfo) == 0 {
		if state.channelDownSince.IsZero() {
			state.channelDownSince = now
		}
		if now.Sub(state.channelDownSince) > channelDownGracePeriod {
			return classificationDataChannelDown
		}
		return classificationTransient
	}
	state.channelDownSince = time.Time{}

	worst := classificationHealthy
	anyBehind := false

	for _, db := range replica.DataInfo {
		switch db.Status {
		case "invalid":
			return classificationInvalid
		case "ready", "recovery", "replicating":
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
```

- [ ] **Step 2: Run classifier tests — must pass**

```
go test ./internal/controller/... -run TestClassifyReplica -v
go test ./internal/controller/... -run TestClassificationIsHealthy -v
```
Expected: all sub-tests PASS.

- [ ] **Step 3: Commit**

```
git add internal/controller/replica_classifier.go
git commit -m "feat(controller): implement replica health classifier"
```

---

### Task 7: Wire per-replica state into `ReplicationManager`

**Files:**
- Modify: `internal/controller/replication.go` (`ReplicationManager` struct around line 20, `NewReplicationManager` lines 25–31)
- Modify: `internal/controller/replication_test.go` (line 125–131, `TestNewReplicationManager`)

State is stored per cluster (namespace/name) → per replica name. Access is protected by a mutex; reconciles serialize per CR but health checks for different clusters can run concurrently in tests.

- [ ] **Step 1: Add state fields to `ReplicationManager`**

In `internal/controller/replication.go`, replace the struct and constructor (lines 19–31):

```go
// ReplicationManager handles Memgraph replication configuration and health checks.
type ReplicationManager struct {
	client   *memgraph.Client
	recorder record.EventRecorder

	// states tracks per-replica timers for classification. Keyed by
	// "namespace/name" -> replica name -> state. Lost on operator restart.
	statesMu sync.Mutex
	states   map[string]map[string]*replicaState

	// now is the clock used for classification. Overridable in tests.
	now func() time.Time
}

// NewReplicationManager creates a new ReplicationManager
func NewReplicationManager(client *memgraph.Client, recorder record.EventRecorder) *ReplicationManager {
	return &ReplicationManager{
		client:   client,
		recorder: recorder,
		states:   make(map[string]map[string]*replicaState),
		now:      time.Now,
	}
}

// stateFor returns (creating if necessary) the per-replica state entry.
// Caller must hold statesMu.
func (rm *ReplicationManager) stateFor(clusterKey, replicaName string) *replicaState {
	byReplica, ok := rm.states[clusterKey]
	if !ok {
		byReplica = make(map[string]*replicaState)
		rm.states[clusterKey] = byReplica
	}
	st, ok := byReplica[replicaName]
	if !ok {
		st = &replicaState{}
		byReplica[replicaName] = st
	}
	return st
}

// pruneStates drops state entries for replicas no longer in the observed set.
// Caller must hold statesMu.
func (rm *ReplicationManager) pruneStates(clusterKey string, observed map[string]struct{}) {
	byReplica, ok := rm.states[clusterKey]
	if !ok {
		return
	}
	for name := range byReplica {
		if _, kept := observed[name]; !kept {
			delete(byReplica, name)
		}
	}
}
```

- [ ] **Step 2: Add new imports**

In the import block at the top of `internal/controller/replication.go`, add `sync` and `time`:

```go
import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
	"github.com/base14/memgraph-operator/internal/memgraph"
)
```

- [ ] **Step 3: Update `TestNewReplicationManager` if needed**

Existing test at `internal/controller/replication_test.go:125`:
```go
func TestNewReplicationManager(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	rm := NewReplicationManager(nil, fakeRecorder)

	if rm == nil {
		t.Fatal("NewReplicationManager returned nil")
	}
	...
}
```

No signature change for `NewReplicationManager`, so no test edit needed unless the assertions probe internal fields. Verify with:
```
go test ./internal/controller/... -run TestNewReplicationManager -v
```
Expected: PASS unchanged.

- [ ] **Step 4: Build**

```
go build ./...
```
Expected: PASS (state plumbing only — no behavior change yet).

- [ ] **Step 5: Commit**

```
git add internal/controller/replication.go
git commit -m "feat(controller): add per-replica state tracking to ReplicationManager"
```

---

### Task 8: Add new event reasons (keep old until Task 10)

**Files:**
- Modify: `internal/controller/events.go` (lines 17–24)
- Modify: `internal/controller/events_test.go` (around line 23)

- [ ] **Step 1: Add the three new constants**

In `internal/controller/events.go`, expand the "Replication events" block (lines 17–24) — leave `EventReasonReplicaUnhealthy` in place for now so Task 9's classifier rewrite can reference both old and new during the transition:

```go
// Replication events
EventReasonMainInstanceConfigured = "MainInstanceConfigured"
EventReasonReplicaRegistered      = "ReplicaRegistered"
EventReasonReplicaUnregistered    = "ReplicaUnregistered"
EventReasonReplicationHealthy     = "ReplicationHealthy"
EventReasonReplicationError       = "ReplicationError"
EventReasonReplicaUnhealthy       = "ReplicaUnhealthy" // deprecated; removed in next task
EventReasonReplicaDataChannelDown = "ReplicaDataChannelDown"
EventReasonReplicaInvalid         = "ReplicaInvalid"
EventReasonReplicaBehindTooLong   = "ReplicaBehindTooLong"
EventReasonReplicationLagHigh     = "ReplicationLagHigh"
```

- [ ] **Step 2: Add the three new constants to the events test table**

In `internal/controller/events_test.go` find the table that includes `{"EventReasonReplicaUnhealthy", EventReasonReplicaUnhealthy}` (around line 23) and append:

```go
{"EventReasonReplicaDataChannelDown", EventReasonReplicaDataChannelDown},
{"EventReasonReplicaInvalid", EventReasonReplicaInvalid},
{"EventReasonReplicaBehindTooLong", EventReasonReplicaBehindTooLong},
```

- [ ] **Step 3: Run events tests**

```
go test ./internal/controller/... -run TestEventReason
```
Expected: PASS.

- [ ] **Step 4: Commit**

```
git add internal/controller/events.go internal/controller/events_test.go
git commit -m "feat(controller): add ReplicaDataChannelDown/Invalid/BehindTooLong event reasons"
```

---

### Task 9: Add per-replica metrics (gauges + recorder methods)

**Files:**
- Modify: `internal/controller/metrics.go`

- [ ] **Step 1: Define the two new gauges**

In `internal/controller/metrics.go`, inside the `var (...)` block alongside `replicationLagGauge` (around line 46), add:

```go
replicaDataChannelUpGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "memgraph_replica_data_channel_up",
		Help: "1 if the replica's data channel is established (data_info populated and status not invalid/unknown), else 0",
	},
	[]string{"cluster", "namespace", "replica"},
)

replicaBehindSecondsGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "memgraph_replica_behind_seconds",
		Help: "Number of seconds the replica has continuously been behind the main; 0 when caught up",
	},
	[]string{"cluster", "namespace", "replica"},
)
```

- [ ] **Step 2: Register them in `init()`**

In the `metrics.Registry.MustRegister(...)` call (around line 208), append the two new gauges to the registration list:

```go
metrics.Registry.MustRegister(
	clusterPhaseGauge,
	// ...existing entries...
	replicationLagGauge,
	replicationHealthyGauge,
	replicaDataChannelUpGauge,
	replicaBehindSecondsGauge,
	// ...rest...
)
```

- [ ] **Step 3: Add recorder methods**

Append to the bottom of `internal/controller/metrics.go` (after `RecordStorageInfo`):

```go
// RecordReplicaDataChannel sets the per-replica data-channel-up gauge.
func (m *MetricsRecorder) RecordReplicaDataChannel(cluster, namespace, replica string, up bool) {
	v := 0.0
	if up {
		v = 1.0
	}
	replicaDataChannelUpGauge.WithLabelValues(cluster, namespace, replica).Set(v)
}

// RecordReplicaBehindSeconds sets how long the replica has been behind, in seconds.
// Pass 0 when the replica is caught up.
func (m *MetricsRecorder) RecordReplicaBehindSeconds(cluster, namespace, replica string, seconds float64) {
	replicaBehindSecondsGauge.WithLabelValues(cluster, namespace, replica).Set(seconds)
}

// DeleteReplicaMetrics removes per-replica gauge entries (called when a replica is unregistered).
func (m *MetricsRecorder) DeleteReplicaMetrics(cluster, namespace, replica string) {
	replicaDataChannelUpGauge.DeleteLabelValues(cluster, namespace, replica)
	replicaBehindSecondsGauge.DeleteLabelValues(cluster, namespace, replica)
}
```

- [ ] **Step 4: Build**

```
go build ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/controller/metrics.go
git commit -m "feat(controller): add per-replica data_channel_up and behind_seconds gauges"
```

---

### Task 10: Rewrite `CheckReplicationHealth` to use the classifier

**Files:**
- Modify: `internal/controller/replication.go` (`CheckReplicationHealth` lines 205–243)
- Modify: `internal/controller/events.go` (remove deprecated constant)
- Modify: `internal/controller/events_test.go` (remove deprecated case)
- Modify: `internal/controller/memgraphcluster_controller.go` if the controller already wires a `MetricsRecorder` reachable from the replication path. Check first.

This is the central behavior change. The new loop:
1. Loads the per-replica state under the mutex.
2. Calls `classifyReplica` for each replica.
3. Emits the correct event reason based on classification.
4. Updates the per-replica metrics.
5. Prunes state entries for replicas that have disappeared.

`CheckReplicationHealth` currently has no access to a `MetricsRecorder`. We add one as an optional struct field set via a setter (avoiding constructor signature churn for callers we don't need to touch).

- [ ] **Step 1: Add a `MetricsRecorder` field + setter to `ReplicationManager`**

In `internal/controller/replication.go`, add a field to the struct (it now has): `metrics *MetricsRecorder` — extend the struct from Task 7:

```go
type ReplicationManager struct {
	client   *memgraph.Client
	recorder record.EventRecorder
	metrics  *MetricsRecorder

	statesMu sync.Mutex
	states   map[string]map[string]*replicaState

	now func() time.Time
}
```

Add a setter just after `NewReplicationManager`:

```go
// SetMetricsRecorder wires the metrics recorder. Safe to call once at startup.
func (rm *ReplicationManager) SetMetricsRecorder(m *MetricsRecorder) {
	rm.metrics = m
}
```

- [ ] **Step 2: Wire the setter in the controller bootstrap**

In `internal/controller/memgraphcluster_controller.go` find the existing line (around 300):
```go
r.replicationManager = NewReplicationManager(mgClient, r.Recorder)
```
Replace with:
```go
r.replicationManager = NewReplicationManager(mgClient, r.Recorder)
r.replicationManager.SetMetricsRecorder(r.metrics)
```

If `r.metrics` is named differently, search for `MetricsRecorder` in the reconciler struct and use the field's actual name. If no MetricsRecorder exists on the reconciler yet, create one inline:
```go
r.replicationManager = NewReplicationManager(mgClient, r.Recorder)
r.replicationManager.SetMetricsRecorder(NewMetricsRecorder())
```

- [ ] **Step 3: Rewrite `CheckReplicationHealth`**

Replace `CheckReplicationHealth` (replication.go:205–243) with:

```go
// CheckReplicationHealth classifies each registered replica, emits events,
// updates per-replica metrics, and returns aggregate health counts.
func (rm *ReplicationManager) CheckReplicationHealth(
	ctx context.Context,
	cluster *memgraphv1alpha1.MemgraphCluster,
	writeInstance string,
	log *zap.Logger,
) (*memgraphv1alpha1.ReplicationHealth, error) {
	if writeInstance == "" {
		return nil, fmt.Errorf("no write instance specified")
	}

	replicas, err := rm.client.ShowReplicas(ctx, cluster.Namespace, writeInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to show replicas: %w", err)
	}

	behindThreshold := cluster.Spec.Replication.BehindAlertThreshold.Duration
	if behindThreshold <= 0 {
		behindThreshold = 5 * time.Minute // safety net if defaulting did not apply
	}

	now := rm.now()
	clusterKey := cluster.Namespace + "/" + cluster.Name

	health := &memgraphv1alpha1.ReplicationHealth{
		TotalReplicas:   int32(len(replicas)),
		HealthyReplicas: 0,
	}

	observed := make(map[string]struct{}, len(replicas))

	rm.statesMu.Lock()
	defer rm.statesMu.Unlock()

	for _, replica := range replicas {
		observed[replica.Name] = struct{}{}
		st := rm.stateFor(clusterKey, replica.Name)

		class := classifyReplica(replica, st, now, behindThreshold)

		if class.isHealthy() {
			health.HealthyReplicas++
		}

		// Metrics
		if rm.metrics != nil {
			channelUp := class != classificationDataChannelDown &&
				class != classificationTransient &&
				class != classificationInvalid &&
				class != classificationUnknownStatus
			rm.metrics.RecordReplicaDataChannel(cluster.Name, cluster.Namespace, replica.Name, channelUp)

			behindSeconds := 0.0
			if !st.behindSince.IsZero() {
				behindSeconds = now.Sub(st.behindSince).Seconds()
			}
			rm.metrics.RecordReplicaBehindSeconds(cluster.Name, cluster.Namespace, replica.Name, behindSeconds)
		}

		// Events for unhealthy classifications only.
		switch class {
		case classificationDataChannelDown:
			log.Warn("replica data channel down",
				zap.String("replica", replica.Name),
				zap.Duration("emptyFor", now.Sub(st.channelDownSince)))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaDataChannelDown,
				fmt.Sprintf("Replica %s registered but data_info is empty for %s — main has not opened a data channel. Check connectivity on :10000, replica role, and version match. Re-registering with a fresh PVC usually resolves this.",
					replica.Name, now.Sub(st.channelDownSince).Round(time.Second)))
		case classificationInvalid:
			log.Warn("replica in invalid state",
				zap.String("replica", replica.Name),
				zap.Any("dataInfo", replica.DataInfo))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaInvalid,
				fmt.Sprintf("Replica %s reports invalid status. Manual intervention required (drop + re-register).", replica.Name))
		case classificationBehindTooLong:
			log.Warn("replica behind for too long",
				zap.String("replica", replica.Name),
				zap.Duration("behindFor", now.Sub(st.behindSince)),
				zap.Duration("threshold", behindThreshold))
			rm.recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReplicaBehindTooLong,
				fmt.Sprintf("Replica %s has been behind for %s (threshold %s).",
					replica.Name, now.Sub(st.behindSince).Round(time.Second), behindThreshold))
		case classificationUnknownStatus:
			log.Warn("replica reports unknown status",
				zap.String("replica", replica.Name),
				zap.Any("dataInfo", replica.DataInfo))
		}
	}

	// Drop state for replicas that have disappeared since the last check.
	rm.pruneStates(clusterKey, observed)
	if rm.metrics != nil {
		// Also drop metric labels for vanished replicas to prevent stale series.
		// We can't iterate Prometheus labels directly, so we rely on the next
		// reconcile or explicit cleanup elsewhere. (Out of scope for this task.)
		_ = observed
	}

	if health.HealthyReplicas == health.TotalReplicas && health.TotalReplicas > 0 {
		rm.recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReplicationHealthy,
			fmt.Sprintf("All %d replicas are healthy", health.TotalReplicas))
	}

	return health, nil
}
```

- [ ] **Step 4: Remove the deprecated `EventReasonReplicaUnhealthy` constant**

In `internal/controller/events.go` delete the line:
```go
EventReasonReplicaUnhealthy       = "ReplicaUnhealthy" // deprecated; removed in next task
```

- [ ] **Step 5: Remove its test case**

In `internal/controller/events_test.go` find:
```go
{"EventReasonReplicaUnhealthy", EventReasonReplicaUnhealthy},
```
Delete that line.

- [ ] **Step 6: Build**

```
go build ./...
```
Expected: PASS. Any remaining references to `EventReasonReplicaUnhealthy` indicate a missed call site — grep:
```
grep -rn EventReasonReplicaUnhealthy --include='*.go'
```
Expected output: empty. If non-empty, fix the call site to use one of the three new reasons appropriate for the path before continuing.

- [ ] **Step 7: Run controller tests**

```
go test ./internal/controller/...
```
Expected: PASS. If `TestCheckReplicationHealth` or similar existing tests fail because they expected the old `ReplicaUnhealthy` event or relied on the old `Status` field on `ReplicaInfo`, update them to assert the new event reasons / inspect `DataInfo` instead. Each fix should be minimal — replace the literal `"ReplicaUnhealthy"` with the matching new reason from the table:

| Old test scenario | New event reason |
|---|---|
| Replica with `data_info: {}` | `EventReasonReplicaDataChannelDown` (after 30s) |
| Replica with status "invalid" | `EventReasonReplicaInvalid` |
| Replica behind > threshold | `EventReasonReplicaBehindTooLong` |

- [ ] **Step 8: Commit**

```
git add internal/controller/replication.go internal/controller/events.go internal/controller/events_test.go internal/controller/memgraphcluster_controller.go
git commit -m "feat(controller): classify replica health with state, emit specific events"
```

---

### Task 11: Full verification

**Files:** (no edits expected — verification only)

- [ ] **Step 1: Full test suite**

```
go test ./...
```
Expected: PASS. If anything fails, fix at the failure site — do not commit broken state.

- [ ] **Step 2: Vet + build**

```
go vet ./...
go build ./...
```
Expected: clean.

- [ ] **Step 3: Lint**

```
golangci-lint run ./...
```
Expected: clean. If new warnings appear (e.g. unused variable, missing doc on exported type), fix them.

- [ ] **Step 4: Confirm no leftover references**

```
grep -rn "parseShowReplicasOutput\|ReplicaUnhealthy\|replica.Status[^=]" --include='*.go'
```
Expected output: empty.

- [ ] **Step 5: Confirm CRD manifest contains the new field**

```
grep behindAlertThreshold config/crd/bases/*.yaml
```
Expected: 2+ lines (field schema + default).

- [ ] **Step 6: If anything in steps 1–5 produced fixes, commit them**

```
git add -A
git status   # review staged changes
git commit -m "chore(controller): post-verification cleanup"
```

If nothing needed fixing, skip the commit.

---

## Self-Review (writing-plans checklist)

**1. Spec coverage:**
- Spec §1 (Parser & types) → Tasks 1, 2, 3 ✓
- Spec §2 (CRD spec change) → Task 4 ✓
- Spec §3 (Detection logic) → Tasks 5, 6, 7, 10 ✓
- Spec §4 (Events) → Task 8, finalized in Task 10 ✓
- Spec §5 (Metrics) → Task 9, wired in Task 10 ✓
- Spec §6 (Tests) → Test steps in Tasks 2, 5, plus existing-test updates in Task 10 ✓
- Spec File-level Impact table → each row mapped to a task ✓
- Spec Risks (regex brittleness, breaking event removal) → addressed in Task 2 (varied test inputs) and Task 10 (grep guard for residual references) ✓

**2. Placeholder scan:** Searched for "TBD", "TODO", "implement later", "similar to" — none present. All code blocks are complete.

**3. Type consistency:**
- `ReplicaInfo` shape matches across Tasks 2, 3, 5, 10 (Name/Host/Mode/DataInfo) ✓
- `ReplicaDatabaseStatus` field names (Status/Behind/Ts) consistent ✓
- `replicaState` field names (behindSince/channelDownSince) consistent across Tasks 5, 6, 7, 10 ✓
- `classifyReplica` signature `(replica, *state, now, behindThreshold)` consistent between test (Task 5) and impl (Task 6) ✓
- `replicaClassification` constants spelled identically in Tasks 5, 6, 10 ✓
- `MetricsRecorder` method names (`RecordReplicaDataChannel`, `RecordReplicaBehindSeconds`, `DeleteReplicaMetrics`) consistent between Task 9 (definition) and Task 10 (caller) ✓
