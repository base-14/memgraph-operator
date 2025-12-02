# Memgraph Operator Implementation Plan

## Overview

Build a Kubernetes operator using **Go + Kubebuilder** that manages Memgraph clusters with:
- 1 Write StatefulSet (single pod, MAIN role)
- N Read StatefulSets (one pod each, REPLICA role)
- Native Memgraph replication (default: ASYNC)
- Snapshot-based backups with optional S3 upload
- Graceful shutdown with snapshot creation
- Dynamic read replica scaling
- OpenTelemetry instrumentation

---

## 1. Custom Resource Definition (CRD)

### MemgraphCluster CRD

```yaml
apiVersion: memgraph.io/v1alpha1
kind: MemgraphCluster
metadata:
  name: my-cluster
  namespace: default
spec:
  # Write instance configuration
  write:
    image: memgraph/memgraph:2.21.0
    resources:
      requests:
        memory: "2Gi"
        cpu: "1000m"
      limits:
        memory: "4Gi"
        cpu: "2000m"
    storage:
      size: 10Gi
      storageClassName: ""  # Use default
    config:
      logLevel: "WARNING"
      memoryLimit: 3000  # MB
      walFlushEveryNTx: 100000
    nodeSelector: {}
    tolerations: []
    affinity: {}

  # Read replicas configuration
  read:
    replicas: 3
    image: memgraph/memgraph:2.21.0
    resources:
      requests:
        memory: "2Gi"
        cpu: "500m"
      limits:
        memory: "4Gi"
        cpu: "1000m"
    storage:
      size: 10Gi
      storageClassName: ""
    config:
      logLevel: "WARNING"
      memoryLimit: 3000
    nodeSelector: {}
    tolerations: []
    affinity: {}

  # Replication settings
  replication:
    mode: ASYNC  # ASYNC (default), SYNC, STRICT_SYNC

  # Snapshot configuration
  snapshot:
    enabled: true
    schedule: "*/15 * * * *"  # Cron expression
    retentionCount: 5  # Keep last N snapshots on disk

    # Optional S3 backup
    s3:
      enabled: false
      bucket: ""
      region: "us-east-1"
      endpoint: ""  # For S3-compatible storage
      prefix: "memgraph/snapshots"
      secretRef:
        name: ""  # Secret with access-key-id and secret-access-key
      retentionDays: 7

status:
  phase: Running  # Pending, Running, Failed
  writeReady: true
  readyReplicas: 3
  registeredReplicas: 3
  lastSnapshotTime: "2024-01-15T10:30:00Z"
  lastS3BackupTime: "2024-01-15T10:30:00Z"

  # Real-time validation results
  validation:
    lastConnectivityTest: "2024-01-15T10:30:00Z"
    lastReplicationTest: "2024-01-15T10:30:00Z"
    replicationLagMs: 45
    allReplicasHealthy: true
    replicas:
      - name: replica-0
        healthy: true
        lagMs: 42
        lastTestTime: "2024-01-15T10:30:00Z"
      - name: replica-1
        healthy: true
        lagMs: 48
        lastTestTime: "2024-01-15T10:30:00Z"
      - name: replica-2
        healthy: true
        lagMs: 45
        lastTestTime: "2024-01-15T10:30:00Z"

  conditions:
    - type: WriteReady
      status: "True"
      lastTransitionTime: "2024-01-15T10:00:00Z"
    - type: ReplicasReady
      status: "True"
      lastTransitionTime: "2024-01-15T10:05:00Z"
    - type: WriteConnectivity
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "Write instance responding to queries"
    - type: ReadConnectivity
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "All 3 read instances responding"
    - type: ReplicationHealthy
      status: "True"
      lastTransitionTime: "2024-01-15T10:05:00Z"
      message: "All replicas passed replication test"
    - type: ReplicationLagAcceptable
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "Replication lag 45ms (threshold: 1000ms)"
```

---

## 2. Services Architecture

### 2.1 Write Service (ClusterIP)

Applications connect here for **write operations**:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {cluster-name}-write
spec:
  type: ClusterIP
  ports:
    - name: bolt
      port: 7687
      targetPort: 7687
  selector:
    app.kubernetes.io/name: memgraph
    app.kubernetes.io/instance: {cluster-name}
    app.kubernetes.io/component: write
```

**Connection string:** `bolt://{cluster-name}-write:7687`

### 2.2 Read Service (ClusterIP with Load Balancing)

Applications connect here for **read operations** - automatically load balanced across all read replicas:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {cluster-name}-read
spec:
  type: ClusterIP
  ports:
    - name: bolt
      port: 7687
      targetPort: 7687
  selector:
    app.kubernetes.io/name: memgraph
    app.kubernetes.io/instance: {cluster-name}
    app.kubernetes.io/component: read
```

**Connection string:** `bolt://{cluster-name}-read:7687`

### 2.3 Headless Services (for StatefulSet DNS and Replication)

These enable stable DNS names for pod-to-pod communication:

**Write Headless:**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {cluster-name}-write-hl
spec:
  clusterIP: None
  publishNotReadyAddresses: true
  ports:
    - name: bolt
      port: 7687
    - name: replication
      port: 10000
  selector:
    app.kubernetes.io/component: write
```

**Read Headless (one per replica for stable DNS):**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {cluster-name}-read-{N}
spec:
  clusterIP: None
  publishNotReadyAddresses: true
  ports:
    - name: bolt
      port: 7687
    - name: replication
      port: 10000
  selector:
    app.kubernetes.io/component: read
    app.kubernetes.io/read-index: "{N}"
```

### 2.4 DNS Names Summary

