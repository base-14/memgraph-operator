// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"bytes"
	"context"
	"encoding/json"
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

// Client provides methods to interact with Memgraph instances via kubectl exec
type Client struct {
	clientset *kubernetes.Clientset
	config    *rest.Config
}

// NewClient creates a new Memgraph client
func NewClient(config *rest.Config) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &Client{
		clientset: clientset,
		config:    config,
	}, nil
}

// ExecuteQuery executes a Cypher query on a Memgraph instance
func (c *Client) ExecuteQuery(ctx context.Context, namespace, podName, query string) (string, error) {
	cmd := []string{
		"mgconsole",
		"--host", "127.0.0.1",
		"--port", "7687",
		"--use-ssl=false",
		"--no-history",
		"--output-format", "tabular",
	}

	// Pass query via stdin
	stdin := strings.NewReader(query + "\n")

	stdout, stderr, err := c.execInPod(ctx, namespace, podName, "memgraph", cmd, stdin)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w, stderr: %s", err, stderr)
	}

	return stdout, nil
}

// SetReplicationRole sets the replication role for a Memgraph instance
func (c *Client) SetReplicationRole(ctx context.Context, namespace, podName string, isMain bool) error {
	var query string
	if isMain {
		query = "SET REPLICATION ROLE TO MAIN;"
	} else {
		query = "SET REPLICATION ROLE TO REPLICA WITH PORT 10000;"
	}

	_, err := c.ExecuteQuery(ctx, namespace, podName, query)
	if err != nil {
		return fmt.Errorf("failed to set replication role: %w", err)
	}

	return nil
}

// RegisterReplica registers a replica with the main instance
func (c *Client) RegisterReplica(ctx context.Context, namespace, mainPodName, replicaName, replicaHost string, mode string) error {
	// mode should be ASYNC, SYNC, or STRICT_SYNC
	if mode == "" {
		mode = "ASYNC"
	}

	query := fmt.Sprintf("REGISTER REPLICA %s %s TO '%s:10000';", replicaName, mode, replicaHost)

	_, err := c.ExecuteQuery(ctx, namespace, mainPodName, query)
	if err != nil {
		return fmt.Errorf("failed to register replica %s: %w", replicaName, err)
	}

	return nil
}

// UnregisterReplica removes a replica from the main instance
func (c *Client) UnregisterReplica(ctx context.Context, namespace, mainPodName, replicaName string) error {
	query := fmt.Sprintf("DROP REPLICA %s;", replicaName)

	_, err := c.ExecuteQuery(ctx, namespace, mainPodName, query)
	if err != nil {
		// Ignore error if replica doesn't exist
		if !strings.Contains(err.Error(), "doesn't exist") {
			return fmt.Errorf("failed to unregister replica %s: %w", replicaName, err)
		}
	}

	return nil
}

// Replica status values derived from SHOW REPLICAS data_info.
// "invalid" is also used when data_info is empty ({}), which means the main
// has no confirmed replication state for the replica — data streaming never
// engaged even though the registration exists.
const (
	ReplicaStatusReady       = "ready"
	ReplicaStatusReplicating = "replicating"
	ReplicaStatusRecovery    = "recovery"
	ReplicaStatusInvalid     = "invalid"
)

// ReplicaDBInfo is the per-database replication state from data_info,
// e.g. {memgraph: {behind: 0, status: "ready", ts: 2}}
type ReplicaDBInfo struct {
	Behind    int64  `json:"behind"`
	Status    string `json:"status"`
	Timestamp int64  `json:"ts"`
}

// ReplicaInfo contains information about a registered replica
type ReplicaInfo struct {
	Name          string
	SocketAddress string // "host:port" as reported by SHOW REPLICAS
	Host          string
	Port          int
	SyncMode      string
	DataInfo      map[string]ReplicaDBInfo // parsed data_info, nil when empty/Null
	Behind        int64                    // worst behind across databases
	Status        string                   // derived: ready|replicating|recovery|invalid
	Timestamp     int64                    // latest confirmed replication timestamp
}

