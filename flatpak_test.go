package main

import (
	"strings"
	"testing"
)

func TestInstallFlatpakPackages(t *testing.T) {
	defer resetMocks()

	// Case 1: flatpak already installed
	hasCmd = func(name string) bool {
		return name == "flatpak"
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	installFlatpakPackages([]string{"org.gimp.GIMP"})

	if len(runCmdCalls) != 2 {
		t.Fatalf("expected 2 runCmd calls, got %d: %v", len(runCmdCalls), runCmdCalls)
	}

	// First call should add flathub remote
	if runCmdCalls[0][1] != "remote-add" {
		t.Errorf("expected remote-add, got %v", runCmdCalls[0])
	}
	// Second call should install GIMP
	if runCmdCalls[1][1] != "install" || runCmdCalls[1][4] != "org.gimp.GIMP" {
		t.Errorf("expected install GIMP, got %v", runCmdCalls[1])
	}

	// Case 2: flatpak not installed, choose not to install
	resetMocks()
	var askedPrompt string
	stdin = strings.NewReader("n\n") // Abort flatpak installation
	hasCmd = func(name string) bool {
		return false
	}
	runCmdCalls = nil
	installFlatpakPackages([]string{"org.gimp.GIMP"})
	if len(runCmdCalls) != 0 {
		t.Errorf("expected no flatpak installs if skipped, got calls: %v", runCmdCalls)
	}
	_ = askedPrompt

	// Case 3: flatpak not installed, choose to install
	resetMocks()
	pkgMgr = "dnf"
	stdin = strings.NewReader("y\n")
	flatpakInstalled := false
	hasCmd = func(name string) bool {
		if name == "flatpak" {
			return flatpakInstalled
		}
		return false
	}
	var runCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCalls = append(runCalls, argv)
		if len(argv) >= 4 && argv[0] == "dnf" && argv[1] == "install" && argv[3] == "flatpak" {
			flatpakInstalled = true
		}
		return CmdResult{ExitCode: 0}
	}
	installFlatpakPackages([]string{"org.gimp.GIMP"})
	// It should call dnf install flatpak, then remote-add, then install GIMP
	foundInstall := false
	for _, call := range runCalls {
		if len(call) >= 4 && call[0] == "dnf" && call[1] == "install" && call[3] == "flatpak" {
			foundInstall = true
		}
	}
	if !foundInstall {
		t.Errorf("expected dnf install flatpak to be called, got calls: %v", runCalls)
	}
}

func TestInstallFlatpakFailures(t *testing.T) {
	defer resetMocks()

	stdin = strings.NewReader("y\n")
	hasCmd = func(name string) bool { return false }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	installFlatpakPackages([]string{"org.gimp.GIMP"})

	resetMocks()
	stdin = strings.NewReader("y\n")
	hasCmd = func(name string) bool { return false }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	installFlatpakPackages([]string{"org.gimp.GIMP"})

	resetMocks()
	hasCmd = func(name string) bool { return name == "flatpak" }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[1] == "install" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installFlatpakPackages([]string{"org.gimp.GIMP"})
}

