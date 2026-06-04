# Memgraph Operator - Required Fixes

## Background

During debugging of KB pod crashes (2025-12-05), we identified several issues in the memgraph-operator that cause high CPU usage on Memgraph MAIN instances and incorrect health reporting.

**Update (2026-06-04):** While debugging intermittent empty query results in a production cluster, we traced a silent, 104-day replication outage to three additional operator defects (Issues 5–7 below). The affected replica was registered and heartbeating but had **zero data** (`data_info: {}`), and the `-read` service kept serving it to clients. This incident also showed that Issue 1's premise is partially wrong — see the correction note under Issue 1.

## Issue Summary

1. **SHOW REPLICAS output format changed in Memgraph v2.21.0** - Parser is incompatible
2. **Status updates trigger immediate re-reconciliation** - Backoff is ineffective
3. **Multiple queries per reconciliation** - Overwhelms Memgraph under frequent reconciliation
4. **Validation test writes to database** - Invasive health checks
5. **`clear-replication-state` init container deletes `epoch_id`** - Breaks replica lineage on every restart (root cause of the production outage)
6. **No remediation for invalid/diverged replicas** - Operator logs warnings forever, never re-seeds
7. **`-read` service selects ALL pods regardless of replication health** - Broken replicas serve client traffic

---

## Issue 1: SHOW REPLICAS Parser Incompatibility (HIGH PRIORITY)

### Problem

The `parseShowReplicasOutput` function in `internal/memgraph/client.go:334-368` expects the old Memgraph output format:

```
| name | host | port | mode | status |
```

But **Memgraph v2.21.0** returns a different format:

```
| name | socket_address | sync_mode | system_info | data_info |
```

The parser reads `parts[5]` (0-indexed) as the status field, but that's now `data_info` which contains `{}` (empty JSON object). This doesn't match "ready" or "replicating", so all replicas are incorrectly marked as **unhealthy**.

### Evidence

```
Actual output:
| "oteldemo1_scout_kb_graph_0" | "oteldemo1-scout-kb-graph-0...10000" | "async" | Null | {} |

Operator log:
{"level":"warn","msg":"unhealthy replica detected","replica":"oteldemo1_scout_kb_graph_0","status":"{}"}
```

### Impact

- All replicas are reported as unhealthy even when replication is working correctly
- Triggers frequent reconciliation attempts
- Emits spurious warning events

### ⚠️ Correction (2026-06-04, production incident)

The premise "replicas are incorrectly marked unhealthy even when replication is working" is **not safe to assume**. On a healthy replica, Memgraph (verified on v3.7.2) reports:

```
data_info: {memgraph: {behind: 0, status: "ready", ts: <non-zero>}}
```

An **empty** `data_info` (`{}`) is NOT a healthy replica misparsed — it means the MAIN has no confirmed replication state for that replica, i.e. data streaming never engaged. In the production incident the replica showed `{}` for 104+ days and genuinely contained zero data while the main had 3151 `DEPENDS_ON` relationships. The original "evidence" above (oteldemo1 showing `{}`) was very likely a real desync too, misdiagnosed as a parser artifact.

**Consequence for the fix:** when implementing the parser, classify `data_info: {}` (or missing per-database entries) as **unhealthy/invalid**, not healthy. The parser bug and real desyncs are independent failure modes that currently produce the same log line; after the fix they must be distinguishable.

### Required Changes

**File:** `internal/memgraph/client.go`

1. Update `parseShowReplicasOutput` to handle the new column format:
   - Column 1: `name`
   - Column 2: `socket_address` (contains host:port combined)
   - Column 3: `sync_mode`
   - Column 4: `system_info` (JSON with system state)
   - Column 5: `data_info` (JSON with replication state)

2. Parse `system_info` and `data_info` JSON fields to determine replica health:
   - `system_info` may contain: `{ts: <timestamp>, behind: <count>, status: "ready"|"syncing"|...}`
   - `data_info` contains per-database replication state

3. Update `ReplicaInfo` struct to include new fields:
   ```go
   type ReplicaInfo struct {
       Name          string
       SocketAddress string  // Combined host:port
       SyncMode      string
       SystemInfo    *ReplicaSystemInfo  // Parsed JSON
       DataInfo      map[string]interface{}  // Parsed JSON
   }

   type ReplicaSystemInfo struct {
       Timestamp int64  `json:"ts"`
       Behind    int64  `json:"behind"`
       Status    string `json:"status"`
   }
   ```

