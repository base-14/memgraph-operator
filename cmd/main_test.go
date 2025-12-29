// Copyright 2025 Base14. See LICENSE file for details.

package main

import (
	"testing"
)

func TestSchemeSetup(t *testing.T) {
	// Verify that the scheme is properly initialized
	if scheme == nil {
		t.Fatal("scheme should not be nil after init")
	}

	// Use scheme to ensure it's accessible
	_ = scheme

	// Check that scheme has types registered
	// The init() function should have added both client-go and memgraph types
	kinds := scheme.AllKnownTypes()
	if len(kinds) == 0 {
		t.Error("scheme should have registered types")
	}
}
