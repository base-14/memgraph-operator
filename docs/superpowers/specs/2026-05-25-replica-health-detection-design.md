# Replica Health Detection — Design

**Status:** Draft (awaiting user review)
**Date:** 2026-05-25
**Scope:** Fix the operator's `SHOW REPLICAS` parser for Memgraph 3.x and add specific detection for "registered but data channel never opened", `invalid`, and "behind too long" states.

## Background

The operator's replica health check (`ReplicationManager.CheckReplicationHealth` in `internal/controller/replication.go`) treats any replica whose `Status` field is not `"ready"` or `"replicating"` as unhealthy. Two real-world breakages surfaced after the Memgraph 2.21 → 3.7 upgrade:

1. **Parser broken.** `SHOW REPLICAS` in Memgraph 3.x returns columns `name | socket_address | sync_mode | system_info | data_info`. The current parser (`parseShowReplicasOutput` in `internal/memgraph/client.go`) was written for the 2.x layout where column 5 was a scalar `state`. It now blindly stuffs the Cypher-map-literal `data_info` blob into `ReplicaInfo.Status`. The status equality check then fails on strings like `{memgraph: {behind: -25, status: "recovery", ts: 27337126}}`, producing noisy and useless `ReplicaUnhealthy` events.
2. **No detection for "data channel down".** A replica can be registered (returned by `SHOW REPLICAS`) and reachable yet have `data_info: {}`, meaning the main never opened a per-database replication channel. Today this is indistinguishable from any other failure and is misreported as a string mismatch.

Additionally, the existing classifier doesn't recognize `"recovery"` as a valid in-flight state (it is) and has no notion of "behind for a long time".

## Goals

1. Parse Memgraph 3.x `SHOW REPLICAS` output correctly.
2. Detect and distinctly surface three failure modes:
   - **DataChannelDown** — `data_info` empty for > 30s after first observation.
   - **Invalid** — `data_info.<db>.status == "invalid"`.
   - **BehindTooLong** — `behind > 0` sustained for longer than a configurable threshold (default 5 minutes).
3. Treat `ready`, `recovery`, and `replicating` as healthy (statuses flip rapidly during normal operation).
4. Expose detection via distinct event reasons and Prometheus metrics so SREs can alert deterministically.

## Non-Goals

- Backwards compatibility with Memgraph 2.x output (the 2.21→3.7 upgrade guide already commits the project to 3.x).
- Divergence detection from negative `behind` values (parked for a follow-up; logged at debug only).
- Auto-remediation (e.g. drop + re-register on failure). Out of scope.
- Replacing the `kubectl exec mgconsole` shell-out with a Bolt driver. Out of scope.
- Per-replica detail in `MemgraphCluster.status` CR schema. Detection state is in-memory.

## Design

### 1. Parser & types (`internal/memgraph/`)

`ExecuteQuery` currently hard-codes `--output-format tabular`. We extract the shared body into an unexported `executeQueryWithFormat(ctx, ns, pod, query, format)` helper. `ExecuteQuery` keeps its public signature and calls the helper with `"tabular"`. `ShowReplicas` calls the helper directly with `"csv"`. All other callers — `SetReplicationRole`, `RegisterReplica`, `UnregisterReplica`, `GetReplicationRole`, `ShowStorageInfo` — go through `ExecuteQuery` and stay on tabular. This contains blast radius to the one parser that's actually broken.

New types in `client.go`:

```go
type ReplicaDatabaseStatus struct {
    Status string  // "ready" | "replicating" | "recovery" | "invalid" | (unknown)
    Behind int64   // negative values are logged but not used for classification
    Ts     int64
}

type ReplicaInfo struct {
    Name     string
    Host     string                            // socket_address as returned
    Mode     string                            // sync_mode (async/sync/strict_sync)
    DataInfo map[string]ReplicaDatabaseStatus  // keyed by database name; empty == channel down
}
```

The old aggregate `Status` field is removed — callers must use `DataInfo`. There is a single caller (`CheckReplicationHealth`), so this is a clean change.

`parseShowReplicasCSV(output string) ([]ReplicaInfo, error)`:

1. `encoding/csv` reads the outer rows.
2. For each row, the `data_info` cell is a Cypher map literal like `{memgraph: {behind: -25, status: "recovery", ts: 27337126}}` or `{}`.
3. A small regex extracts per-database entries:
   ```
   (\w+):\s*\{[^}]*status:\s*"(\w+)"[^}]*behind:\s*(-?\d+)[^}]*ts:\s*(\d+)
   ```
4. Empty `data_info` → `DataInfo` is a non-nil empty map (preserves the distinction from "parse failed").
5. Parse errors return the error to the caller; the caller logs and treats the replica as "unknown state" (unhealthy).

### 2. CRD spec change (`api/v1alpha1/memgraphcluster_types.go`)

Add one field to `ReplicationSpec`:

```go
type ReplicationSpec struct {
    Mode ReplicationMode `json:"mode,omitempty"`

    // BehindAlertThreshold is the duration a replica may stay behind the main
    // before being reported as unhealthy. Default 5m. Must be > 0.
    // +kubebuilder:default="5m"
    // +optional
    BehindAlertThreshold metav1.Duration `json:"behindAlertThreshold,omitempty"`
}
```

Regenerate CRDs and deepcopy. No status-schema changes.