| Purpose | DNS Name |
|---------|----------|
| Write queries | `{name}-write.{namespace}.svc.cluster.local:7687` |
| Read queries (LB) | `{name}-read.{namespace}.svc.cluster.local:7687` |
| Write pod (internal) | `{name}-write-0.{name}-write-hl.{namespace}.svc.cluster.local` |
| Read pod N (internal) | `{name}-read-{N}-0.{name}-read-{N}.{namespace}.svc.cluster.local` |

---

## 3. Operator Architecture

### Controllers

1. **MemgraphClusterReconciler** (main controller)
   - Watches: MemgraphCluster, StatefulSets, Services, Pods
   - Responsibilities:
     - Create/update write StatefulSet
     - Create/update read StatefulSets
     - Create services (write, read, headless)
     - Monitor pod readiness
     - Trigger replication registration

2. **ReplicationController** (separate reconciler or part of main)
   - Watches: Pods with memgraph labels
   - Responsibilities:
     - Detect pod restarts (via generation/restart count)
     - Re-register replicas after restart
     - Monitor replication health via `SHOW REPLICAS;`

3. **SnapshotController** (or integrated into main)
   - Responsibilities:
     - Schedule periodic snapshots via CronJob or internal timer
     - Trigger S3 uploads
     - Manage snapshot retention

### Reconciliation Flow

```
MemgraphCluster Created/Updated
         │
         ▼
┌─────────────────────────────────────┐
│ 1. Reconcile Write StatefulSet      │
│    - Create if not exists           │
│    - Update if spec changed         │
│    - Configure preStop hook         │
│    - Set terminationGracePeriod     │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 2. Reconcile Write Services         │
│    - ClusterIP service (write)      │
│    - Headless service (write-hl)    │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 3. Wait for Write Pod Ready         │
│    - Check pod phase                │
│    - Verify Memgraph responding     │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 4. Reconcile Read StatefulSets      │
│    - Create N StatefulSets          │
│    - Handle scale up/down           │
│    - Remove excess replicas         │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 5. Reconcile Read Services          │
│    - ClusterIP service (read) - LB  │
│    - N headless services            │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 6. Register Replicas                │
│    - For each ready read pod        │
│    - SET REPLICATION ROLE TO REPLICA│
│    - REGISTER REPLICA on main       │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 7. Reconcile Snapshot CronJob       │
│    - Create/update if enabled       │
│    - Configure S3 backup            │
└─────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────┐
│ 8. Update Status                    │
│    - Set phase                      │
│    - Update ready counts            │
│    - Update conditions              │
└─────────────────────────────────────┘
```

---

## 4. Key Implementation Details

### 4.1 Graceful Shutdown (Write Pod)

Configure the write StatefulSet with:

```yaml
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 300  # 5 minutes
      containers:
        - name: memgraph
          lifecycle:
            preStop:
              exec:
                command:
                  - /bin/bash
                  - -c
                  - |
                    echo "Creating snapshot before shutdown..."
                    echo "CREATE SNAPSHOT;" | mgconsole --host 127.0.0.1 --port 7687 --use-ssl=false
                    echo "Snapshot created, proceeding with shutdown"
                    sleep 5
```

### 4.2 Snapshot Restore on Write Restart

When write pod restarts:
1. Operator detects pod restart (via ObservedGeneration or restart count)
2. Memgraph automatically loads from latest snapshot in `/var/lib/memgraph/snapshots`
3. Operator waits for write pod to be ready
4. Operator re-registers all replicas

**Note:** Memgraph automatically restores from the latest snapshot on startup - no explicit restore command needed.

### 4.3 Read Pod Restart Re-clustering

When a read pod restarts:
1. Operator watches Pod events (or periodic reconciliation detects restart)
2. Operator checks if replica is registered via `SHOW REPLICAS;` on main
3. If not registered (or shows disconnected):
   a. Execute on read pod: `SET REPLICATION ROLE TO REPLICA WITH PORT 10000;`
   b. Execute on main pod: `REGISTER REPLICA replica_N ASYNC TO 'host:10000';`
4. Verify registration successful

### 4.4 Dynamic Replica Scaling

**Scale Up:**
1. User updates `spec.read.replicas` from 3 to 5
2. Operator creates StatefulSets read-3 and read-4
3. Waits for pods to be ready
4. Registers new replicas with main

**Scale Down:**
1. User updates `spec.read.replicas` from 5 to 3
2. Operator unregisters replicas 3 and 4 from main:
   `DROP REPLICA replica_3;`
3. Deletes StatefulSets read-3 and read-4
4. PVCs remain (configurable: delete or retain)

### 4.5 Replication Health Monitoring

Periodically (or on status update):
1. Query main: `SHOW REPLICAS;`
2. Parse output to get replica status
3. Update status.conditions with replication health
4. Emit events for unhealthy replicas

### 4.6 Real-Time Validation Tests

The operator conducts real-time tests to verify the cluster is functioning correctly, not just that resources exist.

#### 4.6.1 Replication Validation Test

After registering a replica, verify data actually replicates:

```go
// 1. Write test node to main
testID := uuid.New().String()
writeQuery := fmt.Sprintf("CREATE (n:_OperatorTest {id: '%s', ts: timestamp()}) RETURN n;", testID)
execOnMain(ctx, writeQuery)

// 2. Wait for replication (with timeout based on mode)
timeout := 5 * time.Second  // ASYNC
if mode == "SYNC" {
    timeout = 2 * time.Second
}

// 3. Read from replica and verify
readQuery := fmt.Sprintf("MATCH (n:_OperatorTest {id: '%s'}) RETURN n;", testID)
err := retry.Do(func() error {
    result := execOnReplica(ctx, replicaHost, readQuery)
    if result.Empty() {
        return errors.New("test node not replicated yet")
    }
    return nil
}, retry.Timeout(timeout), retry.Delay(500*time.Millisecond))

// 4. Cleanup test node
cleanupQuery := fmt.Sprintf("MATCH (n:_OperatorTest {id: '%s'}) DELETE n;", testID)
execOnMain(ctx, cleanupQuery)

// 5. Update status based on result
if err != nil {
    // Mark replica as unhealthy, emit event
    setCondition(ReplicationHealthy, false, "ReplicationTestFailed", err.Error())
}
```

