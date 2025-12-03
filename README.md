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
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/memgraph-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/memgraph-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/memgraph-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/memgraph-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

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