4. Update health check logic in `internal/controller/replication.go:222-233` to use parsed SystemInfo:
   ```go
   // Check SystemInfo.Status if available, otherwise check if DataInfo is populated
   if replica.SystemInfo != nil {
       status = strings.ToLower(replica.SystemInfo.Status)
   }
   ```

**File:** `internal/memgraph/client_test.go`

5. Update `TestParseShowReplicasOutput` with test cases for the new format

### Testing

- Test with Memgraph v2.21.0 output format
- Verify backward compatibility with older Memgraph versions if needed
- Confirm replicas show as healthy when replication is working

---

## Issue 2: Status Updates Trigger Immediate Re-reconciliation (MEDIUM PRIORITY)

### Problem

Every reconciliation ends with `r.Status().Update(ctx, cluster)` at `internal/controller/memgraphcluster_controller.go:516`.

In controller-runtime, updating the status of a watched resource triggers a watch event, which schedules an immediate reconciliation. This bypasses the `RequeueAfter` delay we return.

### Evidence

```
Expected: Reconciliation every 30 seconds (requeueAfterLong)
Actual: Reconciliation every ~1-7 seconds
```

The backoff fix added at lines 273-279 is ineffective because:
1. We return `ctrl.Result{RequeueAfter: 30*time.Second}`
2. But then `updateStatus()` modifies the cluster
3. The status update triggers a watch event
4. Controller-runtime schedules reconciliation immediately

### Impact

- 4-8x more reconciliations than intended
- Each reconciliation makes multiple Memgraph queries
- Overwhelms MAIN instance CPU

### Required Changes

**File:** `internal/controller/memgraphcluster_controller.go`

Option A: **Skip status update when nothing changed**
```go
func (r *MemgraphClusterReconciler) updateStatus(...) error {
    // Compare old vs new status
    if reflect.DeepEqual(oldStatus, newStatus) {
        return nil  // Skip update if nothing changed
    }
    return r.Status().Update(ctx, cluster)
}
```

Option B: **Use rate limiting / debouncing**
- Track last status update time
- Only update status if >N seconds since last update
- Or use controller-runtime's built-in rate limiting

Option C: **Separate status update frequency from reconciliation frequency**
- Only update status on significant changes
- Use annotations or a separate mechanism for tracking reconciliation state

### Recommended Approach

Option A is simplest. Implement deep comparison of status before updating. Most reconciliations don't change status, so this eliminates most spurious updates.

---

## Issue 3: Multiple Memgraph Queries Per Reconciliation (MEDIUM PRIORITY)

### Problem

Each reconciliation makes 4+ queries to the MAIN instance:

1. `ensureMainRole()` → `GetReplicationRole()` (line 135 in replication.go)
2. `ConfigureReplication()` → `ShowReplicas()` (line 63)
3. For each replica: `ensureReplicaRole()` → `GetReplicationRole()` (line 160)
4. `CheckReplicationHealth()` → `ShowReplicas()` (line 212)
5. `collectStorageMetrics()` → `GetStorageInfo()` for each pod

With 2 replicas and frequent reconciliation, this is ~6 queries every few seconds.

### Impact

- High query load on MAIN instance
- CPU saturation when combined with actual workload
- Slow response times for application queries

### Required Changes

**File:** `internal/controller/memgraphcluster_controller.go`

1. **Cache query results within a reconciliation:**
   ```go
   type reconcileContext struct {
       replicationRole map[string]string  // podName -> role
       replicas        []ReplicaInfo
       storageInfo     map[string]*StorageInfo
   }
   ```

2. **Reduce redundant queries:**
   - `ShowReplicas()` is called twice - once in `ConfigureReplication` and once in `CheckReplicationHealth`. Call once and reuse.
   - `GetReplicationRole()` is called for MAIN and each REPLICA. Could batch or cache.

3. **Make storage metrics collection optional or less frequent:**
   - Only collect every N reconciliations
   - Or make it configurable via CRD spec

**File:** `internal/controller/replication.go`