#### 4.6.2 Connectivity Validation Test

Verify Bolt protocol connectivity to each instance:

```go
// Test write instance connectivity
func validateWriteConnectivity(ctx context.Context, host string) error {
    query := "RETURN 1 AS ping;"
    result, err := execQuery(ctx, host, query)
    if err != nil {
        return fmt.Errorf("write instance not responding: %w", err)
    }
    return nil
}

// Test read instance connectivity
func validateReadConnectivity(ctx context.Context, host string) error {
    query := "RETURN 1 AS ping;"
    result, err := execQuery(ctx, host, query)
    if err != nil {
        return fmt.Errorf("read instance not responding: %w", err)
    }
    return nil
}
```

#### 4.6.3 Replication Lag Measurement

Measure actual replication lag for monitoring:

```go
func measureReplicationLag(ctx context.Context, mainHost string, replicaHost string) (time.Duration, error) {
    // 1. Create timestamped test node on main
    testID := uuid.New().String()
    writeTime := time.Now()
    writeQuery := fmt.Sprintf("CREATE (n:_OperatorLagTest {id: '%s', ts: %d}) RETURN n;",
        testID, writeTime.UnixNano())
    execOnMain(ctx, writeQuery)

    // 2. Poll replica until node appears
    readQuery := fmt.Sprintf("MATCH (n:_OperatorLagTest {id: '%s'}) RETURN n.ts AS ts;", testID)
    var replicatedTime time.Time
    err := retry.Do(func() error {
        result := execOnReplica(ctx, replicaHost, readQuery)
        if result.Empty() {
            return errors.New("not replicated")
        }
        replicatedTime = time.Now()
        return nil
    }, retry.Timeout(30*time.Second))

    // 3. Cleanup
    cleanupQuery := fmt.Sprintf("MATCH (n:_OperatorLagTest {id: '%s'}) DELETE n;", testID)
    execOnMain(ctx, cleanupQuery)

    if err != nil {
        return 0, err
    }

    lag := replicatedTime.Sub(writeTime)

    // 4. Record metric
    telemetry.ReplicationLagSeconds.Record(ctx, lag.Seconds(),
        metric.WithAttributes(attribute.String("replica", replicaHost)))

    return lag, nil
}
```

#### 4.6.4 Snapshot Validation Test

Verify snapshots are being created correctly:

```go
func validateSnapshot(ctx context.Context, writeHost string) error {
    // 1. Trigger snapshot
    execOnMain(ctx, "CREATE SNAPSHOT;")

    // 2. List snapshots directory
    snapshotListQuery := "CALL mg.list_snapshots() YIELD * RETURN *;"
    result := execOnMain(ctx, snapshotListQuery)

    // 3. Verify at least one snapshot exists
    if result.Empty() {
        return errors.New("no snapshots found after CREATE SNAPSHOT")
    }

    // 4. Check snapshot is recent (within last minute)
    latestSnapshot := result.First()
    snapshotTime := latestSnapshot.Get("timestamp")
    if time.Since(snapshotTime) > 1*time.Minute {
        return errors.New("latest snapshot is stale")
    }

    return nil
}
```

#### 4.6.5 Test Schedule

| Test | Frequency | Trigger |
|------|-----------|---------|
| Connectivity (write) | Every 30s | Periodic reconcile |
| Connectivity (read) | Every 30s | Periodic reconcile |
| Replication validation | After replica registration | On pod ready |
| Replication validation | Every 5 min | Periodic health check |
| Replication lag | Every 1 min | Periodic metrics |
| Snapshot validation | After CronJob runs | On snapshot complete |

#### 4.6.6 Status Updates from Tests

Tests update the CR status with detailed results:

```yaml
status:
  phase: Running
  validation:
    lastConnectivityTest: "2024-01-15T10:30:00Z"
    lastReplicationTest: "2024-01-15T10:30:00Z"
    replicationLagMs: 45
    allReplicasHealthy: true
  conditions:
    - type: WriteConnectivity
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "Write instance responding to queries"
    - type: ReadConnectivity
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "All 3 read instances responding"
    - type: ReplicationHealthy
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "All replicas passed replication test"
    - type: ReplicationLagAcceptable
      status: "True"
      lastTransitionTime: "2024-01-15T10:30:00Z"
      message: "Replication lag 45ms (threshold: 1000ms)"
```

#### 4.6.7 Events Emitted

The operator emits Kubernetes events for test results:

```go
// Success events
r.Recorder.Event(cluster, corev1.EventTypeNormal, "ReplicationTestPassed",
    fmt.Sprintf("Replica %s passed replication validation", replicaName))

// Warning events
r.Recorder.Event(cluster, corev1.EventTypeWarning, "ReplicationTestFailed",
    fmt.Sprintf("Replica %s failed replication test: %s", replicaName, err))

r.Recorder.Event(cluster, corev1.EventTypeWarning, "HighReplicationLag",
    fmt.Sprintf("Replica %s lag is %dms (threshold: %dms)", replicaName, lag, threshold))

r.Recorder.Event(cluster, corev1.EventTypeWarning, "ConnectivityLost",
    fmt.Sprintf("Lost connectivity to %s: %s", instanceName, err))
```

---

## 5. Resources Created by Operator

For a MemgraphCluster named `mycluster` with 3 read replicas:

