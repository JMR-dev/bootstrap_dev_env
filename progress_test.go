package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadWriteProgressStep(t *testing.T) {
	defer resetMocks()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "progress.config")

	// Set up mock file operations
	var fileContents []byte
	osReadFile = func(name string) ([]byte, error) {
		if name == configPath {
			if fileContents == nil {
				return nil, os.ErrNotExist
			}
			return fileContents, nil
		}
		return nil, os.ErrNotExist
	}
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if name == configPath {
			fileContents = data
			return nil
		}
		return fmt.Errorf("unexpected file write to %s", name)
	}

	// Override progressConfigPath
	originalPathFunc := progressConfigPath
	progressConfigPath = func() string {
		return configPath
	}
	defer func() {
		progressConfigPath = originalPathFunc
	}()

	// 1. Initial state (no file)
	disableProgressTracking = false
	step := readProgressStep()
	if step != 0 {
		t.Errorf("expected step 0 initially, got %d", step)
	}

	// 2. Write step 1
	err := writeProgressStep(1)
	if err != nil {
		t.Fatalf("failed to write step 1: %v", err)
	}
	step = readProgressStep()
	if step != 1 {
		t.Errorf("expected step 1 after writing, got %d", step)
	}

	// 3. Write step 2
	err = writeProgressStep(2)
	if err != nil {
		t.Fatalf("failed to write step 2: %v", err)
	}
	step = readProgressStep()
	if step != 2 {
		t.Errorf("expected step 2 after writing, got %d", step)
	}
}

func TestRunMainStep2Exit(t *testing.T) {
	defer resetMocks()

	// Mock file read to return step=2
	osReadFile = func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "progress.config") {
			return []byte("step=2\n"), nil
		}
		return nil, os.ErrNotExist
	}

	disableProgressTracking = false

	var exited bool
	osExit = func(code int) {
		exited = true
	}

	runMain([]string{"bootstrap_environment"})

	if exited {
		t.Error("expected runMain to return normally rather than exit when step=2")
	}
}

func TestRunMainStep0OnlyZshGitCurl(t *testing.T) {
	defer resetMocks()

	// Mock file read to return nothing (step=0)
	var writtenData []byte
	osReadFile = func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "progress.config") {
			return nil, os.ErrNotExist
		}
		if strings.HasSuffix(name, "/etc/passwd") {
			return []byte(fmt.Sprintf("%s:x:1000:1000::/home/user:/bin/bash\n", os.Getenv("USER"))), nil
		}
		return nil, os.ErrNotExist
	}
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, "progress.config") {
			writtenData = data
			return nil
		}
		return nil
	}

	hasCmd = func(name string) bool {
		return name == "zsh" || name == "git" || name == "curl"
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "which" && argv[1] == "zsh" {
			return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh\n")}, true
		}
		return CmdResult{ExitCode: 1}, true
	}

	disableProgressTracking = false

	// Mock user selection: Accept
	stdin = strings.NewReader("y\n")

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	var runShellCalls []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCalls = append(runShellCalls, cmd)
		return CmdResult{ExitCode: 0}
	}

	var exited bool
	osExit = func(code int) {
		exited = true
	}

	runMain([]string{"bootstrap_environment"})

	if !exited {
		t.Error("expected runMain to exit at step 1")
	}

	// Verify step 1 was written to progress.config
	if !strings.Contains(string(writtenData), "step=1") {
		t.Errorf("expected step=1 written to progress.config, got: %q", string(writtenData))
	}
}

func TestRunMainStep0TransitionToStep2(t *testing.T) {
	defer resetMocks()

	// Zsh default is true, oh-my-zsh and zsh/git/curl already installed
	// Under step=0, we should transition directly to step=1 and then execute step 2 in the same run.
	var writtenData []byte
	osReadFile = func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "progress.config") {
			return nil, os.ErrNotExist
		}
		if strings.HasSuffix(name, "/etc/passwd") {
			// passwd already says zsh is default shell
			return []byte(fmt.Sprintf("%s:x:1000:1000::/home/user:/bin/zsh\n", os.Getenv("USER"))), nil
		}
		return nil, os.ErrNotExist
	}
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, "progress.config") {
			writtenData = data
			return nil
		}
		return nil
	}

	hasCmd = func(name string) bool {
		return true // all installed
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "which" && argv[1] == "zsh" {
			return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh\n")}, true
		}
		if len(argv) >= 3 && argv[1] == "install" && argv[2] == "--list" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.10.0\n  3.11.0\n")}, true
		}
		return CmdResult{ExitCode: 0}, true
	}

	disableProgressTracking = false

	// Mock user selection: Accept
	stdin = strings.NewReader("y\n")

	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}

	var exited bool
	osExit = func(code int) {
		exited = true
	}

	runMain([]string{"bootstrap_environment", "--only", "custom"})

	// Since they are already installed, it will continue.
	// Since we mock all installed, total items to install for Step 2 is 0.
	// It should exit normally or print all packages installed.
	if exited {
		t.Errorf("did not expect exit during transition path. Issues: %v", issues)
	}

	// Should have written step 1, then eventually step 2
	if !strings.Contains(string(writtenData), "step=2") {
		t.Errorf("expected step=2 written to progress.config, got: %q", string(writtenData))
	}
}