4. Refactor `ConfigureReplication` to return replica info for reuse:
   ```go
   func (rm *ReplicationManager) ConfigureReplication(...) ([]ReplicaInfo, error) {
       replicas, err := rm.client.ShowReplicas(...)
       // ... configure replication ...
       return replicas, nil
   }
   ```

5. Remove separate `CheckReplicationHealth` call - integrate into `ConfigureReplication`

---

## Issue 4: Validation Test Writes to Database (LOW PRIORITY)

### Problem

The `testReplicationLag` function in `internal/controller/validation.go:105-172` performs invasive health checks:
- Writes a test node to MAIN
- Reads from each REPLICA
- Deletes the test node

### Impact

- Adds write load to database during health checks
- Could interfere with actual workload
- Leaves orphan data if cleanup fails

### Required Changes

**File:** `internal/controller/validation.go`

1. Consider using read-only health checks:
   - Query node/edge counts
   - Compare counts between MAIN and REPLICAs
   - Use Memgraph's built-in replication lag metrics if available

2. Make validation configurable:
   ```yaml
   spec:
     validation:
       enabled: true
       mode: "readonly"  # or "write-test"
       interval: 60s
   ```

3. Add circuit breaker for validation:
   - Skip validation if previous N attempts failed
   - Reduce frequency when cluster is under load

### Note (2026-06-04)

`testReplicationLag` (write to MAIN → read from each REPLICA) is exactly the check that **would have caught the 104-day production divergence** — the test node would never have appeared on the replica. Before making validation read-only, investigate why it didn't surface this incident (not running? failing silently? result not acted upon?). Whatever replaces it must still detect a replica that silently stops receiving data. A read-only equivalent: compare `SHOW STORAGE INFO` vertex/edge counts between MAIN and replicas.

---

## Issue 5: `clear-replication-state` Init Container Deletes `epoch_id` (HIGH PRIORITY — root cause of the production outage)

### Problem

The `clear-replication-state` init container in `internal/controller/statefulset.go:222-250` runs on **every pod restart** and executes:

```sh
rm -rf $DATA/.internal/replication
rm -f  $DATA/.internal/replication_*
rm -f  $DATA/.internal/epoch_id        # ← the harmful line
```

The comment claims *"Graph data is preserved - only replication state is cleared."* That combination is precisely the problem: in Memgraph, `epoch_id` is the proof that two instances share commit history. Deleting it while preserving graph data produces an instance that **has data under an unprovable lineage**. When such a pod becomes a REPLICA, the MAIN sees a diverged instance (the documented "replica at one point acted as main / branched history" condition): it accepts registration, keeps a heartbeat TCP session alive, but **never streams data**. `SHOW REPLICAS` shows `data_info: {}` indefinitely.

A fresh, empty replica (ts=0, no epoch) always recovers — MAIN sends a snapshot. A data-bearing replica with wiped epoch **never** recovers automatically.

### Evidence (production incident, 2026-06-04)

- The affected replica pod: role `REPLICA`, registered, MAIN holds an ESTABLISHED TCP session to its `:10000` — yet `MATCH ()-[r:DEPENDS_ON]->() RETURN count(...)` = **0** vs **3151** on MAIN.
- `SHOW REPLICAS` on MAIN: `data_info: {}` (0 properties), `system_info: null`.
- Operator logged `unhealthy replica detected ... status:"{}"` every reconcile for the pods' entire 104-day uptime.
- User impact: a client proxy (`BACKEND_ADDR=<cluster>-graph-read:7687`) intermittently returned empty results to downstream consumers whenever a connection landed on the empty replica (see Issue 7).
- Manual recovery attempts (`DROP REPLICA` + `REGISTER REPLICA`) fail or are futile: registration is not the broken layer, and the reconcile loop (Issue 2) re-registers within seconds anyway.

### Impact

- Any pod restart can silently break replication **permanently** for that replica
- Failure mode is invisible to clients of "registered" status — only `data_info` reveals it
- Combined with Issue 7, broken replicas serve empty results to production traffic

### Required Changes

**File:** `internal/controller/statefulset.go`

