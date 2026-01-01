# Memgraph Upgrade Guide: 2.21 → 3.7

This document covers breaking changes and feature additions when upgrading from Memgraph 2.21.0 (released November 6, 2024) to 3.7.x.

## Executive Summary

| Category | Count |
|----------|-------|
| Breaking Changes | 15+ |
| Major New Features | 40+ |
| Critical Bug Fixes | 20+ |

**Risk Level**: Medium-High. The 2.x → 3.x transition includes architectural changes, permission model overhaul, and removed utilities.

---

## Part A: Breaking Changes

### Critical (Requires Action Before Upgrade)

#### 1. Fine-Grained Access Control Overhaul (v3.7.0)

**Impact**: All per-label and per-edge-type permissions must be manually recreated.

The permission model changed from hierarchical to discrete:

```
# Old (v3.6 and earlier) - hierarchical
GRANT UPDATE ON LABELS :User TO developer;

# New (v3.7+) - discrete, explicit
GRANT READ, UPDATE ON NODES CONTAINING LABELS :User TO developer;
```

Key changes:
- `NOTHING → READ → UPDATE → CREATE_DELETE` hierarchy removed
- Each permission (CREATE, READ, UPDATE, DELETE) now independent
- New syntax: `NODES CONTAINING LABELS`, `EDGES OF TYPE`
- Labels can use `MATCHING ANY` or `MATCHING EXACTLY` modifiers

**Migration steps**:
1. Export current permissions: `SHOW PRIVILEGES FOR [user/role]`
2. Backup auth storage: `/var/lib/memgraph/auth/`
3. Upgrade Memgraph
4. Manually recreate all per-label/edge permissions with new syntax

