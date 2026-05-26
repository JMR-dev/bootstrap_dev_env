package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestIssuesLogging(t *testing.T) {
	defer resetMocks()
	resetMocks()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	warn("something is deprecated")
	errLog("something failed")
	notice("please restart shell")

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "[WARN] something is deprecated") {
		t.Errorf("stdout missing warning: %q", output)
	}
	if !strings.Contains(output, "[ERROR] something failed") {
		t.Errorf("stdout missing error: %q", output)
	}

	issuesMu.Lock()
	issueLen := len(issues)
	noticeLen := len(notices)
	issuesMu.Unlock()

	if issueLen != 2 {
		t.Errorf("expected 2 logged issues, got %d", issueLen)
	}
	if noticeLen != 1 {
		t.Errorf("expected 1 notice, got %d", noticeLen)
	}
}

func TestWriteRunLog(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	_ = tmp

	// Redirect path function or mock executable path
	// In issues.go, we can define a package variable to override runLogPath if we want,
	// or we can mock osWriteFile. Since we mocked osWriteFile, let's use that!
	var writtenPath string
	var writtenData []byte
	osWriteFile = func(path string, data []byte, perm os.FileMode) error {
		writtenPath = path
		writtenData = data
		return nil
	}

	// No issues case
	writeRunLog()
	if writtenPath != "" {
		t.Error("expected run log not to be written when there are no issues")
	}

	// Add an issue
	warn("test warning")
	writeRunLog()

	if writtenPath == "" {
		t.Fatal("expected run log to be written")
	}
	if !strings.Contains(string(writtenData), "[WARN] test warning") {
		t.Errorf("expected log to contain the warning, got: %s", string(writtenData))
	}
}

func TestPrintNotices(t *testing.T) {
	defer resetMocks()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printNotices() // Should be empty

	notice("first notice")
	notice("second notice")
	printNotices()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Notices:") {
		t.Error("stdout missing notices header")
	}
	if !strings.Contains(output, "first notice") || !strings.Contains(output, "second notice") {
		t.Errorf("stdout missing notice contents: %q", output)
	}
}