// IsHealthy reports whether the replica is actively streaming data.
// A registered replica with empty data_info is NOT healthy — registration
// and heartbeat can be alive while no data is replicated.
func (r ReplicaInfo) IsHealthy() bool {
	return r.Status == ReplicaStatusReady || r.Status == ReplicaStatusReplicating
}

// DataInfoPresent reports whether the main has confirmed replication state
// for this replica. False means data streaming never engaged.
func (r ReplicaInfo) DataInfoPresent() bool {
	return len(r.DataInfo) > 0
}

// StorageInfo contains storage statistics from SHOW STORAGE INFO
type StorageInfo struct {
	Name                   string
	VertexCount            int64
	EdgeCount              int64
	AverageDegree          float64
	MemoryRes              int64 // bytes
	PeakMemoryRes          int64 // bytes
	DiskUsage              int64 // bytes
	MemoryTracked          int64 // bytes
	AllocationLimit        int64 // bytes
	UnreleasedDeltaObjects int64
	StorageMode            string
	IsolationLevel         string
}

// ShowReplicas returns the list of registered replicas from the main instance
func (c *Client) ShowReplicas(ctx context.Context, namespace, mainPodName string) ([]ReplicaInfo, error) {
	query := "SHOW REPLICAS;"

	output, err := c.ExecuteQuery(ctx, namespace, mainPodName, query)
	if err != nil {
		return nil, fmt.Errorf("failed to show replicas: %w", err)
	}

	return parseShowReplicasOutput(output), nil
}

// GetReplicationRole returns the current replication role of the instance
func (c *Client) GetReplicationRole(ctx context.Context, namespace, podName string) (string, error) {
	query := "SHOW REPLICATION ROLE;"

	output, err := c.ExecuteQuery(ctx, namespace, podName, query)
	if err != nil {
		return "", fmt.Errorf("failed to get replication role: %w", err)
	}

	// Parse output - should contain "main" or "replica"
	output = strings.TrimSpace(strings.ToLower(output))
	if strings.Contains(output, "main") {
		return "MAIN", nil
	} else if strings.Contains(output, "replica") {
		return "REPLICA", nil
	}

	return "UNKNOWN", nil
}

// CreateSnapshot triggers a snapshot on the instance
func (c *Client) CreateSnapshot(ctx context.Context, namespace, podName string) error {
	query := "CREATE SNAPSHOT;"

	_, err := c.ExecuteQuery(ctx, namespace, podName, query)
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	return nil
}

