package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Issue log accumulated over the run; written to bootstrap_run.log at the end
// when there's something to report.

var (
	issuesMu   sync.Mutex
	issues     []string
	notices    []string
	errorCount int
	// issueLogWriter is the destination for human-facing issue log lines.
	// Overridden during tests to suppress intentional error-path output.
	issueLogWriter io.Writer = os.Stdout
)

func logIssue(level, msg string) {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	// Route the human-facing line through the active task's buffer when
	// running inside a parallel worker, so concurrent warns/errLogs don't
	// interleave on stdout. The structured issue (added to the slice below)
	// still flows into the global issues log used by writeRunLog.
	if t := currentTask(); t != nil {
		t.Printf("[%s] %s\n", level, msg)
	} else {
		fmt.Fprintf(issueLogWriter, "  [%s] %s\n", level, msg)
	}
	issues = append(issues, fmt.Sprintf("[%s] %s", level, msg))
	if level == "ERROR" {
		errorCount++
	}
}

func warn(msg string)   { logIssue("WARN", msg) }
func errLog(msg string) { logIssue("ERROR", msg) }

func hasErrors() bool {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	return errorCount > 0
}

func notice(msg string) {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	notices = append(notices, msg)
}

func runLogPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "bootstrap_run.log"
	}
	return filepath.Join(filepath.Dir(exe), "bootstrap_run.log")
}

func writeRunLog() {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	if len(issues) == 0 {
		fmt.Println("\nNo issues — log file not written.")
		return
	}
	path := runLogPath()
	ts := time.Now().Format("2006-01-02 15:04:05")
	lines := []string{fmt.Sprintf("# Bootstrap run — %s", ts), ""}
	lines = append(lines, issues...)
	content := strings.Join(lines, "\n") + "\n"
	if err := osWriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write run log: %v\n", err)
		return
	}
	fmt.Printf("\n%d issue(s) logged to: %s\n", len(issues), path)
}

func printNotices() {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	if len(notices) == 0 {
		return
	}
	fmt.Println("\nNotices:")
	for _, n := range notices {
		fmt.Printf("  • %s\n", n)
	}
}
