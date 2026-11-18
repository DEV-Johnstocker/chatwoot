package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// buildRelease names this build for Langfuse traces: "build-" and the first
// 8 hex characters of the SHA-256 of the running program. The Docker build
// has no git history, so this stands in for the commit; it changes with
// every deploy and is logged at startup, so a release maps to a deploy.
// "" when the program file cannot be read.
// Issue: DEV-Johnstocker/chatwoot#1
func buildRelease() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return "build-" + hex.EncodeToString(h.Sum(nil))[:8]
}