// Ping checks if the Memgraph instance is responsive
func (c *Client) Ping(ctx context.Context, namespace, podName string) error {
	query := "RETURN 1;"

	_, err := c.ExecuteQuery(ctx, namespace, podName, query)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// GetStorageInfo returns storage statistics from the instance
func (c *Client) GetStorageInfo(ctx context.Context, namespace, podName string) (*StorageInfo, error) {
	query := "SHOW STORAGE INFO;"

	output, err := c.ExecuteQuery(ctx, namespace, podName, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage info: %w", err)
	}

	info := parseStorageInfoOutput(output)

	// Debug: if all numeric values are zero but we got output, log it
	if output != "" && info.VertexCount == 0 && info.MemoryRes == 0 && info.AllocationLimit == 0 {
		// Check if any lines were actually parsed by looking for non-empty name
		if info.Name == "" && info.StorageMode == "" {
			// Nothing was parsed - log the raw output for debugging
			return info, fmt.Errorf("storage info parsing returned empty results, raw output length: %d bytes, first 500 chars: %.500s", len(output), output)
		}
	}

	return info, nil
}

// execInPod executes a command in a pod container
func (c *Client) execInPod(ctx context.Context, namespace, podName, container string, cmd []string, stdin *strings.Reader) (string, string, error) {
	req := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	streamOptions := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	if stdin != nil {
		streamOptions.Stdin = stdin
	}

	err = exec.StreamWithContext(ctx, streamOptions)
	if err != nil {
		return stdout.String(), stderr.String(), err
	}

	return stdout.String(), stderr.String(), nil
}

// parseStorageInfoOutput parses the output of SHOW STORAGE INFO command
// Output format is a table with | storage info | value | columns
// Note: Memgraph returns quoted keys and string values (e.g., | "name" | "memgraph" |)
func parseStorageInfoOutput(output string) *StorageInfo {
	info := &StorageInfo{}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") {
			continue
		}

		// Parse table row: | storage info | value |
		if strings.HasPrefix(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 3 {
				key := strings.TrimSpace(parts[1])
				value := strings.TrimSpace(parts[2])

				// Strip surrounding quotes from key and value if present
				// Memgraph returns keys like "name" and string values like "memgraph"
				key = strings.Trim(key, "\"")
				value = strings.Trim(value, "\"")

				switch key {
				case "name":
					info.Name = value
				case "vertex_count":
					info.VertexCount, _ = strconv.ParseInt(value, 10, 64)
				case "edge_count":
					info.EdgeCount, _ = strconv.ParseInt(value, 10, 64)
				case "average_degree":
					info.AverageDegree, _ = strconv.ParseFloat(value, 64)
				case "memory_res":
					info.MemoryRes = parseMemoryValue(value)
				case "peak_memory_res":
					info.PeakMemoryRes = parseMemoryValue(value)
				case "disk_usage":
					info.DiskUsage = parseMemoryValue(value)
				case "memory_tracked":
					info.MemoryTracked = parseMemoryValue(value)
				case "allocation_limit":
					info.AllocationLimit = parseMemoryValue(value)
				case "unreleased_delta_objects":
					info.UnreleasedDeltaObjects, _ = strconv.ParseInt(value, 10, 64)
				case "storage_mode":
					info.StorageMode = value
				case "global_isolation_level":
					info.IsolationLevel = value
				}
			}
		}
	}

	return info
}

// parseMemoryValue parses memory values that may have units (e.g., "1.5 GiB", "512 MiB", "43.16MiB")
// Handles both space-separated (e.g., "512 MiB") and concatenated (e.g., "43.16MiB") formats
func parseMemoryValue(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	// Try parsing as plain number first
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}

	// Parse values with units - try space-separated first
	parts := strings.Fields(value)
	if len(parts) >= 2 {
		// Space-separated format: "512 MiB"
		num, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0
		}
		return applyMemoryUnit(num, parts[1])
	}

	// No space - try to extract number and unit from concatenated format: "43.16MiB"
	if len(parts) == 1 {
		numStr, unit := splitNumberAndUnit(parts[0])
		if numStr == "" {
			return 0
		}
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0
		}
		if unit == "" {
			return int64(num)
		}
		return applyMemoryUnit(num, unit)
	}

	return 0
}