See: [MLBAC Migration Guide](https://memgraph.com/docs/database-management/authentication-and-authorization/mlbac-migration-guide)

---

#### 2. `mg_import_csv` Removed (v3.6.0, v3.7.0)

**Impact**: If you use `mg_import_csv` for data loading, you must switch to `LOAD CSV`.

```cypher
# Use this instead
LOAD CSV FROM "/path/to/file.csv" WITH HEADER AS row
CREATE (n:Label {prop: row.column});
```

---

#### 3. TTL (Time-To-Live) Durability Reset (v3.5.0)

**Impact**: TTL configurations from pre-v3.5.0 are ignored after upgrade.

All TTL settings must be reconfigured after upgrading.

---

#### 4. WAL Disabled Mode Forbidden Under Replication/HA (v3.6.0)

**Impact**: If running `--storage-wal-enabled=false` with replication, this is now prohibited.

Replication and HA require WAL to be enabled.

---

### Moderate (May Require Code Changes)

#### 5. Procedure Results Default to Null (v3.2.0)

**Impact**: Query behavior change for procedures with missing yield fields.

```
# Before: threw exception if field missing
# After: returns null for missing fields
```

Update error handling in applications that relied on exceptions.

---

#### 6. DateTime Timezone Behavior Change (v3.6.1)

**Impact**: `datetime({timezone:"Europe/Brussels"})` now returns current time.

```cypher
# Before (v3.6.0): returned minimum datetime values
# After (v3.6.1): returns current clock time in specified timezone

# To get old behavior, specify explicit fields:
datetime({year: 0, month: 1, day: 1, timezone: "Europe/Brussels"})
```

---

#### 7. System Queries Require Database Access (v3.5.0)

**Impact**: System queries now require both correct privileges AND access to "memgraph" database.

Users running system commands may need additional grants.

---

#### 8. Index Type Output Format Changes

Multiple versions changed `SHOW INDEX INFO` output:

| Version | Change |
|---------|--------|
| v3.4.0 | Vector indexes: `vector` → `label+property_vector` |
| v3.6.0 | Text indexes: `text` → `label_text` or `edge-type_text` |

**Impact**: Scripts parsing index info output need updates.

---

### Low (Deployment/Infrastructure)

#### 9. Dynamic libstdc++ Linking (v3.5.0)

**Impact**: Native (non-Docker) installations may need `libstdc++` available.

Docker/K8s deployments are unaffected.

---

#### 10. Debian 11 Support Dropped (v3.7.0)

**Impact**: Must use Debian 12+ for native installations.

---

#### 11. Docker Image Tag Format Change (v3.2.0+)

**Impact**: Update image references in deployments.

```
# Old format
memgraph/memgraph-mage:3.2-memgraph-3.2

# New format
memgraph/memgraph-mage:3.2
```

---

#### 12. Coordinator Environment Variables Ignored (v3.6.0)

**Impact**: `MEMGRAPH_USER` and `MEMGRAPH_PASSWORD` environment variables are ignored when running coordinators.

---

#### 13. `text-search` Removed from Experimental Features (v3.6.0)

**Impact**: Remove `text-search` from `--experimental-enabled` flag if present.

---

---

## Part B: Feature Additions

### Memory & GC Improvements (Relevant to Your Delta Issue)

| Version | Feature | Description |
|---------|---------|-------------|
| v3.7.1 | `--storage-gc-aggressive` | Full GC cleanup with unique lock (blocks briefly) |
| v3.7.1 | GC shared lock in analytical mode | Reduces blocking during GC |
| v3.7.2 | Parallel vector index recovery | `--storage-parallel-schema-recovery=true` |
| v3.5.0 | User resource limits | Per-user memory caps via `USER PROFILE` |

---

### Query & Cypher Enhancements

| Version | Feature | Example |
|---------|---------|---------|
| v3.5.0 | EXISTS subqueries | `WHERE EXISTS { MATCH (a)-[:KNOWS]->(b) }` |
| v3.5.0 | KShortest paths | `MATCH ((n1)-[*KShortest]->(n2))` |
| v3.5.2 | Pattern expressions in WHERE | `WHERE NOT (a)-[]->(:Something)` |
| v3.2.0 | OR label expressions | `MATCH (n:Label1\|Label2)` |
| v3.6.0 | Nested property updates | Update map properties in-place |
| v3.7.0 | Parquet file loading | `LOAD PARQUET FROM '/path/file.parquet'` |

---

### Indexing Improvements

| Version | Feature | Description |
|---------|---------|-------------|
| v3.2.0 | Composite indexes | Indexes on 2+ properties |
| v3.2.0 | Global edge property index | `CREATE GLOBAL EDGE INDEX ON :(prop)` |
| v3.3.0 | Nested property indexes | Indexes on map properties using dot notation |
| v3.4.0 | Vector indexes on edges | `CREATE VECTOR EDGE INDEX` |
| v3.4.0 | Vector quantization | `scalar_kind` for reduced memory |
| v3.5.0 | Multi-property text indexes | `CREATE TEXT INDEX ON :Label(prop1, prop2)` |
| v3.6.0 | Text indexes on edges | `CREATE TEXT EDGE INDEX` |

---

### Replication & HA

| Version | Feature | Description |
|---------|---------|-------------|
| v3.5.0 | STRICT_SYNC replication | Zero-data-loss failovers |
| v3.5.0 | `SHOW REPLICATION LAG` | Transaction lag monitoring |
| v3.6.0 | `max_failover_replica_lag` | Control acceptable lag for failover |
| v3.6.0 | `max_replica_read_lag` | Coordinator setting for read lag |
| v3.6.0 | RPC versioning (ISSU) | In-service software upgrades |
| v3.7.2 | Snapshots on replicas | Manual or scheduled snapshots on replicas |

---

### Security & Access Control

| Version | Feature | Description |
|---------|---------|-------------|
| v3.5.0 | Multiple roles per user | Database-specific permissions |
| v3.5.0 | `SHOW ACTIVE USERS` | View active user sessions |
| v3.5.0 | `USER PROFILE` | Per-user resource limits and monitoring |
| v3.7.0 | Custom SSO certificates | `MEMGRAPH_SSO_CUSTOM_OIDC_EXTRA_CA_CERTS` |

---

### Operational Commands

| Version | Command | Description |
|---------|---------|-------------|
| v3.5.0 | `SHOW INDEXES` | List all indexes |
| v3.5.0 | `SHOW CONSTRAINTS` | List all constraints |
| v3.5.0 | `SHOW VECTOR INDEXES` | List vector indexes |
| v3.5.0 | `SHOW TRIGGER INFO` | Trigger details |
| v3.5.0 | `CREATE SNAPSHOT` returns path | Get snapshot file location |
| v3.6.0 | `DROP ALL INDEXES` | Remove all indexes at once |
| v3.6.0 | `DROP ALL CONSTRAINTS` | Remove all constraints at once |
| v3.6.0 | `DROP DATABASE FORCE` | Force drop with active connections |
| v3.6.0 | Database renaming | Rename databases/tenants |

---

### Performance & Scheduling

| Version | Feature | Description |
|---------|---------|-------------|
| v3.3.0 | Priority-based work scheduler | Prevents blocking under heavy load |
| v3.4.0 | Non-blocking index creation | Reads never blocked during index builds |
| v3.4.0 | `getHopsCounter()` | Track traversal depth in queries |

---

### Vector & AI Features

| Version | Feature | Description |
|---------|---------|-------------|
| v3.0.0 | Basic vector capabilities | Foundation for vector search |
| v3.4.0 | Edge vector indexes | Vector similarity on relationships |
| v3.4.0 | Quantization options | Memory-efficient vector storage |
| v3.6.0 | `cosine_similarity()` function | In-query vector calculations |
| v3.6.2 | Embedding module | Sentence embeddings on CPU/GPU |
| v3.6.2 | `text()` function | String list embeddings |
| v3.6.2 | `model_info()` function | Embedding model metadata |

---

## Part C: Recommended Upgrade Path

### Option 1: Direct Upgrade (If No FGAC)

If you don't use fine-grained access control:

1. Backup: `CREATE SNAPSHOT;` + copy data directory
2. Update image to `memgraph/memgraph:3.7.2`
3. Test in staging environment
4. Deploy to production
5. Reconfigure TTL settings if used
6. Enable new GC flags: `--storage-gc-aggressive=true`

### Option 2: Staged Upgrade (Recommended for Production)

```
2.21 → 3.2 → 3.5 → 3.7
```

This allows testing at each major feature boundary.

### Option 3: Fresh Deployment (If Major Schema Changes Needed)

1. Deploy new 3.7.2 cluster
2. Export data: `DUMP DATABASE;`
3. Import to new cluster
4. Switch traffic

---

## Part D: Configuration Changes for Your Delta Issue

After upgrading to 3.7.x, consider these settings:

```bash
# Aggressive GC for delta cleanup (briefly blocks system)
--storage-gc-aggressive=true

# Faster GC cycles
--storage-gc-cycle-sec=10

# Query timeout to prevent long-running transactions
--query-execution-timeout-sec=300

# Parallel operations
--storage-parallel-schema-recovery=true
--storage-parallel-snapshot-creation=true
```

Also leverage new `USER PROFILE` feature to set per-user memory limits.

---

## Part E: Enterprise vs Community (Open Source) Features

If you're running the open-source Community edition, be aware of licensing changes.

### Features That Moved to Enterprise in v3.0+

**Dynamic Graph Algorithms** — These now require an Enterprise license:

| Algorithm | Description |
|-----------|-------------|
| `betweenness_centrality_online` | Real-time centrality updates |
| `community_detection_online` | Streaming community detection |
| `katz_centrality_online` | Dynamic Katz centrality |
| `node2vec_online` | Streaming node embeddings |
| `pagerank_online` | Real-time PageRank |

The static versions (`pagerank`, `community_detection`, `betweenness_centrality`, etc.) remain free.

---

### Already Enterprise-Only (No Change from 2.21)

These were already paywalled:

| Feature | Description |
|---------|-------------|
| Automatic failover | Manual failover still free in Community |
| RBAC / LBAC | Role-based and label-based access control |
| Multi-tenancy | Multiple databases per instance |
| LDAP / SSO / SAML | External auth integrations |
| Audit logging | Query audit trail |
| TTL (Time-To-Live) | Auto-delete expired nodes |
| CRON snapshot scheduling | Cron expressions for snapshots (interval-based is free) |

---

### New Features That Are Enterprise-Only

| Feature | Version | Description |
|---------|---------|-------------|
| `USER PROFILE` | v3.5.0 | Per-user memory limits and session caps |
| STRICT_SYNC replication | v3.5.0 | Zero-data-loss failover mode |
| Multiple roles per user | v3.5.0 | Database-specific permissions |

---

### What Stays Free in Community Edition

| Category | Features |
|----------|----------|
| **Core** | ACID transactions, Cypher, triggers, streams |
| **Durability** | WAL, snapshots, manual backup |
| **HA** | Replication with manual failover |
| **Auth** | Native authentication, SSL encryption |
| **Indexes** | All index types (vector, text, composite, nested) |
| **Algorithms** | 50+ static algorithms in MAGE |
| **New in 3.x** | Parquet loading, EXISTS subqueries, OR labels, nested property indexes, composite indexes |

---

### Pre-Upgrade Checklist for Community Users

Before upgrading, verify you're not using Enterprise features:

```cypher
-- Check for dynamic algorithm usage (Enterprise in 3.0+)
-- If you call any of these, you'll be blocked:
CALL pagerank_online.get()
CALL community_detection_online.get()
CALL betweenness_centrality_online.get()
CALL katz_centrality_online.get()
CALL node2vec_online.get()

-- Check for TTL usage (Enterprise)
CALL ttl.set(...)
```

If using CRON expressions for snapshots (not `--storage-snapshot-interval-sec`), that's also Enterprise-only.

---

## References

- [Release Notes](https://memgraph.com/docs/release-notes)
- [GitHub Releases](https://github.com/memgraph/memgraph/releases)
- [MLBAC Migration Guide](https://memgraph.com/docs/database-management/authentication-and-authorization/mlbac-migration-guide)
- [Platform Migration Guide](https://memgraph.com/docs/data-migration/migrate-memgraph-platform)
- [Enabling Memgraph Enterprise](https://memgraph.com/docs/database-management/enabling-memgraph-enterprise)
- [Available Algorithms](https://memgraph.com/docs/advanced-algorithms/available-algorithms)
- [Pricing](https://memgraph.com/pricing)
