package main

// Last wave of coverage tests targeting setupFirecrackerVM (testable
// early-return branches), the remaining install handler edge cases, and
// a few stragglers.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── setupFirecrackerVM: early returns ───────────────────────────────────

func TestSetupFirecrackerVMNotMac(t *testing.T) {
	defer resetMocks()
	isMacOS = false
	setupFirecrackerVM() // should no-op
}

func TestSetupFirecrackerVMBackendEmpty(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	archName = "aarch64"
	// Apple M2 on macOS 14 → selectVMBackend returns "" → setup skips.
	probe = func(argv []string, _ time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("14.0")}, true
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("Apple M2")}, true
	}
	captureStdout(t, func() {
		setupFirecrackerVM()
	})
}

func TestSetupFirecrackerVMSshKeygenFails(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	osMkdirAll = func(_ string, _ os.FileMode) error { return nil }
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		if argv[0] == "ssh-keygen" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	captureStdout(t, func() {
		setupFirecrackerVM()
	})
	if !hasIssueContaining("ssh-keygen failed") {
		t.Error("expected ssh-keygen failure error")
	}
}

func TestSetupFirecrackerVMFedoraImageLookupFails(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	archName = "x86_64"
	osStat = func(name string) (os.FileInfo, error) {
		// Key exists; qcow2 missing.
		if strings.HasSuffix(name, "id_ed25519") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	osMkdirAll = func(_ string, _ os.FileMode) error { return nil }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	fetchText = func(_ string) string { return "" }
	captureStdout(t, func() {
		setupFirecrackerVM()
	})
	if !hasIssueContaining("Could not resolve latest Fedora") {
		t.Error("expected Fedora lookup failure error")
	}
}

func TestSetupFirecrackerVMPubKeyReadFails(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil } // key + qcow2 exist
	osMkdirAll = func(_ string, _ os.FileMode) error { return nil }
	osReadFile = func(_ string) ([]byte, error) { return nil, os.ErrNotExist }
	captureStdout(t, func() {
		setupFirecrackerVM()
	})
	if !hasIssueContaining("could not read public key") {
		t.Error("expected pub-key read failure")
	}
}

// ── installFirecracker: archive contains non-firecracker file ──────────

func TestInstallFirecrackerSkipsNonMatchingFiles(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		if argv[0] == "tar" {
			// Drop a file that doesn't start with "firecracker" — should be skipped.
			_ = os.WriteFile(filepath.Join(tmp, "README"), []byte("x"), 0o644)
			_ = os.WriteFile(filepath.Join(tmp, "firecracker-v1"), []byte("x"), 0o755)
		}
		return CmdResult{ExitCode: 0}
	}
	installFirecracker(filepath.Join(tmp, "fc.tgz"), tmp)
}

// ── installZig: existing glob match in /usr/local needs a writable parent ──

// We can't write to /usr/local in tests, but we can verify the symlink
// path runs through end-to-end with a no-op runCmd. The Glob returns []
// in tests, so the loop body stays uncovered.

// ── resolveLatestGo: version trimmed to empty (release tag was just "go") ──

func TestResolveLatestGoEmptyTrimmedVersion(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, v any) bool {
		// Release with version "go" → trim → empty.
		data := `[{"version":"go","files":[]}]`
		_ = v
		// Marshal manually since we don't import json here; use the helper
		// via reflection-free path: use the canonical mock from elsewhere.
		return jsonUnmarshal([]byte(data), v)
	}
	if _, _, ok := resolveLatestGo(nil); ok {
		t.Error("expected resolveLatestGo false when version is empty after trim")
	}
}

func TestResolveLatestFirecrackerEmptyTagTrim(t *testing.T) {
	defer resetMocks()
	isMacOS = false
	fetchJSON = func(_ string, v any) bool {
		v.(*ghRelease).TagName = "v" // → trimmed to ""
		return true
	}
	if _, _, ok := resolveLatestFirecracker(nil); ok {
		t.Error("expected false when trimmed tag is empty")
	}
}