// splitNumberAndUnit splits a string like "43.16MiB" into ("43.16", "MiB")
func splitNumberAndUnit(s string) (string, string) {
	// Find where the unit starts (first letter)
	for i, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// applyMemoryUnit multiplies the number by the appropriate unit multiplier
func applyMemoryUnit(num float64, unit string) int64 {
	unit = strings.ToUpper(unit)
	switch unit {
	case "B", "BYTES":
		return int64(num)
	case "KB", "KIB":
		return int64(num * 1024)
	case "MB", "MIB":
		return int64(num * 1024 * 1024)
	case "GB", "GIB":
		return int64(num * 1024 * 1024 * 1024)
	case "TB", "TIB":
		return int64(num * 1024 * 1024 * 1024 * 1024)
	}
	return int64(num)
}

// parseShowReplicasOutput parses the output of SHOW REPLICAS command.
//
// Current Memgraph (v2.21+/v3.x) format:
//
//	| name | socket_address | sync_mode | system_info | data_info |
//	| "r0" | "host:10000"   | "async"   | Null        | {memgraph: {behind: 0, status: "ready", ts: 2}} |
//
// Legacy format (pre-v2.21):
//
//	| name | host | port | mode | status |
//
// The two are distinguished per row: the legacy format has a numeric port in
// the third column where the current format has a sync_mode string.
func parseShowReplicasOutput(output string) []ReplicaInfo {
	lines := strings.Split(output, "\n")
	replicas := make([]ReplicaInfo, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") || !strings.HasPrefix(line, "|") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 7 {
			continue
		}

		cols := make([]string, 0, len(parts)-2)
		for _, p := range parts[1 : len(parts)-1] {
			cols = append(cols, strings.TrimSpace(p))
		}

		// Strip surrounding quotes from replica name if present
		// Memgraph returns names like "replica_name" but DROP REPLICA expects unquoted names
		name := strings.Trim(cols[0], "\"")
		if name == "" || name == "name" {
			continue
		}

		// Legacy format detection: numeric port in the third column
		if port, err := strconv.Atoi(strings.Trim(cols[2], "\"")); err == nil {
			replicas = append(replicas, ReplicaInfo{
				Name:     name,
				Host:     strings.Trim(cols[1], "\""),
				Port:     port,
				SyncMode: strings.Trim(cols[3], "\""),
				Status:   strings.Trim(cols[4], "\""),
			})
			continue
		}

		replica := ReplicaInfo{
			Name:          name,
			SocketAddress: strings.Trim(cols[1], "\""),
			SyncMode:      strings.Trim(cols[2], "\""),
			DataInfo:      parseDataInfo(cols[4]),
		}
		if host, portStr, found := strings.Cut(replica.SocketAddress, ":"); found {
			replica.Host = host
			replica.Port, _ = strconv.Atoi(portStr)
		} else {
			replica.Host = replica.SocketAddress
		}
		replica.Behind, replica.Status, replica.Timestamp = summarizeDataInfo(replica.DataInfo)

		replicas = append(replicas, replica)
	}

	return replicas
}

// bareKeyPattern matches unquoted keys in Memgraph's map literal output,
// e.g. {memgraph: {behind: 0, status: "ready", ts: 2}}
var bareKeyPattern = regexp.MustCompile(`([{,]\s*)([A-Za-z_][A-Za-z0-9_-]*)\s*:`)

// parseDataInfo parses the data_info column of SHOW REPLICAS.
// Returns nil for empty ({}), Null, or unparseable input — all of which mean
// the main has no confirmed replication state for the replica.
func parseDataInfo(raw string) map[string]ReplicaDBInfo {
	raw = strings.Trim(strings.TrimSpace(raw), "\"")
	if raw == "" || raw == "{}" || strings.EqualFold(raw, "null") {
		return nil
	}

	// Quote bare keys so the map literal becomes valid JSON
	normalized := bareKeyPattern.ReplaceAllString(raw, `$1"$2":`)

	var dataInfo map[string]ReplicaDBInfo
	if err := json.Unmarshal([]byte(normalized), &dataInfo); err != nil {
		return nil
	}
	if len(dataInfo) == 0 {
		return nil
	}

	return dataInfo
}

// summarizeDataInfo derives replica-level Behind/Status/Timestamp from the
// per-database data_info map. Empty data_info means replication never
// engaged, which is reported as "invalid".
func summarizeDataInfo(dataInfo map[string]ReplicaDBInfo) (behind int64, status string, timestamp int64) {
	if len(dataInfo) == 0 {
		return 0, ReplicaStatusInvalid, 0
	}

	// Worst-status-wins ordering; unknown statuses rank as invalid
	statusRank := map[string]int{
		ReplicaStatusReady:       0,
		ReplicaStatusReplicating: 1,
		ReplicaStatusRecovery:    2,
		ReplicaStatusInvalid:     3,
	}

	first := true
	status = ReplicaStatusReady
	for _, db := range dataInfo {
		if first || db.Behind > behind {
			behind = db.Behind
		}
		first = false

		if db.Timestamp > timestamp {
			timestamp = db.Timestamp
		}

		dbStatus := strings.ToLower(strings.Trim(db.Status, "\""))
		rank, known := statusRank[dbStatus]
		if !known {
			dbStatus, rank = ReplicaStatusInvalid, statusRank[ReplicaStatusInvalid]
		}
		if rank > statusRank[status] {
			status = dbStatus
		}
	}

	return behind, status, timestamp
}
