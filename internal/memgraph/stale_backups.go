// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const staleBackupCleanupScript = `cd "$1" 2>/dev/null || { echo 0; exit 0; }
freed=0
for d in snapshots/.old wal/.old; do
  if [ -d "$d" ]; then
    kib=$(du -sk "$d" | cut -f1)
    rm -rf "$d"
    freed=$((freed + kib))
  fi
done
echo "$freed"`

func staleBackupCleanupCommand(dataDir string) []string {
	return []string{"sh", "-c", staleBackupCleanupScript, "sh", dataDir}
}

func parseFreedBytes(output string) (int64, error) {
	s := strings.TrimSpace(output)
	if s == "" {
		return 0, nil
	}
	kib, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unexpected cleanup output %q: %w", s, err)
	}
	return kib * 1024, nil
}

// RemoveStaleBackups deletes the snapshots/.old and wal/.old directories that
// Memgraph leaves behind when a replica recovers from the main's snapshot, and
// returns the number of bytes freed.
func (c *Client) RemoveStaleBackups(ctx context.Context, namespace, podName, dataDir string) (int64, error) {
	stdout, stderr, err := c.execInPod(ctx, namespace, podName, "memgraph", staleBackupCleanupCommand(dataDir), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to remove stale backups: %w, stderr: %s", err, stderr)
	}
	return parseFreedBytes(stdout)
}