| Resource | Name | Purpose |
|----------|------|---------|
| **StatefulSets** | | |
| StatefulSet | `mycluster-write` | Write instance (1 pod) |
| StatefulSet | `mycluster-read-0` | Read replica 0 |
| StatefulSet | `mycluster-read-1` | Read replica 1 |
| StatefulSet | `mycluster-read-2` | Read replica 2 |
| **Application Services** | | |
| Service | `mycluster-write` | **Write endpoint** - ClusterIP for write queries |
| Service | `mycluster-read` | **Read endpoint** - ClusterIP, load balances across all read pods |
| **Headless Services (internal)** | | |
| Service | `mycluster-write-hl` | Write pod DNS for replication |
| Service | `mycluster-read-0` | Read-0 pod DNS |
| Service | `mycluster-read-1` | Read-1 pod DNS |
| Service | `mycluster-read-2` | Read-2 pod DNS |
| **Other** | | |
| CronJob | `mycluster-snapshot` | Periodic snapshots + S3 upload |
| ServiceAccount | `mycluster` | For exec operations |
| Role | `mycluster` | RBAC permissions |
| RoleBinding | `mycluster` | Bind SA to role |
| PVC | `data-mycluster-write-0` | Write storage |
| PVC | `data-mycluster-read-{N}-0` | Read storage |

---

## 6. Project Structure

```
memgraph-operator/
├── .github/
│   └── workflows/
│       ├── ci.yaml                    # CI pipeline (lint, test, build)
│       └── release.yaml               # Release pipeline
├── .golangci.yml                      # Golangci-lint configuration
├── Dockerfile                         # Multi-stage build
├── Makefile                           # Build, test, lint targets
├── PROJECT                            # Kubebuilder project file
├── go.mod
├── go.sum
├── cmd/
│   └── main.go                        # Operator entrypoint
├── api/
│   └── v1alpha1/
│       ├── memgraphcluster_types.go   # CRD types
│       ├── memgraphcluster_types_test.go
│       ├── groupversion_info.go
│       └── zz_generated.deepcopy.go
├── config/
│   ├── crd/
│   │   └── bases/
│   │       └── memgraph.io_memgraphclusters.yaml
│   ├── default/
│   ├── manager/
│   ├── rbac/
│   └── samples/
│       └── memgraph_v1alpha1_memgraphcluster.yaml
├── internal/
│   ├── controller/
│   │   ├── memgraphcluster_controller.go
│   │   ├── memgraphcluster_controller_test.go
│   │   ├── statefulset.go
│   │   ├── statefulset_test.go
│   │   ├── service.go
│   │   ├── service_test.go
│   │   ├── replication.go
│   │   ├── replication_test.go
│   │   ├── snapshot.go
│   │   └── snapshot_test.go
│   ├── memgraph/
│   │   ├── client.go                  # Memgraph query client (exec wrapper)
│   │   └── client_test.go
│   ├── validation/
│   │   ├── connectivity.go            # Connectivity tests
│   │   ├── connectivity_test.go
│   │   ├── replication.go             # Replication validation tests
│   │   ├── replication_test.go
│   │   ├── lag.go                     # Replication lag measurement
│   │   ├── lag_test.go
│   │   ├── snapshot.go                # Snapshot validation
│   │   ├── snapshot_test.go
│   │   └── runner.go                  # Test runner/scheduler
│   └── telemetry/
│       ├── otel.go                    # OpenTelemetry setup
│       ├── metrics.go                 # Custom metrics
│       └── tracing.go                 # Tracing helpers
└── test/
    ├── e2e/
    │   ├── e2e_suite_test.go
    │   └── memgraphcluster_test.go
    └── utils/
        └── testutils.go               # Test helpers
```

---

## 7. Makefile Targets

```makefile
# Project configuration
IMG ?= memgraph-operator:latest
ENVTEST_K8S_VERSION = 1.29.0

##@ General
.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

##@ Development
.PHONY: manifests
manifests: controller-gen ## Generate CRD manifests
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code (DeepCopy, etc.)
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

##@ Testing
.PHONY: test
test: manifests generate fmt vet envtest ## Run unit tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
	go test ./... -coverprofile cover.out -covermode=atomic

.PHONY: test-unit
test-unit: ## Run unit tests only (no envtest)
	go test ./internal/... -coverprofile cover.out -covermode=atomic -short

.PHONY: test-integration
test-integration: manifests generate envtest ## Run integration tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
	go test ./internal/controller/... -coverprofile cover.out -covermode=atomic

.PHONY: test-e2e
test-e2e: ## Run e2e tests (requires running cluster)
	go test ./test/e2e/... -v -timeout 30m

.PHONY: coverage
coverage: test ## Generate coverage report
	go tool cover -html=cover.out -o coverage.html
	@echo "Coverage report: coverage.html"

##@ Code Quality
.PHONY: lint
lint: golangci-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run --timeout 5m

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint with auto-fix
	$(GOLANGCI_LINT) run --fix --timeout 5m

.PHONY: staticcheck
staticcheck: ## Run staticcheck
	go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

.PHONY: gosec
gosec: ## Run security scanner
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	gosec ./...

.PHONY: check
check: fmt vet lint staticcheck test ## Run all checks

##@ Build
.PHONY: build
build: manifests generate fmt vet ## Build operator binary
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run operator locally
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push ${IMG}

.PHONY: docker-buildx
docker-buildx: ## Build and push multi-arch image
	docker buildx build --platform linux/amd64,linux/arm64 -t ${IMG} --push .

##@ Deployment
.PHONY: install
install: manifests kustomize ## Install CRDs into cluster
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from cluster
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy operator to cluster
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy operator from cluster
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

##@ Release
.PHONY: release
release: manifests kustomize ## Generate release manifests
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Tools
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KUSTOMIZE ?= $(LOCALBIN)/kustomize
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN)
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE)
$(KUSTOMIZE): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@v5.3.0

.PHONY: envtest
envtest: $(ENVTEST)
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/ cover.out coverage.html dist/
```

