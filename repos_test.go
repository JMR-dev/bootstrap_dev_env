package main

import (
	"os"
	"strings"
	"testing"
)

func TestRepoFileExists(t *testing.T) {
	defer resetMocks()

	var statPath string
	osStat = func(name string) (os.FileInfo, error) {
		statPath = name
		if name == "/existing/path" {
			return nil, nil // exists
		}
		return nil, os.ErrNotExist
	}

	if !repoFileExists("/nonexistent", "/existing/path") {
		t.Error("expected true when at least one path exists")
	}
	if statPath != "/existing/path" {
		t.Errorf("expected statPath to check existing, got %q", statPath)
	}

	if repoFileExists("/nonexistent1", "/nonexistent2") {
		t.Error("expected false when no paths exist")
	}
}

func TestWriteDNFRepo(t *testing.T) {
	defer resetMocks()

	var runArgv []string
	var runInput []byte
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runArgv = argv
		runInput = opts.Input
		return CmdResult{ExitCode: 0}
	}

	writeDNFRepo("adoptium", "Adoptium", "http://baseurl", "http://gpgkey")

	if len(runArgv) < 2 || runArgv[0] != "tee" || runArgv[1] != "/etc/yum.repos.d/adoptium.repo" {
		t.Errorf("unexpected run command: %v", runArgv)
	}
	content := string(runInput)
	if !strings.Contains(content, "[adoptium]") || !strings.Contains(content, "gpgkey=http://gpgkey") {
		t.Errorf("unexpected repo file content: %s", content)
	}
}

func TestSetupDockerRepo(t *testing.T) {
	defer resetMocks()

	// Case 1: DNF
	pkgMgr = "dnf"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	var runArgv []string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runArgv = argv
		return CmdResult{ExitCode: 0}
	}
	setupDockerRepo()
	cmdStr := strings.Join(runArgv, " ")
	if runArgv[0] != "dnf" || !strings.Contains(cmdStr, "docker-ce.repo") {
		t.Errorf("unexpected run command for DNF Docker setup: %v", runArgv)
	}

	// Case 2: APT
	pkgMgr = "apt-get"
	var runShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmds = append(runShellCmds, cmd)
		if strings.Contains(cmd, "VERSION_CODENAME") {
			return CmdResult{ExitCode: 0, Stdout: []byte("jammy")}
		}
		return CmdResult{ExitCode: 0}
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runArgv = argv
		return CmdResult{ExitCode: 0}
	}
	archName = "x86_64"
	setupDockerRepo()

	if len(runShellCmds) < 2 {
		t.Fatalf("expected at least 2 shell commands, got %v", runShellCmds)
	}
	if !strings.Contains(runShellCmds[0], "docker.gpg") {
		t.Errorf("expected GPG key command, got %q", runShellCmds[0])
	}
}

func TestSetupGHRepo(t *testing.T) {
	defer resetMocks()

	// Case 1: DNF
	pkgMgr = "dnf"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	var runArgv []string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runArgv = argv
		return CmdResult{ExitCode: 0}
	}
	setupGHRepo()
	cmdStr := strings.Join(runArgv, " ")
	if runArgv[0] != "dnf" || !strings.Contains(cmdStr, "gh-cli.repo") {
		t.Errorf("unexpected run command for DNF GH setup: %v", runArgv)
	}
}

func TestSetupChromeRepo(t *testing.T) {
	defer resetMocks()

	pkgMgr = "dnf"
	archName = "aarch64" // Chrome doesn't support aarch64 on linux, should warn and skip
	var warned bool
	issuesMu.Lock()
	issues = nil
	issuesMu.Unlock()
	setupChromeRepo()
	issuesMu.Lock()
	for _, iss := range issues {
		if strings.Contains(iss, "Google Chrome has no Linux build") {
			warned = true
		}
	}
	issuesMu.Unlock()
	if !warned {
		t.Error("expected warning for aarch64 Linux Chrome setup")
	}
}

func TestSetupVivaldiRepo(t *testing.T) {
	defer resetMocks()

	pkgMgr = "dnf"
	archName = "aarch64" // Vivaldi doesn't support aarch64 on linux, should warn and skip
	var warned bool
	setupVivaldiRepo()
	issuesMu.Lock()
	for _, iss := range issues {
		if strings.Contains(iss, "Vivaldi repo on this arch is not supported") {
			warned = true
		}
	}
	issuesMu.Unlock()
	if !warned {
		t.Error("expected warning for aarch64 Linux Vivaldi setup")
	}
}

func TestSetupTemurinRepo(t *testing.T) {
	defer resetMocks()

	pkgMgr = "dnf"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	var runArgv []string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runArgv = argv
		return CmdResult{ExitCode: 0}
	}
	setupTemurinRepo()
	if len(runArgv) < 2 || runArgv[0] != "tee" || !strings.Contains(runArgv[1], "Adoptium.repo") {
		t.Errorf("unexpected run command for Temurin setup: %v", runArgv)
	}
}

func TestSetupDotnetRepo(t *testing.T) {
	defer resetMocks()

	pkgMgr = "apt-get"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}
	setupDotnetRepo()
	if !strings.Contains(runShellCmd, "packages-microsoft-prod.deb") {
		t.Errorf("expected wget/dpkg call for Dotnet, got %q", runShellCmd)
	}
}

func TestRepoGroups(t *testing.T) {
	groups := repoGroups()
	if len(groups) != 6 {
		t.Errorf("expected 6 repo groups, got %d", len(groups))
	}
}

func TestReposAdditionalEdgeCases(t *testing.T) {
	defer resetMocks()

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	
	pkgMgr = "dnf"
	setupGHRepo()
	
	archName = "x86_64"
	setupChromeRepo()
	setupVivaldiRepo()
	setupTemurinRepo()

	pkgMgr = "apt-get"
	setupGHRepo()
	setupChromeRepo()
	setupVivaldiRepo()
	setupTemurinRepo()
	setupDotnetRepo()

	pkgMgr = "dnf"
	setupDotnetRepo()

	resetMocks()
	pkgMgr = "apt-get"
	archName = "x86_64"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	var shellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		shellCmds = append(shellCmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	var cmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		cmdCalls = append(cmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	setupGHRepo()
	setupChromeRepo()
	setupVivaldiRepo()
	setupTemurinRepo()

	if len(shellCmds) < 4 {
		t.Errorf("expected at least 4 shell commands for APT setup, got %d", len(shellCmds))
	}
}

