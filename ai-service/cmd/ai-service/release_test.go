package main

import (
	"strings"
	"testing"
)

func TestBuildRelease_NamesTheRunningProgram(t *testing.T) {
	// Arrange / Act
	a, b := buildRelease(), buildRelease()

	// Assert
	if !strings.HasPrefix(a, "build-") || len(a) != len("build-")+8 || a != b {
		t.Errorf("release = %q then %q, want one stable build-<8 hex>", a, b)
	}
}