---

## 8. Dockerfile (Multi-stage Build)

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /workspace

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION:-dev}" \
    -o manager cmd/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy binary from builder
COPY --from=builder /workspace/manager .

# Copy timezone data for proper time handling
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

USER 65532:65532

ENTRYPOINT ["/manager"]
```

---

## 9. Golangci-lint Configuration

```yaml
# .golangci.yml
run:
  timeout: 5m
  go: "1.22"

linters:
  enable:
    # Default linters
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    # Additional linters
    - bodyclose
    - dogsled
    - dupl
    - errorlint
    - exhaustive
    - exportloopref
    - funlen
    - gocognit
    - goconst
    - gocritic
    - gocyclo
    - godot
    - gofmt
    - goimports
    - gomnd
    - goprintffuncname
    - gosec
    - lll
    - misspell
    - nakedret
    - nestif
    - nilerr
    - nlreturn
    - noctx
    - nolintlint
    - prealloc
    - predeclared
    - revive
    - rowserrcheck
    - sqlclosecheck
    - stylecheck
    - tparallel
    - unconvert
    - unparam
    - whitespace
    - wsl

linters-settings:
  funlen:
    lines: 100
    statements: 50
  gocognit:
    min-complexity: 20
  gocyclo:
    min-complexity: 15
  lll:
    line-length: 140
  gomnd:
    checks:
      - argument
      - case
      - condition
      - return
    ignored-numbers:
      - "0"
      - "1"
      - "2"
      - "3"
    ignored-functions:
      - "time.*"
  govet:
    enable-all: true
  revive:
    rules:
      - name: blank-imports
      - name: context-as-argument
      - name: context-keys-type
      - name: dot-imports
      - name: error-return
      - name: error-strings
      - name: error-naming
      - name: exported
      - name: increment-decrement
      - name: indent-error-flow
      - name: package-comments
      - name: range
      - name: receiver-naming
      - name: time-naming
      - name: unexported-return
      - name: var-declaration
      - name: var-naming

issues:
  exclude-rules:
    # Exclude some linters from running on tests files
    - path: _test\.go
      linters:
        - funlen
        - dupl
        - gomnd
    # Exclude generated files
    - path: zz_generated
      linters:
        - all
```

---

## 10. OpenTelemetry Integration

### 10.1 Telemetry Setup

```go
// internal/telemetry/otel.go
package telemetry

import (
    "context"
    "os"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Config struct {
    ServiceName    string
    ServiceVersion string
    OTLPEndpoint   string  // e.g., "otel-collector:4317"
    MetricsAddr    string  // e.g., ":8080"
}

func InitTelemetry(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
    // Create resource with service info
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName(cfg.ServiceName),
            semconv.ServiceVersion(cfg.ServiceVersion),
        ),
        resource.WithHost(),
        resource.WithProcess(),
    )
    if err != nil {
        return nil, err
    }

    // Setup trace exporter
    traceExporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    // Setup trace provider
    tp := trace.NewTracerProvider(
        trace.WithBatcher(traceExporter),
        trace.WithResource(res),
        trace.WithSampler(trace.AlwaysSample()),
    )
    otel.SetTracerProvider(tp)

    // Setup propagator
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    ))

    // Setup metrics exporter (Prometheus)
    promExporter, err := prometheus.New()
    if err != nil {
        return nil, err
    }

    mp := metric.NewMeterProvider(
        metric.WithReader(promExporter),
        metric.WithResource(res),
    )
    otel.SetMeterProvider(mp)

    // Return shutdown function
    return func(ctx context.Context) error {
        if err := tp.Shutdown(ctx); err != nil {
            return err
        }
        return mp.Shutdown(ctx)
    }, nil
}
```

### 10.2 Custom Metrics

```go
// internal/telemetry/metrics.go
package telemetry

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

var (
    meter = otel.Meter("memgraph-operator")

    // Cluster metrics
    ClustersTotal      metric.Int64UpDownCounter
    ClustersReady      metric.Int64UpDownCounter
    ReadReplicasTotal  metric.Int64UpDownCounter
    ReadReplicasReady  metric.Int64UpDownCounter

    // Replication metrics
    ReplicationLagSeconds metric.Float64Histogram
    ReplicationErrors     metric.Int64Counter

    // Snapshot metrics
    SnapshotsTotal    metric.Int64Counter
    SnapshotDuration  metric.Float64Histogram
    S3UploadsTotal    metric.Int64Counter
    S3UploadErrors    metric.Int64Counter

    // Reconciliation metrics
    ReconcileTotal    metric.Int64Counter
    ReconcileDuration metric.Float64Histogram
    ReconcileErrors   metric.Int64Counter

    // Validation metrics
    ValidationTotal       metric.Int64Counter
    ValidationFailures    metric.Int64Counter
    ValidationDuration    metric.Float64Histogram
    ConnectivityTestTotal metric.Int64Counter
    ConnectivityFailures  metric.Int64Counter
    ReplicationTestTotal  metric.Int64Counter
    ReplicationTestFailures metric.Int64Counter
)

