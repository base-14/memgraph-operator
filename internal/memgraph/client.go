// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"bytes"
	"context"
	"fmt"
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

// ReplicaInfo contains information about a registered replica
type ReplicaInfo struct {
	Name   string
	Host   string
	Port   int
	Mode   string
	Status string
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

// parseShowReplicasOutput parses the output of SHOW REPLICAS command
func parseShowReplicasOutput(output string) []ReplicaInfo {
	var replicas []ReplicaInfo

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "|") && strings.Contains(line, "name") {
			continue
		}

		// Parse table row: | name | host | port | mode | status |
		if strings.HasPrefix(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 6 {
				replica := ReplicaInfo{
					Name:   strings.TrimSpace(parts[1]),
					Host:   strings.TrimSpace(parts[2]),
					Mode:   strings.TrimSpace(parts[4]),
					Status: strings.TrimSpace(parts[5]),
				}
				if replica.Name != "" && replica.Name != "name" {
					replicas = append(replicas, replica)
				}
			}
		}
	}

	return replicas
}