1. **Stop deleting `epoch_id`.** The original motivation (MAIN failing at startup trying to reconnect to since-removed replicas) is addressed by clearing the replication *registration* state only:
   ```sh
   rm -rf $DATA/.internal/replication
   rm -f  $DATA/.internal/replication_*
   # epoch_id is NOT deleted — it is required for replica lineage
   ```
2. Verify against the deployed Memgraph version (v3.7.2 in production — see Version Compatibility) which on-disk paths hold (a) replica registrations on MAIN, (b) role state, (c) epoch/lineage. Delete only (a), and only if startup-reconnect failures are still reproducible on that version.
3. Divergence recovery (data wipe + reseed) belongs in the controller as explicit remediation (Issue 6), not in a blanket init container.

### Testing

- Restart a replica pod with data → it must rejoin and continue replicating (`data_info.status == "ready"`, `behind == 0`)
- Restart the MAIN pod → it must come back as MAIN without startup failures and re-establish streaming to replicas
- Restart both pods together (the incident scenario) → replication must re-engage without manual intervention

---

## Issue 6: No Remediation for Invalid/Diverged Replicas (HIGH PRIORITY)

### Problem

The operator can *detect* a broken replica but never *fixes* it:

- `ConfigureReplication` (`internal/controller/replication.go:96`) registers a replica only `if !currentReplicaNames[replicaName]` — an existing-but-dead registration is skipped forever.
- `CheckReplicationHealth` (`internal/controller/replication.go:222-233`) classifies anything not `ready`/`replicating` as unhealthy, logs a warning, emits an event — and does nothing else.

Result: a replica in `invalid`/diverged/never-seeded state stays broken indefinitely while the operator logs the same warning every reconcile (in the incident: every ~1-30s for 104 days).

### Impact

- Replication outages require manual diagnosis and manual PVC surgery to resolve
- Manual `DROP REPLICA`/`REGISTER REPLICA` races the reconcile loop (Issue 2), making operator-fighting the only manual path
- "Self-healing" is the operator's core value proposition and is absent for the most common replication failure

### Required Changes

**File:** `internal/controller/replication.go` (+ controller plumbing)

1. Track consecutive unhealthy observations per replica (in status or in-memory with backoff).
2. After N consecutive cycles (e.g., 5) of `data_info` empty/`invalid`:
   a. `DROP REPLICA <name>` on MAIN
   b. Trigger a clean re-seed of the replica pod: delete the pod **and** wipe its data (annotation consumed by an init container, or PVC delete + recreate — design decision)
   c. On pod Ready: set role, re-register → MAIN seeds a fresh replica via snapshot transfer
3. Emit distinct events for each phase (`ReplicaReseedStarted`, `ReplicaReseedCompleted`, `ReplicaReseedFailed`) so the remediation is observable.
4. Cap retries with exponential backoff; surface a terminal `ReplicationDegraded` condition on the CR if reseeding fails repeatedly.
5. Depends on Issue 1: remediation must key off correctly parsed `data_info`, and `{}` must classify as unhealthy (see Issue 1 correction note).

### Testing

- Inject divergence (wipe `epoch_id` on a data-bearing replica, restart) → operator detects, reseeds, replica converges to MAIN's counts
- Verify no remediation loop on transient `recovery` status (replica catching up must not be reaped)
- Verify backoff: unreachable replica does not cause hot reseed loops

---

## Issue 7: `-read` Service Selects All Pods Regardless of Replication Health (HIGH PRIORITY)

### Problem

`buildReadService` (`internal/controller/service.go:109-115`) ships with an acknowledged placeholder:

```go
// Initially selects all pods; we'll use an annotation or label to mark read pods
// For now, this selects all pods - reads should work on any pod
selector := labelsForCluster(cluster)
```

"Reads should work on any pod" is only true when every replica is in sync. The selector includes the MAIN **and** every replica, healthy or not. There is no mechanism to evict a desynced replica from read rotation.

### Evidence (production incident, 2026-06-04)

A client proxy points at `<cluster>-graph-read:7687`. With one healthy MAIN and one empty replica behind that service, ~50% of new proxy backend connections returned empty query results to downstream consumers. Confirmed by running `SHOW REPLICATION ROLE` + a count query repeatedly through the proxy: role flipped `main`/`replica` in lockstep with count `3151`/`0`.