### 3. Detection logic (`internal/controller/replication.go`)

`ReplicationManager` gains in-memory per-replica state:

```go
type replicaState struct {
    behindSince      time.Time // zero == not currently behind
    channelDownSince time.Time // zero == data_info populated
}

type ReplicationManager struct {
    // ...existing fields...
    states map[string]map[string]*replicaState // cluster-key → replica-name → state
    mu     sync.Mutex
}
```

State is lost on operator restart — acceptable, alerts re-arm within the configured threshold. Cluster-key is `namespace/name`. Entries for deleted clusters/replicas are pruned during health checks.

Classifier (called once per replica per health check):

```
classify(replica, state, now, behindThreshold):
    if len(replica.DataInfo) == 0:
        if state.channelDownSince.IsZero(): state.channelDownSince = now
        if now.Sub(state.channelDownSince) > 30s: return DataChannelDown
        return Transient
    state.channelDownSince = time.Time{}

    worst := Healthy
    for _, db := range replica.DataInfo:
        switch db.Status {
        case "invalid":
            return Invalid   // terminal — return immediately
        case "ready", "recovery", "replicating":
            // ok
        default:
            worst = UnknownStatus   // defensive
        }
        if db.Behind > 0:
            if state.behindSince.IsZero(): state.behindSince = now
            if now.Sub(state.behindSince) > behindThreshold:
                return BehindTooLong
            if worst == Healthy: worst = Behind
        else:
            state.behindSince = time.Time{}   // any DB caught up clears timer
    return worst
```

The returned classification drives both event emission and metric updates from a single source of truth.

`HealthyReplicas` accounting: `Healthy`, `Behind`, and `Transient` count as healthy. `DataChannelDown`, `Invalid`, `UnknownStatus`, `BehindTooLong` count as unhealthy.

Negative `behind` is logged at debug and otherwise ignored.

### 4. Events (`internal/controller/events.go`)

Remove `EventReasonReplicaUnhealthy`. Add:

```go
EventReasonReplicaDataChannelDown = "ReplicaDataChannelDown"
EventReasonReplicaInvalid         = "ReplicaInvalid"
EventReasonReplicaBehindTooLong   = "ReplicaBehindTooLong"
```

All emitted as `corev1.EventTypeWarning`. Messages include the replica name, the relevant status/behind/duration, and a one-line remediation hint (e.g. "re-register with a fresh PVC" for DataChannelDown).

No event is emitted for `Behind` (warning state) — the metric carries that signal. Avoids event-spam during normal lag.

### 5. Metrics (`internal/controller/metrics.go`)

Two new gauges with `{cluster, namespace, replica}` labels (matching the existing `replicationLagGauge` pattern):

```go
replicaDataChannelUpGauge   // 1 if data_info populated and classification is not Invalid/Unknown; else 0
replicaBehindSecondsGauge   // seconds since behindSince; 0 when caught up
```

Both registered in `init()` and cleaned up in `CleanupClusterMetrics` (mirroring `snapshotLastSuccessTimestamp` at metrics.go:360).

### 6. Tests

- **`internal/memgraph/client_test.go`** — table-driven tests for `parseShowReplicasCSV` covering: empty `data_info`, single-DB `ready`, `recovery` with negative behind, `invalid`, multiple DBs, malformed cell. TDD: write failing tests first.
- **`internal/controller/replication_test.go`** — tests for the classifier with a synthetic clock (`now func() time.Time`) covering each state transition, the 30s and threshold boundaries, and `HealthyReplicas` math.
- **CRD validation** — kubebuilder validation on `BehindAlertThreshold > 0`; tested via `api/v1alpha1/memgraphcluster_types_test.go`.
- E2E left unchanged — covered indirectly by existing replication E2E tests.

## File-level Impact

| File | Change |
|---|---|
| `internal/memgraph/client.go` | Add `ExecuteQueryCSV` (or `format` param). New types `ReplicaDatabaseStatus`, updated `ReplicaInfo`. Replace `parseShowReplicasOutput` with `parseShowReplicasCSV`. |
| `internal/memgraph/client_test.go` | New parser tests. |
| `api/v1alpha1/memgraphcluster_types.go` | Add `BehindAlertThreshold metav1.Duration` to `ReplicationSpec`. |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerated. |
| `config/crd/bases/...memgraphcluster.yaml` | Regenerated. |
| `internal/controller/replication.go` | Per-replica state map, classifier, event/metric emission, prune logic. |
| `internal/controller/replication_test.go` | Classifier tests with injected clock. |
| `internal/controller/events.go` | Three new event-reason constants; remove `ReplicaUnhealthy`. |
| `internal/controller/metrics.go` | Two new gauges + cleanup. |

## Risks & Mitigations

- **Cypher-map regex brittleness.** Mitigated by table-driven tests covering all observed shapes, and by isolating the regex to the `data_info` cell only.
- **In-memory state lost on restart.** Accepted: alerts re-arm within the configured threshold. Documented in code.
- **Behavior change for existing alerts.** Removing `ReplicaUnhealthy` is a breaking change for any Prometheus alert that matches that reason string. Call out in release notes.
- **CSV output format differences across mgconsole versions.** Mitigated by pinning to a Memgraph 3.x baseline (already done per the upgrade guide); parser fails closed (parse error → unhealthy) rather than silently misinterpreting.
