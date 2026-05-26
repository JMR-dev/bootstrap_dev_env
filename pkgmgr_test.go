package main

import (
	"testing"
	"time"
)

func TestDetectPkgMgr(t *testing.T) {
	defer resetMocks()

	// Case 1: macOS should return brew
	isMacOS = true
	if mgr := detectPkgMgr(); mgr != "brew" {
		t.Errorf("expected brew on macOS, got %q", mgr)
	}

	// Case 2: Linux with dnf
	isMacOS = false
	hasCmd = func(name string) bool {
		return name == "dnf"
	}
	if mgr := detectPkgMgr(); mgr != "dnf" {
		t.Errorf("expected dnf, got %q", mgr)
	}

	// Case 3: Linux with apt-get
	hasCmd = func(name string) bool {
		return name == "apt-get"
	}
	if mgr := detectPkgMgr(); mgr != "apt-get" {
		t.Errorf("expected apt-get, got %q", mgr)
	}

	// Case 4: Linux with pacman
	hasCmd = func(name string) bool {
		return name == "pacman"
	}
	if mgr := detectPkgMgr(); mgr != "pacman" {
		t.Errorf("expected pacman, got %q", mgr)
	}

	// Case 5: No package manager found (exits)
	hasCmd = func(name string) bool {
		return false
	}
	var exited bool
	var exitCode int
	osExit = func(code int) {
		exited = true
		exitCode = code
	}
	detectPkgMgr()
	if !exited || exitCode != 1 {
		t.Errorf("expected exit with code 1, exited=%v code=%d", exited, exitCode)
	}
}

func TestResolveSystemPkgs(t *testing.T) {
	defer resetMocks()

	pkgMgr = "apt-get"
	resolved, skipped := resolveSystemPkgs([]string{"ffmpeg-free", "lua", "podman", "docker-compose"})
	// ffmpeg-free -> ffmpeg, lua -> lua5.4, podman -> podman, docker-compose is not overridden for apt-get
	expectedResolved := []string{"ffmpeg", "lua5.4", "podman", "docker-compose"}
	if len(resolved) != len(expectedResolved) {
		t.Fatalf("expected resolved length %d, got %d", len(expectedResolved), len(resolved))
	}
	for i, r := range resolved {
		if r != expectedResolved[i] {
			t.Errorf("at index %d: expected %q, got %q", i, expectedResolved[i], r)
		}
	}
	if len(skipped) != 0 {
		t.Errorf("expected no skipped packages, got %v", skipped)
	}

	// Test skip override
	pkgMgr = "dnf"
	resolved, skipped = resolveSystemPkgs([]string{"docker-compose", "rg"})
	// docker-compose -> skipped, rg -> ripgrep
	if len(resolved) != 1 || resolved[0] != "ripgrep" {
		t.Errorf("expected resolution to [ripgrep], got %v", resolved)
	}
	if len(skipped) != 1 || skipped[0] != "docker-compose" {
		t.Errorf("expected skipped to be [docker-compose], got %v", skipped)
	}

	// brew: bashtop -> btop (replace), buildah -> skipped
	pkgMgr = "brew"
	resolved, skipped = resolveSystemPkgs([]string{"bashtop", "buildah", "rg"})
	if len(resolved) != 2 || resolved[0] != "btop" || resolved[1] != "ripgrep" {
		t.Errorf("expected [btop ripgrep], got %v", resolved)
	}
	if len(skipped) != 1 || skipped[0] != "buildah" {
		t.Errorf("expected skipped to be [buildah], got %v", skipped)
	}
}

func TestIsSystemPkgInstalled(t *testing.T) {
	defer resetMocks()

	// Case dnf: rpm -q
	pkgMgr = "dnf"
	var probeArgv []string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeArgv = argv
		return CmdResult{ExitCode: 0}, true
	}
	if !isSystemPkgInstalled("git") {
		t.Error("expected true when rpm returns 0")
	}
	if len(probeArgv) < 3 || probeArgv[0] != "rpm" || probeArgv[2] != "git" {
		t.Errorf("unexpected probe argv: %v", probeArgv)
	}

	// Case apt-get: dpkg-query
	pkgMgr = "apt-get"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeArgv = argv
		return CmdResult{ExitCode: 0, Stdout: []byte("install ok installed")}, true
	}
	if !isSystemPkgInstalled("git") {
		t.Error("expected true when dpkg-query output contains install ok installed")
	}
	if len(probeArgv) < 4 || probeArgv[0] != "dpkg-query" {
		t.Errorf("unexpected probe argv: %v", probeArgv)
	}

	// Case pacman: pacman -Qi
	pkgMgr = "pacman"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeArgv = argv
		return CmdResult{ExitCode: 0}, true
	}
	if !isSystemPkgInstalled("git") {
		t.Error("expected true when pacman returns 0")
	}

	// Case brew: brew list --formula / --cask
	pkgMgr = "brew"
	hasCmd = func(name string) bool {
		return name == "brew"
	}
	var probeCalls [][]string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCalls = append(probeCalls, argv)
		if argv[2] == "--formula" {
			return CmdResult{ExitCode: 1}, true
		}
		return CmdResult{ExitCode: 0}, true // succeeds on second call
	}
	if !isSystemPkgInstalled("git") {
		t.Error("expected true when brew list succeeds")
	}
	if len(probeCalls) != 2 {
		t.Errorf("expected 2 brew calls, got %d", len(probeCalls))
	}
}

func TestIsFlatpakInstalled(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool {
		return name == "flatpak"
	}
	var probeArgv []string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeArgv = argv
		return CmdResult{ExitCode: 0}, true
	}

	if !isFlatpakInstalled("org.gimp.GIMP") {
		t.Error("expected true")
	}
	if len(probeArgv) < 3 || probeArgv[0] != "flatpak" || probeArgv[2] != "org.gimp.GIMP" {
		t.Errorf("unexpected probe argv: %v", probeArgv)
	}
}

func TestPkgMgrEdgeCases(t *testing.T) {
	defer resetMocks()

	pkgMgr = "brew"
	hasCmd = func(name string) bool { return false }
	if isSystemPkgInstalled("git") {
		t.Error("expected false when brew is not installed")
	}

	resetMocks()
	pkgMgr = "brew"
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	if isSystemPkgInstalled("git") {
		t.Error("expected false when brew list fails")
	}

	resetMocks()
	pkgMgr = "unsupported"
	if isSystemPkgInstalled("git") {
		t.Error("expected false for unsupported package manager")
	}

	resetMocks()
	hasCmd = func(name string) bool { return false }
	if isFlatpakInstalled("org.gimp.GIMP") {
		t.Error("expected false when flatpak command not found")
	}
}