### Impact

- Replication failures become **user-facing data corruption** (silently empty/stale results) instead of reduced capacity
- Intermittent symptom (connection-dependent) is expensive to diagnose; in the incident it was initially misattributed to the proxy
- Port-forward verification is misleading here: `kubectl port-forward svc/...` pins to one pod, so the read service "looks fine" when tested that way

### Required Changes

**File:** `internal/controller/service.go`, `internal/controller/labels.go`, reconcile loop

1. Maintain a per-pod replication-health label from reconcile results (Issue 1's parsed `data_info`), e.g.:
   ```
   memgraph.base14.io/role: main|replica
   memgraph.base14.io/replication-healthy: "true"|"false"
   ```
2. Change the `-read` selector to `replication-healthy=true` (replicas in `ready`/`replicating`, plus the MAIN if reads-from-main is desired for capacity).
3. **Fallback rule:** if zero healthy replicas exist, the selector must fall back to the MAIN (mirroring `buildWriteService`'s pod-name selector) — never serve a known-broken replica, and never empty the service.
4. Label updates must be prompt on health transitions (tie into Issue 6's remediation phases so a reseeding replica is out of rotation until converged).

### Testing

- Healthy cluster: `-read` endpoints = all healthy pods
- Break one replica (Issue 6 injection): endpoints drop the broken replica within one reconcile; client queries never return empty
- Zero healthy replicas: endpoints = MAIN only
- Replica reseeds: re-enters endpoints only after `ready`/`behind: 0`

---

## Implementation Order

1. **Issue 1** (SHOW REPLICAS parser) - Must fix first: correct health *detection* is a prerequisite for Issues 6 and 7. Apply the correction note (`{}` = unhealthy).
2. **Issue 5** (`epoch_id` wipe) - Stops *creating* new diverged replicas on every restart
3. **Issue 6** (auto-remediation) - Fixes existing/future diverged replicas without manual PVC surgery
4. **Issue 7** (read service health gating) - Stops broken replicas from serving client traffic (closes the user-facing symptom class)
5. **Issue 2** (Status update triggering reconciliation) - Significant impact reduction; also removes the reconcile-race that blocks manual recovery
6. **Issue 3** (Query reduction) - Optimization
7. **Issue 4** (Validation) - Revisit only after understanding why it missed the production divergence

## Estimated Effort

| Issue | Effort | Files Changed |
|-------|--------|---------------|
| Issue 1 | 2-3 hours | client.go, client_test.go, replication.go |
| Issue 2 | 1-2 hours | memgraphcluster_controller.go |
| Issue 3 | 2-3 hours | memgraphcluster_controller.go, replication.go |
| Issue 4 | 1-2 hours | validation.go |
| Issue 5 | 2-3 hours | statefulset.go, statefulset_test.go |
| Issue 6 | 4-6 hours | replication.go, replication_test.go, memgraphcluster_controller.go, api types (status/conditions) |
| Issue 7 | 3-4 hours | service.go, service_test.go, labels.go, memgraphcluster_controller.go |

**Total: ~15-23 hours**

## Version Compatibility

After fixing Issue 1, consider:
- Testing with Memgraph v2.18, v2.19, v2.20, v2.21
- The SHOW REPLICAS format may have changed between versions
- May need version detection and format-specific parsing

**Note (2026-06-04):** The production cluster runs **Memgraph v3.7.2**, while the chart's values claim `memgraph/memgraph:2.21.0` — config drift worth auditing separately. All fixes must be validated against v3.7.2 first; its healthy `SHOW REPLICAS` output is:

```
| name | socket_address | sync_mode | system_info | data_info |
| "x"  | "host:10000"   | "async"   | Null        | {memgraph: {behind: 0, status: "ready", ts: N}} |
```

## Incident Reference

The production investigation (2026-06-04) that produced Issues 5-7 and the Issue 1 correction:
intermittent empty proxy results → traced through proxy → `-read` service → empty replica →
operator init container `epoch_id` wipe. Key reproduction technique: run `SHOW REPLICATION ROLE` plus a
count query repeatedly over fresh connections through the service; lockstep flips identify per-pod
divergence that `kubectl port-forward svc/...` (which pins to one pod) cannot reveal.
