package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunMainInvalidOnly(t *testing.T) {
	defer resetMocks()

	var exited bool
	var exitCode int
	osExit = func(code int) {
		exited = true
		exitCode = code
	}

	runMain([]string{"bootstrap_environment", "--only", "invalid"})

	if !exited {
		t.Error("expected runMain with invalid --only value to exit")
	}
	if exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", exitCode)
	}
}

func TestRunMainAllInstalled(t *testing.T) {
	defer resetMocks()

	// All packages are already installed (total packages to install = 0)
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // all custom paths exist
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "rpm" || argv[0] == "dpkg-query" || argv[0] == "pacman" {
			return CmdResult{ExitCode: 0, Stdout: []byte("install ok installed")}, true
		}
		if argv[0] == "flatpak" {
			return CmdResult{ExitCode: 0}, true
		}
		return CmdResult{ExitCode: 0}, true
	}
	hasCmd = func(name string) bool {
		return true
	}

	var exited bool
	osExit = func(code int) {
		exited = true
	}

	// We only run custom to keep it short & avoid other logic dependencies
	runMain([]string{"bootstrap_environment", "--only", "custom"})

	if exited {
		t.Error("expected program to complete successfully without exiting")
	}
}

func TestRunMainInstallAbort(t *testing.T) {
	defer resetMocks()

	// Simulating some packages to install, but user selects N
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist // custom paths missing -> need install
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true // packages not installed
	}
	hasCmd = func(name string) bool {
		return true
	}

	// Mock stdin to say "n" to abort the prompt "Proceed? [y/N]"
	stdin = strings.NewReader("n\n")

	var exited bool
	var exitCode int
	osExit = func(code int) {
		exited = true
		exitCode = code
	}

	runMain([]string{"bootstrap_environment", "--only", "custom"})

	if !exited {
		t.Error("expected program to exit on user abort")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestRunMainMacos(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	pkgMgr = "brew"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // brew etc exist
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	hasCmd = func(name string) bool {
		return true
	}
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

	// Run with --only custom, --no-vm, --no-ai
	runMain([]string{"bootstrap_environment", "--only", "custom", "--no-vm", "--no-ai"})

	if exited {
		t.Error("expected program to complete successfully on macOS mock run")
	}
}