func InitMetrics() error {
    var err error

    ClustersTotal, err = meter.Int64UpDownCounter("memgraph_clusters_total",
        metric.WithDescription("Total number of MemgraphCluster resources"))
    if err != nil {
        return err
    }

    ClustersReady, err = meter.Int64UpDownCounter("memgraph_clusters_ready",
        metric.WithDescription("Number of ready MemgraphCluster resources"))
    if err != nil {
        return err
    }

    ReadReplicasTotal, err = meter.Int64UpDownCounter("memgraph_read_replicas_total",
        metric.WithDescription("Total number of read replicas across all clusters"))
    if err != nil {
        return err
    }

    ReadReplicasReady, err = meter.Int64UpDownCounter("memgraph_read_replicas_ready",
        metric.WithDescription("Number of ready read replicas"))
    if err != nil {
        return err
    }

    ReplicationLagSeconds, err = meter.Float64Histogram("memgraph_replication_lag_seconds",
        metric.WithDescription("Replication lag in seconds"),
        metric.WithUnit("s"))
    if err != nil {
        return err
    }

    ReplicationErrors, err = meter.Int64Counter("memgraph_replication_errors_total",
        metric.WithDescription("Total replication errors"))
    if err != nil {
        return err
    }

    SnapshotsTotal, err = meter.Int64Counter("memgraph_snapshots_total",
        metric.WithDescription("Total snapshots created"))
    if err != nil {
        return err
    }

    SnapshotDuration, err = meter.Float64Histogram("memgraph_snapshot_duration_seconds",
        metric.WithDescription("Snapshot creation duration"),
        metric.WithUnit("s"))
    if err != nil {
        return err
    }

    ReconcileTotal, err = meter.Int64Counter("memgraph_reconcile_total",
        metric.WithDescription("Total reconciliation attempts"))
    if err != nil {
        return err
    }

    ReconcileDuration, err = meter.Float64Histogram("memgraph_reconcile_duration_seconds",
        metric.WithDescription("Reconciliation duration"),
        metric.WithUnit("s"))
    if err != nil {
        return err
    }

    ReconcileErrors, err = meter.Int64Counter("memgraph_reconcile_errors_total",
        metric.WithDescription("Total reconciliation errors"))
    if err != nil {
        return err
    }

    return nil
}
```

### 10.3 Tracing in Reconciler

```go
// Example usage in controller
func (r *MemgraphClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ctx, span := otel.Tracer("memgraph-operator").Start(ctx, "Reconcile",
        trace.WithAttributes(
            attribute.String("cluster.name", req.Name),
            attribute.String("cluster.namespace", req.Namespace),
        ),
    )
    defer span.End()

    start := time.Now()
    defer func() {
        telemetry.ReconcileDuration.Record(ctx, time.Since(start).Seconds(),
            metric.WithAttributes(
                attribute.String("cluster", req.Name),
            ),
        )
    }()

    // ... reconciliation logic with child spans

    ctx, writeSpan := otel.Tracer("memgraph-operator").Start(ctx, "ReconcileWriteStatefulSet")
    err := r.reconcileWriteStatefulSet(ctx, cluster)
    writeSpan.End()

    // ...
}
```

---

## 11. Unit Testing Strategy

### 11.1 Test Structure

```go
// internal/controller/memgraphcluster_controller_test.go
package controller

import (
    "context"
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    memgraphv1alpha1 "github.com/base14/memgraph-operator/api/v1alpha1"
)

var _ = Describe("MemgraphCluster Controller", func() {
    Context("When reconciling a new MemgraphCluster", func() {
        It("Should create write StatefulSet", func() {
            // Test implementation
        })

        It("Should create write service", func() {
            // Test implementation
        })

        It("Should create read service", func() {
            // Test implementation
        })

        It("Should create N read StatefulSets", func() {
            // Test implementation
        })
    })

    Context("When scaling read replicas", func() {
        It("Should add new StatefulSets on scale up", func() {
            // Test implementation
        })

        It("Should remove StatefulSets on scale down", func() {
            // Test implementation
        })

        It("Should unregister replicas before deletion", func() {
            // Test implementation
        })
    })

    Context("When handling pod restarts", func() {
        It("Should re-register replica after restart", func() {
            // Test implementation
        })

        It("Should re-register all replicas after write restart", func() {
            // Test implementation
        })
    })
})
```

### 11.2 Table-Driven Tests

```go
// internal/controller/statefulset_test.go
func TestBuildWriteStatefulSet(t *testing.T) {
    tests := []struct {
        name     string
        cluster  *memgraphv1alpha1.MemgraphCluster
        wantErr  bool
        validate func(t *testing.T, sts *appsv1.StatefulSet)
    }{
        {
            name: "default configuration",
            cluster: &memgraphv1alpha1.MemgraphCluster{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      "test-cluster",
                    Namespace: "default",
                },
                Spec: memgraphv1alpha1.MemgraphClusterSpec{
                    Write: memgraphv1alpha1.WriteSpec{
                        Image: "memgraph/memgraph:2.21.0",
                    },
                },
            },
            wantErr: false,
            validate: func(t *testing.T, sts *appsv1.StatefulSet) {
                assert.Equal(t, "test-cluster-write", sts.Name)
                assert.Equal(t, int32(1), *sts.Spec.Replicas)
                assert.Equal(t, int64(300), *sts.Spec.Template.Spec.TerminationGracePeriodSeconds)
            },
        },
        {
            name: "custom resources",
            cluster: &memgraphv1alpha1.MemgraphCluster{
                // ... custom config
            },
            validate: func(t *testing.T, sts *appsv1.StatefulSet) {
                // ... assertions
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            sts, err := buildWriteStatefulSet(tt.cluster)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            tt.validate(t, sts)
        })
    }
}
```

### 11.3 Mock Client for Memgraph

```go
// internal/memgraph/client_test.go
type MockMemgraphClient struct {
    mock.Mock
}

func (m *MockMemgraphClient) ExecuteQuery(ctx context.Context, host string, query string) (string, error) {
    args := m.Called(ctx, host, query)
    return args.String(0), args.Error(1)
}

func (m *MockMemgraphClient) ShowReplicas(ctx context.Context, host string) ([]Replica, error) {
    args := m.Called(ctx, host)
    return args.Get(0).([]Replica), args.Error(1)
}

