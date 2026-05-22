package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Issue log accumulated over the run; written to bootstrap_run.log at the end
// when there's something to report.

var (
	issuesMu sync.Mutex
	issues   []string
	notices  []string
)

func logIssue(level, msg string) {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	fmt.Printf("  [%s] %s\n", level, msg)
	issues = append(issues, fmt.Sprintf("[%s] %s", level, msg))
}

func warn(msg string)   { logIssue("WARN", msg) }
func errLog(msg string) { logIssue("ERROR", msg) }

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
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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
