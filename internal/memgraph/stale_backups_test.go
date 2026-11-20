// Copyright 2025 Base14. See LICENSE file for details.

package memgraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCleanup(t *testing.T, dataDir string) string {
	t.Helper()
	cmd := staleBackupCleanupCommand(dataDir)
	out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("cleanup command failed: %v, output: %s", err, out)
	}
	return string(out)
}

func TestStaleBackupCleanupRemovesOnlyOldDirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "snapshots", "20261118_timestamp_1"), 4096)
	writeFile(t, filepath.Join(dir, "snapshots", ".old", "20261117_timestamp_0"), 256*1024)
	writeFile(t, filepath.Join(dir, "wal", "20261118_current"), 4096)
	writeFile(t, filepath.Join(dir, "wal", ".old", "20261117_from_0_to_1"), 64*1024)

	out := runCleanup(t, dir)

	for _, gone := range []string{"snapshots/.old", "wal/.old"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after cleanup (err=%v)", gone, err)
		}
	}
	for _, kept := range []string{"snapshots/20261118_timestamp_1", "wal/20261118_current"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was removed, want it kept: %v", kept, err)
		}
	}
	freed, err := parseFreedBytes(out)
	if err != nil {
		t.Fatalf("parseFreedBytes(%q): %v", out, err)
	}
	if freed < 320*1024 {
		t.Errorf("freed = %d bytes, want at least %d (both .old directories)", freed, 320*1024)
	}
}

func TestStaleBackupCleanupWithNothingToRemove(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "snapshots", "20261118_timestamp_1"), 4096)

	freed, err := parseFreedBytes(runCleanup(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if freed != 0 {
		t.Errorf("freed = %d, want 0 when there is no .old directory", freed)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshots", "20261118_timestamp_1")); err != nil {
		t.Errorf("current snapshot removed: %v", err)
	}
}

func TestStaleBackupCleanupWithMissingDataDir(t *testing.T) {
	freed, err := parseFreedBytes(runCleanup(t, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	if freed != 0 {
		t.Errorf("freed = %d, want 0 for a missing data directory", freed)
	}
}

func TestParseFreedBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "0\n", want: 0},
		{in: "4900000\n", want: 4900000 * 1024},
		{in: "", want: 0},
		{in: "du: cannot read\n", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFreedBytes(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseFreedBytes(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseFreedBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