func TestRegisterReplica(t *testing.T) {
    mockClient := new(MockMemgraphClient)
    mockClient.On("ExecuteQuery", mock.Anything, "main-host:7687",
        "SET REPLICATION ROLE TO REPLICA WITH PORT 10000;").Return("", nil)
    mockClient.On("ExecuteQuery", mock.Anything, "main-host:7687",
        mock.MatchedBy(func(q string) bool {
            return strings.Contains(q, "REGISTER REPLICA")
        })).Return("", nil)

    err := registerReplica(context.Background(), mockClient, "main-host:7687", "replica-0", "ASYNC")
    assert.NoError(t, err)
    mockClient.AssertExpectations(t)
}
```

---

## 12. Implementation Phases

### Phase 1: Core Operator Setup
- [ ] Initialize Kubebuilder project
- [ ] Define MemgraphCluster CRD types
- [ ] Generate CRD manifests
- [ ] Implement basic reconciler skeleton
- [ ] Setup Makefile with all targets
- [ ] Setup golangci-lint configuration
- [ ] Setup Dockerfile

### Phase 2: Write Instance Management
- [ ] Create write StatefulSet with proper config
- [ ] Create write services (ClusterIP + headless)
- [ ] Configure preStop hook for graceful shutdown
- [ ] Implement status updates for write readiness
- [ ] Add unit tests for StatefulSet builder

### Phase 3: Read Instance Management
- [ ] Create N read StatefulSets
- [ ] Create read services (ClusterIP + headless per replica)
- [ ] Implement wait-for-main init container
- [ ] Handle dynamic scaling (up/down)
- [ ] Add unit tests for scaling logic

### Phase 4: Replication Management
- [ ] Implement Memgraph client (exec into pods)
- [ ] Register replicas on pod ready
- [ ] Detect pod restarts and re-register
- [ ] Monitor replication health
- [ ] Add unit tests for replication logic

### Phase 5: Snapshot & Backup
- [ ] Create snapshot CronJob
- [ ] Implement S3 backup (optional)
- [ ] Handle snapshot on shutdown
- [ ] Snapshot retention/cleanup
- [ ] Add unit tests for snapshot logic

### Phase 6: Real-Time Validation
- [ ] Implement connectivity tests (write + read)
- [ ] Implement replication validation test (write-then-read)
- [ ] Implement replication lag measurement
- [ ] Implement snapshot validation test
- [ ] Create validation runner with configurable schedule
- [ ] Update CR status with validation results
- [ ] Emit Kubernetes events for test failures
- [ ] Add unit tests for validation logic
- [ ] Add OTel metrics for validation results

### Phase 7: Observability
- [ ] Setup OpenTelemetry provider
- [ ] Implement custom metrics
- [ ] Add tracing to reconciler
- [ ] Add metrics endpoint
- [ ] Document metrics and traces

### Phase 8: Testing & Polish
- [ ] Integration tests with envtest
- [ ] E2E tests with kind/minikube
- [ ] Documentation and examples
- [ ] CI/CD pipeline setup

---

## 13. Dependencies

```go
// go.mod
module github.com/base14/memgraph-operator

go 1.22

require (
    // Kubernetes
    k8s.io/api v0.29.x
    k8s.io/apimachinery v0.29.x
    k8s.io/client-go v0.29.x
    sigs.k8s.io/controller-runtime v0.17.x

    // OpenTelemetry
    go.opentelemetry.io/otel v1.24.0
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.24.0
    go.opentelemetry.io/otel/exporters/prometheus v0.46.0
    go.opentelemetry.io/otel/sdk v1.24.0
    go.opentelemetry.io/otel/sdk/metric v1.24.0

    // Testing
    github.com/onsi/ginkgo/v2 v2.15.0
    github.com/onsi/gomega v1.31.0
    github.com/stretchr/testify v1.9.0
)
```

---

## 14. Example Usage

```yaml
# Deploy a Memgraph cluster
apiVersion: memgraph.io/v1alpha1
kind: MemgraphCluster
metadata:
  name: production
  namespace: memgraph
spec:
  write:
    image: memgraph/memgraph:2.21.0
    resources:
      requests:
        memory: "4Gi"
        cpu: "2000m"
      limits:
        memory: "8Gi"
        cpu: "4000m"
    storage:
      size: 50Gi
      storageClassName: fast-ssd
  read:
    replicas: 3
    image: memgraph/memgraph:2.21.0
    resources:
      requests:
        memory: "4Gi"
        cpu: "1000m"
      limits:
        memory: "8Gi"
        cpu: "2000m"
    storage:
      size: 50Gi
  replication:
    mode: ASYNC  # Default
  snapshot:
    enabled: true
    schedule: "*/10 * * * *"
    s3:
      enabled: true
      bucket: prod-memgraph-backups
      region: us-west-2
      prefix: clusters/production
      secretRef:
        name: memgraph-s3-credentials
```

**Application connection:**
```python
# Write operations
write_driver = GraphDatabase.driver("bolt://production-write:7687")

