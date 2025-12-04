# memgraph-operator

A Kubernetes operator for deploying and managing Memgraph graph database clusters with built-in replication, snapshots, and S3 backups.

## Description

The Memgraph Operator automates the deployment and lifecycle management of Memgraph clusters on Kubernetes. It provides:

- **Cluster Management**: Deploy Memgraph clusters with N replicas (1 MAIN + N-1 REPLICAs)
- **Automatic Replication**: Configures MAIN/REPLICA topology with ASYNC, SYNC, or STRICT_SYNC modes
- **Snapshot Management**: Scheduled snapshots via CronJobs with configurable retention
- **S3 Backups**: Optional backup uploads to S3-compatible storage
- **Service Discovery**: Automatic creation of write (MAIN) and read (REPLICA) services
- **Health Monitoring**: Tracks cluster phase, replication status, and instance health

## Getting Started

### Prerequisites
- Kubernetes 1.26+
- Helm 3.x

### Installation

See the [Helm chart repository](https://github.com/base-14/memgraph-operator-helm) for installation instructions and configuration options.

### Uninstallation

```sh
helm uninstall memgraph-operator -n memgraph-system
```

Note: CRDs and cluster resources persist after uninstallation. Delete them manually if complete cleanup is needed.

## Metrics

The operator exposes Prometheus metrics for monitoring cluster health and operations:

| Metric | Type | Description |
|--------|------|-------------|
| `memgraph_cluster_phase` | Gauge | Current phase of the cluster (0=Pending, 1=Initializing, 2=Running, 3=Failed) |
| `memgraph_cluster_ready_instances` | Gauge | Number of ready instances in the cluster |
| `memgraph_cluster_desired_instances` | Gauge | Desired number of instances in the cluster |
| `memgraph_cluster_registered_replicas` | Gauge | Number of replicas registered with the main instance |
| `memgraph_replication_lag_milliseconds` | Gauge | Replication lag in milliseconds |
| `memgraph_replication_healthy` | Gauge | Whether replication is healthy (1) or not (0) |
| `memgraph_instance_healthy` | Gauge | Whether an instance is healthy (1) or not (0) |
| `memgraph_reconcile_operations_total` | Counter | Total number of reconcile operations by result |
| `memgraph_reconcile_duration_seconds` | Histogram | Duration of reconcile operations in seconds |
| `memgraph_snapshot_last_success_timestamp_seconds` | Gauge | Unix timestamp of the last successful snapshot |
| `memgraph_snapshot_operations_total` | Counter | Total number of snapshot operations by result |
| `memgraph_failover_events_total` | Counter | Total number of failover events |
| `memgraph_validation_last_run_timestamp_seconds` | Gauge | Unix timestamp of the last validation run |
| `memgraph_validation_passed` | Gauge | Whether the last validation passed (1) or not (0) |

### Storage Metrics (from SHOW STORAGE INFO)

The operator collects storage statistics from each Memgraph instance:

| Metric | Type | Description |
|--------|------|-------------|
| `memgraph_storage_vertex_count` | Gauge | Number of vertices in the database |
| `memgraph_storage_edge_count` | Gauge | Number of edges in the database |
| `memgraph_storage_average_degree` | Gauge | Average degree of vertices |
| `memgraph_storage_memory_resident_bytes` | Gauge | Current resident memory usage |
| `memgraph_storage_memory_peak_bytes` | Gauge | Peak resident memory usage |
| `memgraph_storage_disk_usage_bytes` | Gauge | Disk space consumed |
| `memgraph_storage_memory_tracked_bytes` | Gauge | Actively tracked memory allocation |
| `memgraph_storage_allocation_limit_bytes` | Gauge | Maximum memory allocation limit |
| `memgraph_storage_unreleased_delta_objects` | Gauge | Delta objects awaiting cleanup |

All metrics include `cluster` and `namespace` labels. Instance-level metrics also include `instance` and `role` labels.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

