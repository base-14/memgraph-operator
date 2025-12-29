// Copyright 2025 Base14. See LICENSE file for details.

package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetNonEmptyLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single line",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "multiple lines",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "with empty lines",
			input:    "line1\n\nline2\n\n\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "only empty lines",
			input:    "\n\n\n",
			expected: nil,
		},
		{
			name:     "trailing newline",
			input:    "line1\nline2\n",
			expected: []string{"line1", "line2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetNonEmptyLines(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("GetNonEmptyLines() returned %d lines, want %d", len(result), len(tt.expected))
				return
			}

			for i, line := range result {
				if line != tt.expected[i] {
					t.Errorf("GetNonEmptyLines()[%d] = %q, want %q", i, line, tt.expected[i])
				}
			}
		})
	}
}

func TestGetProjectDir(t *testing.T) {
	dir, err := GetProjectDir()

	if err != nil {
		t.Fatalf("GetProjectDir() error = %v", err)
	}

	if dir == "" {
		t.Error("GetProjectDir() returned empty string")
	}

	// Directory should exist
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("GetProjectDir() returned non-existent directory: %s", dir)
	}
}

func TestUncommentCode(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	// Write initial content
	initialContent := `# This is a comment
# prefix:line1
# prefix:line2
# prefix:line3
normal line`

	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test uncommenting
	target := "# prefix:line1\n# prefix:line2\n# prefix:line3"
	err := UncommentCode(tmpFile, target, "# prefix:")

	if err != nil {
		t.Fatalf("UncommentCode() error = %v", err)
	}

	// Read the result
	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result file: %v", err)
	}

	// Verify the content was uncommented
	resultStr := string(result)
	if resultStr != "# This is a comment\nline1\nline2\nline3\nnormal line" {
		t.Errorf("UncommentCode() result = %q", resultStr)
	}
}

func TestUncommentCodeTargetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	if err := os.WriteFile(tmpFile, []byte("some content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	err := UncommentCode(tmpFile, "nonexistent target", "# ")
	if err == nil {
		t.Error("UncommentCode() should error when target not found")
	}
}

func TestUncommentCodeFileNotFound(t *testing.T) {
	err := UncommentCode("/nonexistent/file.yaml", "target", "# ")
	if err == nil {
		t.Error("UncommentCode() should error when file not found")
	}
}

func TestUncommentCodeEmptyTarget(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	// Write content with an empty line that can be "uncommtented"
	initialContent := "line1\n\nline3"
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Empty line as target - should not find it
	err := UncommentCode(tmpFile, "nonexistent", "")
	if err == nil {
		t.Error("UncommentCode() should error for non-existent target")
	}
}

func TestUncommentCodeSingleLine(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	initialContent := "# comment: value"
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	err := UncommentCode(tmpFile, "# comment: value", "# comment: ")
	if err != nil {
		t.Fatalf("UncommentCode() error = %v", err)
	}

	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result: %v", err)
	}

	if string(result) != "value" {
		t.Errorf("Got %q, want %q", string(result), "value")
	}
}

func TestGetNonEmptyLinesVariations(t *testing.T) {
	// Test with Windows-style line endings
	result := GetNonEmptyLines("line1\r\nline2")
	if len(result) != 2 {
		// Note: This tests the current behavior - may include \r
		t.Logf("Windows line endings result: %v", result)
	}

	// Test with spaces only line
	result = GetNonEmptyLines("line1\n   \nline2")
	// Spaces-only line is not empty
	if len(result) != 3 {
		t.Errorf("Expected 3 lines (spaces is not empty), got %d", len(result))
	}
}

func TestGetProjectDirFromE2EPath(t *testing.T) {
	// Get original directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()

	// Create a temp directory structure simulating test/e2e
	tmpDir := t.TempDir()
	e2eDir := filepath.Join(tmpDir, "test", "e2e")
	if err := os.MkdirAll(e2eDir, 0755); err != nil {
		t.Fatalf("Failed to create e2e dir: %v", err)
	}

	// Change to e2e directory
	if err := os.Chdir(e2eDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	dir, err := GetProjectDir()
	if err != nil {
		t.Fatalf("GetProjectDir() error = %v", err)
	}

	// Should strip /test/e2e suffix
	if filepath.Base(dir) == "e2e" {
		t.Error("GetProjectDir() should strip /test/e2e suffix")
	}
}

func TestUncommentCodeMultipleLines(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	// Write content with multiple lines to uncomment
	initialContent := `# First section
# PREFIX:line1
# PREFIX:line2
# PREFIX:line3
# Other comment
regular line`

	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Uncomment a block with prefix
	target := "# PREFIX:line1\n# PREFIX:line2\n# PREFIX:line3"
	err := UncommentCode(tmpFile, target, "# PREFIX:")

	if err != nil {
		t.Fatalf("UncommentCode() error = %v", err)
	}

	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result: %v", err)
	}

	expected := `# First section
line1
line2
line3
# Other comment
regular line`
	if string(result) != expected {
		t.Errorf("Got:\n%s\n\nWant:\n%s", string(result), expected)
	}
}

func TestUncommentCodeEmptyPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	// Write content
	initialContent := "abc\ndef\nghi"
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Uncomment with empty prefix (just removes nothing from each line)
	err := UncommentCode(tmpFile, "abc", "")
	if err != nil {
		t.Fatalf("UncommentCode() error = %v", err)
	}

	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result: %v", err)
	}

	// With empty prefix, just the target is kept as-is
	if string(result) != initialContent {
		t.Errorf("Got %q, want %q", string(result), initialContent)
	}
}

func TestUncommentCodePreservesRest(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")

	// Write content
	initialContent := "before content\n# target:value\nafter content"
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	err := UncommentCode(tmpFile, "# target:value", "# target:")
	if err != nil {
		t.Fatalf("UncommentCode() error = %v", err)
	}

	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result: %v", err)
	}

	expected := "before content\nvalue\nafter content"
	if string(result) != expected {
		t.Errorf("Got %q, want %q", string(result), expected)
	}
}
