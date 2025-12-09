# Memgraph Operator - Required Fixes

## Background

During debugging of KB pod crashes (2025-12-05), we identified several issues in the memgraph-operator that cause high CPU usage on Memgraph MAIN instances and incorrect health reporting.

## Issue Summary

1. **SHOW REPLICAS output format changed in Memgraph v2.21.0** - Parser is incompatible
2. **Status updates trigger immediate re-reconciliation** - Backoff is ineffective
3. **Multiple queries per reconciliation** - Overwhelms Memgraph under frequent reconciliation

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

---

## Implementation Order

1. **Issue 1** (SHOW REPLICAS parser) - Must fix first, causes all other issues to be worse
2. **Issue 2** (Status update triggering reconciliation) - Significant impact reduction
3. **Issue 3** (Query reduction) - Optimization
4. **Issue 4** (Validation) - Nice to have

## Estimated Effort

| Issue | Effort | Files Changed |
|-------|--------|---------------|
| Issue 1 | 2-3 hours | client.go, client_test.go, replication.go |
| Issue 2 | 1-2 hours | memgraphcluster_controller.go |
| Issue 3 | 2-3 hours | memgraphcluster_controller.go, replication.go |
| Issue 4 | 1-2 hours | validation.go |

**Total: ~6-10 hours**

## Version Compatibility

After fixing Issue 1, consider:
- Testing with Memgraph v2.18, v2.19, v2.20, v2.21
- The SHOW REPLICAS format may have changed between versions
- May need version detection and format-specific parsing