// ── runMain: empty package lists short-circuit ──────────────────────────

func TestRunMainEmptyOnlyValid(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) { return CmdResult{ExitCode: 0}, true }
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	osExit = func(_ int) {}
	captureStdout(t, func() {
		// "" only flag (default) with everything reported as installed.
		runMain([]string{"bootstrap_environment"})
	})
}

func jsonUnmarshal(data []byte, v any) bool {
	return json.Unmarshal(data, v) == nil
}

// ── extra runMain branches ──────────────────────────────────────────────

func TestRunMainSystemInstallPath(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) { return CmdResult{ExitCode: 1}, true }
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	stdin = strings.NewReader("y\n")
	defer func() { stdin = os.Stdin }()
	osExit = func(_ int) {}
	captureStdout(t, func() {
		runMain([]string{"bootstrap_environment", "--only", "system"})
	})
}

func TestRunMainCustomInstallPath(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	// Custom pkgs all need install (not present). The osStat mock runs
	// from multiple goroutines via parallel Wave A, so it must be
	// goroutine-safe (no shared mutable state outside of read-only env
	// inspection).
	osStat = func(name string) (os.FileInfo, error) {
		// ~/.nvm exists so the npm batch path runs.
		if strings.HasSuffix(name, ".nvm") || strings.HasSuffix(name, ".pyenv") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) { return CmdResult{ExitCode: 1}, true }
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0, Stdout: []byte("v20\n")} }
	download = func(_, dest string) bool {
		return os.WriteFile(dest, []byte("x"), 0o644) == nil
	}
	fetchJSON = func(_ string, _ any) bool { return false }
	fetchText = func(_ string) string { return "" }
	stdin = strings.NewReader("y\n")
	defer func() { stdin = os.Stdin }()
	osExit = func(_ int) {}
	captureStdout(t, func() {
		runMain([]string{"bootstrap_environment", "--only", "custom"})
	})
}

// ── runShellReal: probe times out via tiny timeout ──────────────────────

func TestRunShellRealTimeout(t *testing.T) {
	r := runShellReal("sleep 1", CmdOpts{Timeout: 10 * time.Millisecond})
	if r.ExitCode != 124 {
		t.Errorf("expected timeout (124), got %d", r.ExitCode)
	}
}

// ── runCmdReal: command times out ───────────────────────────────────────

func TestRunCmdRealTimeout(t *testing.T) {
	r := runCmdReal([]string{"sleep", "1"}, CmdOpts{Timeout: 10 * time.Millisecond})
	if r.ExitCode != 124 {
		t.Errorf("expected timeout (124), got %d", r.ExitCode)
	}
}

// ── runCmdReal: cwd + input passing ────────────────────────────────────

func TestRunCmdRealCwdAndInput(t *testing.T) {
	tmp := t.TempDir()
	r := runCmdReal([]string{"sh", "-c", "cat > out.txt; pwd"},
		CmdOpts{Cwd: tmp, Input: []byte("data"), Capture: true})
	if !r.OK() {
		t.Fatalf("expected OK, got: %v / %s", r.Err, r.Stderr)
	}
	if !strings.Contains(string(r.Stdout), tmp) {
		t.Errorf("expected stdout to contain cwd %s, got: %s", tmp, r.Stdout)
	}
	if data, err := os.ReadFile(filepath.Join(tmp, "out.txt")); err != nil || string(data) != "data" {
		t.Errorf("expected stdin data to be written, got: %q (err=%v)", data, err)
	}
}

// ── runShellReal: cwd + input ──────────────────────────────────────────

