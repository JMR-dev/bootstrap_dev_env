package main

import (
	"io"
	"os"
	"testing"
)

// TestMain silences issue-log output for the entire test binary.
// Tests still exercise error paths and assert via errorCount; we just
// don't want the human-facing "[ERROR] ..." prints polluting CI logs
// (GitHub Actions auto-annotates "[ERROR]" lines as workflow errors).
func TestMain(m *testing.M) {
	issueLogWriter = io.Discard
	disableProgressTracking = true
	os.Exit(m.Run())
}
