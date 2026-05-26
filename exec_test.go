package main

import (
	"strings"
	"testing"
	"time"
)

func TestCmdResultOK(t *testing.T) {
	res1 := CmdResult{ExitCode: 0, Err: nil}
	if !res1.OK() {
		t.Error("expected ExitCode 0 and Err nil to be OK")
	}

	res2 := CmdResult{ExitCode: 1, Err: nil}
	if res2.OK() {
		t.Error("expected ExitCode 1 to NOT be OK")
	}
}

func TestRunCmdReal(t *testing.T) {
	// Simple echo check
	res := runCmdReal([]string{"echo", "hello world"}, CmdOpts{Capture: true})
	if !res.OK() {
		t.Errorf("expected OK command, got result: %+v", res)
	}
	out := strings.TrimSpace(string(res.Stdout))
	if out != "hello world" {
		t.Errorf("expected 'hello world', got %q", out)
	}

	// Exit failure check
	resFail := runCmdReal([]string{"false"}, CmdOpts{Capture: true})
	if resFail.OK() {
		t.Error("expected false command to fail")
	}
	if resFail.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", resFail.ExitCode)
	}

	// Timeout check (using a short timeout)
	resTimeout := runCmdReal([]string{"sleep", "5"}, CmdOpts{Timeout: 10 * time.Millisecond, Capture: true})
	if resTimeout.ExitCode != 124 {
		t.Errorf("expected exit code 124 (timeout), got %d", resTimeout.ExitCode)
	}
}

func TestRunShellReal(t *testing.T) {
	// Simple shell command
	res := runShellReal("echo shell hello", CmdOpts{Capture: true})
	if !res.OK() {
		t.Errorf("expected OK shell command, got result: %+v", res)
	}
	out := strings.TrimSpace(string(res.Stdout))
	if out != "shell hello" {
		t.Errorf("expected 'shell hello', got %q", out)
	}

	// Failed command
	resFail := runShellReal("exit 42", CmdOpts{Capture: true})
	if resFail.OK() {
		t.Error("expected shell command with exit 42 to fail")
	}
	if resFail.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", resFail.ExitCode)
	}
}

func TestHasCmdReal(t *testing.T) {
	if !hasCmdReal("go") {
		t.Error("expected hasCmdReal('go') to return true (running go test)")
	}
	if hasCmdReal("nonexistent-command-xyz") {
		t.Error("expected nonexistent command to return false")
	}
}

func TestProbeReal(t *testing.T) {
	res, ok := probeReal([]string{"echo", "probe"}, 0)
	if !ok {
		t.Error("expected probe to succeed")
	}
	if strings.TrimSpace(string(res.Stdout)) != "probe" {
		t.Errorf("expected 'probe', got %q", string(res.Stdout))
	}

	// Test a failing command probe
	resFail, okFail := probeReal([]string{"false"}, 0)
	if !okFail {
		t.Error("expected probe check of false command to return ok=true (meaning it completed and didn't crash/timeout)")
	}
	if resFail.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", resFail.ExitCode)
	}

	// Test an invalid command (launch failure)
	_, okErr := probeReal([]string{"nonexistent-executable-file"}, 0)
	if okErr {
		t.Error("expected probe to return false on launch failure")
	}
}

func TestExecRealEdgeCases(t *testing.T) {
	// 2. Cwd and Input in runCmdReal
	tmp := t.TempDir()
	resCwd := runCmdReal([]string{"pwd"}, CmdOpts{Cwd: tmp, Capture: true})
	if !resCwd.OK() {
		t.Errorf("pwd failed: %+v", resCwd)
	}

	resInput := runCmdReal([]string{"cat"}, CmdOpts{Input: []byte("my-input"), Capture: true})
	if !resInput.OK() || strings.TrimSpace(string(resInput.Stdout)) != "my-input" {
		t.Errorf("cat input failed, got result: %+v", resInput)
	}

	// 3. Capture = false in runCmdReal
	_ = runCmdReal([]string{"echo", "capture-false-cmd"}, CmdOpts{Capture: false})

	// 4. Launch failure in runCmdReal (not exit error)
	resLaunch := runCmdReal([]string{"nonexistent-command-12345"}, CmdOpts{Capture: true})
	if resLaunch.OK() || resLaunch.ExitCode != 1 || resLaunch.Err == nil {
		t.Errorf("expected launch failure, got: %+v", resLaunch)
	}

	// 5. Cwd and Input in runShellReal
	resShellCwd := runShellReal("pwd", CmdOpts{Cwd: tmp, Capture: true})
	if !resShellCwd.OK() {
		t.Errorf("shell pwd failed: %+v", resShellCwd)
	}

	resShellInput := runShellReal("cat", CmdOpts{Input: []byte("my-shell-input"), Capture: true})
	if !resShellInput.OK() || strings.TrimSpace(string(resShellInput.Stdout)) != "my-shell-input" {
		t.Errorf("shell cat input failed, got: %+v", resShellInput)
	}

	// 6. Capture = false in runShellReal
	_ = runShellReal("echo capture-false-shell", CmdOpts{Capture: false})

	// 7. Timeout in runShellReal
	resShellTimeout := runShellReal("sleep 5", CmdOpts{Timeout: 10 * time.Millisecond, Capture: true})
	if resShellTimeout.ExitCode != 124 {
		t.Errorf("expected exit code 124 for shell timeout, got %d", resShellTimeout.ExitCode)
	}

	// 8. Timeout in probeReal
	_, okProbeTimeout := probeReal([]string{"sleep", "5"}, 10*time.Millisecond)
	if okProbeTimeout {
		t.Error("expected probe to return ok=false on timeout")
	}
}