# Read operations (auto load-balanced)
read_driver = GraphDatabase.driver("bolt://production-read:7687")
```

---

## 16. Success Criteria

### Functional
- [ ] MemgraphCluster CRD can be created/updated/deleted
- [ ] Write instance comes up with persistent storage
- [ ] Write service routes to write pod
- [ ] Read service load-balances across all read pods
- [ ] N read replicas are created and registered
- [ ] Read replicas auto-reconnect after restart
- [ ] Write instance snapshots before shutdown
- [ ] Write instance restores from snapshot on restart
- [ ] Dynamic scaling works (both up and down)
- [ ] S3 backups work when configured

### Real-Time Validation
- [ ] Connectivity tests run every 30s and update status
- [ ] Replication validation tests verify actual data flow
- [ ] Replication lag is measured and exposed as metric
- [ ] Snapshot validation confirms snapshots are created
- [ ] Failed validation tests emit Kubernetes events
- [ ] Status reflects actual cluster health, not just resource existence

### Quality
- [ ] Unit test coverage > 80%
- [ ] All linters pass (golangci-lint, staticcheck, gosec)
- [ ] Operator handles edge cases gracefully

### Observability
- [ ] OTel metrics exported (Prometheus format)
- [ ] OTel traces exported to collector
- [ ] Validation metrics: `memgraph_validation_*`
- [ ] Replication lag metric: `memgraph_replication_lag_seconds`

### CI/CD
- [ ] CI pipeline passes (lint, test, build)
- [ ] Docker image builds for amd64/arm64
- [ ] Release manifests generated

---

## 17. Implementation Summary (What Was Built)

### Architecture Change from Original Plan

The implementation uses a **simplified architecture** compared to the original plan:

| Original Plan | Actual Implementation |
|---------------|----------------------|
| Separate Write + Read StatefulSets | Single StatefulSet with dynamic roles |
| N separate StatefulSets for N read replicas | One StatefulSet with N replicas |
| Write/Read components | Any pod can be MAIN or REPLICA |
| Complex service topology | Simplified: headless + write + read services |

This design allows any pod (`cluster-0`, `cluster-1`, etc.) to dynamically take the MAIN or REPLICA role, enabling easier failover and simpler scaling.

### Completed Implementation Phases

#### Phase 1: Core Operator Setup ✅
- Initialized Kubebuilder project with `memgraph.io/v1alpha1` API
- Defined `MemgraphCluster` CRD with comprehensive spec/status types
- Generated CRD manifests and DeepCopy methods
- Set up Makefile, Dockerfile, and golangci-lint configuration

#### Phase 2: StatefulSet & Services ✅
- Created single StatefulSet builder (`statefulset.go`)
  - Configurable replicas, image, storage, resources
  - Init containers for sysctl and volume permissions
  - PreStop hook for graceful shutdown with snapshot
  - Startup, liveness, and readiness probes
- Created services (`service.go`)
  - Headless service for pod DNS resolution
  - Write service (ClusterIP) pointing to MAIN instance
  - Read service (ClusterIP) for load-balanced read queries

#### Phase 3: Replication Management ✅
- Created `ReplicationManager` (`replication.go`)
  - `ConfigureReplication()`: Sets up MAIN/REPLICA roles
  - `ensureMainRole()` / `ensureReplicaRole()`: Role assignment
  - `cleanupStaleReplicas()`: Removes obsolete replica registrations
  - `CheckReplicationHealth()`: Queries `SHOW REPLICAS;` for health
  - `HandleMainFailover()`: Promotes replica on MAIN failure (HA)
- Created Memgraph client (`internal/memgraph/client.go`)
  - Uses `kubectl exec` to run Cypher queries in pods
  - Methods: `ExecuteQuery`, `GetReplicationRole`, `SetReplicationRole`, `RegisterReplica`, `UnregisterReplica`, `ShowReplicas`, `CreateSnapshot`

#### Phase 4: Snapshot & Backup ✅
- Created `SnapshotManager` (`snapshot.go`)
  - `TriggerSnapshot()`: On-demand snapshot creation
  - `buildSnapshotCronJob()`: Scheduled snapshots via CronJob
  - S3 backup support with optional endpoint configuration
  - Configurable schedule (default: every 15 minutes)
- Integrated snapshot CronJob reconciliation into controller

#### Phase 5: Real-Time Validation ✅
- Created `ValidationManager` (`validation.go`)
  - `RunValidation()`: Tests connectivity and replication
  - `testConnectivity()`: Verifies Bolt protocol response
  - `testReplicationLag()`: Measures actual replication delay
  - `UpdateValidationStatus()`: Updates CR status with results
- Status includes per-instance health, lag measurements, and timestamps

#### Phase 6: Observability ✅
- Created Prometheus metrics (`metrics.go`)
  - Cluster gauges: phase, ready/desired instances, registered replicas
  - Replication metrics: lag per replica
  - Reconciliation counters and histograms: total, errors, duration
  - Snapshot counters: total triggered
  - Failover counters
  - Validation counters and histograms
- Integrated `MetricsRecorder` into controller reconcile loop

#### Phase 7: Testing & Polish ✅
- Unit tests for all components:
  - `replication_test.go`: FQDN generation, replica naming, failover logic
  - `snapshot_test.go`: CronJob building, S3 command generation
  - `validation_test.go`: Timestamp parsing, status updates
  - `metrics_test.go`: Metric recording functions
  - `statefulset_test.go`: StatefulSet building
  - `service_test.go`: Service building
- All tests pass: `go test ./...`
- All lint checks pass: `make lint`
- Generated CRD manifests: `make generate manifests`

### Key Files Created

| File | Purpose |
|------|---------|
| `api/v1alpha1/memgraphcluster_types.go` | CRD types (Spec, Status, nested structs) |
| `internal/controller/memgraphcluster_controller.go` | Main reconciler with all integration |
| `internal/controller/statefulset.go` | StatefulSet builder |
| `internal/controller/service.go` | Service builders |
| `internal/controller/replication.go` | Replication management |
| `internal/controller/snapshot.go` | Snapshot CronJob management |
| `internal/controller/validation.go` | Real-time validation tests |
| `internal/controller/metrics.go` | Prometheus metrics |
| `internal/memgraph/client.go` | Memgraph query client via kubectl exec |
| `config/crd/bases/memgraph.io_memgraphclusters.yaml` | Generated CRD manifest |

### What Remains / Future Work

- **E2E Tests**: Integration tests with actual Kubernetes cluster
- **OpenTelemetry Tracing**: Full OTel integration (metrics done, tracing planned)
- **CI/CD Pipeline**: GitHub Actions for automated testing and releases
- **Helm Chart**: Packaging for easy deployment
- **Documentation**: User guide, API reference, examples