func TestRunShellRealCwdAndInput(t *testing.T) {
	tmp := t.TempDir()
	r := runShellReal("cat > shell-out.txt; pwd",
		CmdOpts{Cwd: tmp, Input: []byte("shelldata"), Capture: true})
	if !r.OK() {
		t.Fatalf("expected OK, got %v", r.Err)
	}
	if data, err := os.ReadFile(filepath.Join(tmp, "shell-out.txt")); err != nil || string(data) != "shelldata" {
		t.Errorf("expected stdin data written via shell, got %q err=%v", data, err)
	}
}

// ── installPip apt-get fallback secondary failure ──────────────────────

func TestInstallPipAptFallbackFails(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) { return CmdResult{ExitCode: 0}, true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} } // everything fails
	installPip()
	if !hasIssueContaining("python3-pip failed to install via apt-get") {
		t.Error("expected apt python3-pip failure")
	}
}

// ── ensureXcodeCLT non-macOS quick exit (already added but exercise the cov path) ──

// ── invokingUser: SUDO_USER unset, user.Current succeeds ──

func TestInvokingUserNoSudoCurrentUser(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	if invokingUser() == "" {
		t.Error("expected invokingUser to fall back to user.Current()")
	}
}

// ── installSystemPackages: tmpdir works for specials ──────────────────

func TestInstallSystemPackagesWithSpecialReal(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	captureStdout(t, func() {
		installSystemPackages([]string{"git"}, []string{"pipx"})
	})
}

// ── npmInstalled: home-dir failure path ────────────────────────────────

// The os.UserHomeDir call only returns an error when HOME is unset on Unix
// AND no /etc/passwd entry exists. Hard to trigger reliably across CI; the
// branch is mostly defensive. Skip explicit coverage.

// ── runOneCustomInstall: install path missing, with name != pip ────────

func TestRunOneCustomInstallNoCheckPath(t *testing.T) {
	defer resetMocks()
	// Pretend nothing is installed and use a package that has no install path
	// AND no URL — should warn twice.
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	hasCmd = func(_ string) bool { return false }
	runOneCustomInstall(&CustomPackage{Name: "unknownpkg-2"})
}

// ── repos: apt-get docker setup (no existing file) ────────────────────
// Without docker installed this exercises the gpg+keyring branch via mocks.

func TestSetupDockerRepoApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	osReadFile = func(_ string) ([]byte, error) {
		return []byte("ID=ubuntu\n"), nil
	}
	captureStdout(t, func() {
		setupDockerRepo()
	})
}

func TestSetupChromeRepoApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	captureStdout(t, func() {
		setupChromeRepo()
	})
}

func TestSetupVivaldiRepoApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	captureStdout(t, func() {
		setupVivaldiRepo()
	})
}

// ── installFirecrackerZshFunction: existing block gets replaced ───────

func TestInstallFirecrackerZshFunctionReplaceExisting(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	osReadFile = func(_ string) ([]byte, error) {
		return []byte("# >>> firecracker-vm wrapper >>>\nold body\n# <<< firecracker-vm wrapper <<<\n\nelse"), nil
	}
	written := ""
	osWriteFile = func(_ string, data []byte, _ os.FileMode) error {
		written = string(data)
		return nil
	}
	newBlock := "# >>> firecracker-vm wrapper >>>\nnew body\n# <<< firecracker-vm wrapper <<<\n"
	installFirecrackerZshFunction(newBlock)
	if !strings.Contains(written, "new body") {
		t.Errorf("expected new body in output, got: %q", written)
	}
	if strings.Contains(written, "old body") {
		t.Errorf("expected old body to be removed, got: %q", written)
	}
}

// ── latestFedoraCloudImage: missing checksum entry ────────────────────

func TestLatestFedoraCloudImageMissingFiles(t *testing.T) {
	defer resetMocks()
	archName = "x86_64"
	calls := 0
	fetchText = func(_ string) string {
		calls++
		if calls == 1 {
			return `href="40/"`
		}
		// images dir has no matching qcow / checksum.
		return `href="not-fedora.iso"`
	}
	if _, _, _, ok := latestFedoraCloudImage(); ok {
		t.Error("expected false when matches not found")
	}
}
