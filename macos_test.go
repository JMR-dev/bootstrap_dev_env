package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrewPrefix(t *testing.T) {
	defer resetMocks()

	archName = "aarch64"
	if brewPrefix() != "/opt/homebrew" {
		t.Errorf("expected /opt/homebrew on Apple Silicon, got %q", brewPrefix())
	}

	archName = "x86_64"
	if brewPrefix() != "/usr/local" {
		t.Errorf("expected /usr/local on Intel, got %q", brewPrefix())
	}
}

func TestEnsureXcodeCLT(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	var probeCalls [][]string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCalls = append(probeCalls, argv)
		// First call returns exit code 1 (not installed), then subsequent calls return 0 (installed)
		if len(probeCalls) == 1 {
			return CmdResult{ExitCode: 1}, true
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("/Library/Developer/CommandLineTools")}, true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	ensureXcodeCLT()

	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "xcode-select" || runCmdCalls[0][1] != "--install" {
		t.Errorf("expected xcode-select --install call, got: %v", runCmdCalls)
	}
	if len(probeCalls) < 2 {
		t.Errorf("expected at least 2 probe checks, got %d", len(probeCalls))
	}
}

func TestEnsureHomebrew(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	hasCmd = func(name string) bool {
		return false // Not installed
	}

	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}

	osStat = func(name string) (os.FileInfo, error) {
		// Mock homebrew path check returning exists
		if strings.HasSuffix(name, "brew") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	ensureHomebrew()

	if !strings.Contains(runShellCmd, "Homebrew/install/HEAD/install.sh") {
		t.Errorf("unexpected installer shell command: %q", runShellCmd)
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "bash" || !strings.Contains(runCmdCalls[0][2], "shellenv") {
		t.Errorf("expected shellenv zprofile command, got: %v", runCmdCalls)
	}
}

func TestMacosMajorAndSiliconGen(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("15.0.1\n")}, true
		}
		if argv[0] == "sysctl" {
			return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3 Max\n")}, true
		}
		return CmdResult{ExitCode: 1}, true
	}

	if macosMajor() != 15 {
		t.Errorf("expected macOS major version 15, got %d", macosMajor())
	}

	archName = "aarch64"
	if appleSiliconGeneration() != 3 {
		t.Errorf("expected Apple Silicon generation M3 (3), got %d", appleSiliconGeneration())
	}
}

func TestSelectVMBackend(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	archName = "x86_64"
	if backend := selectVMBackend(); backend != "virtualbox" {
		t.Errorf("expected virtualbox on Intel Mac, got %q", backend)
	}

	// Apple Silicon M3 on macOS 15 Sequoia
	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true
		}
		if argv[0] == "sysctl" {
			return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true
		}
		return CmdResult{ExitCode: 1}, true
	}
	if backend := selectVMBackend(); backend != "qemu" {
		t.Errorf("expected qemu on M3 macOS 15, got %q", backend)
	}

	// Apple Silicon M1 on macOS 14 (no local hypervisor)
	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("14.5\n")}, true
		}
		if argv[0] == "sysctl" {
			return CmdResult{ExitCode: 0, Stdout: []byte("Apple M1\n")}, true
		}
		return CmdResult{ExitCode: 1}, true
	}
	if backend := selectVMBackend(); backend != "" {
		t.Errorf("expected empty backend on M1 macOS 14, got %q", backend)
	}
}

func TestLatestFedoraCloudImage(t *testing.T) {
	defer resetMocks()

	fetchText = func(url string) string {
		if url == "https://dl.fedoraproject.org/pub/fedora/linux/releases/" {
			return `
<a href="38/">38/</a>
<a href="39/">39/</a>
<a href="40/">40/</a>
`
		}
		if strings.Contains(url, "40/Cloud/") {
			return `
<a href="Fedora-Cloud-Base-40-1.10.x86_64.qcow2">Fedora-Cloud-Base-40-1.10.x86_64.qcow2</a>
<a href="Fedora-Cloud-Base-40-1.10.x86_64-CHECKSUM">Fedora-Cloud-Base-40-1.10.x86_64-CHECKSUM</a>
`
		}
		return ""
	}

	archName = "x86_64"
	filename, qcowURL, _, ok := latestFedoraCloudImage()
	if !ok {
		t.Fatal("expected success")
	}
	if filename != "Fedora-Cloud-Base-40-1.10.x86_64.qcow2" {
		t.Errorf("unexpected filename: %q", filename)
	}
	if !strings.Contains(qcowURL, "40/Cloud/x86_64/images/") {
		t.Errorf("unexpected qcowURL: %q", qcowURL)
	}
}

func TestInstallFirecrackerZshFunction(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	zshrc := filepath.Join(tmp, ".zshrc")
	osWriteFile(zshrc, []byte("echo initial\n"), 0644)

	osReadFile = func(name string) ([]byte, error) {
		if name == zshrc {
			return []byte("echo initial\n"), nil
		}
		return nil, os.ErrNotExist
	}

	var writtenContent string
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if name == zshrc {
			writtenContent = string(data)
		}
		return nil
	}

	installFirecrackerZshFunction("firecracker() { echo wrapper; }")

	if !strings.Contains(writtenContent, "firecracker() { echo wrapper; }") {
		t.Errorf("expected wrapper code inside written zshrc, got %q", writtenContent)
	}
}

func TestVerifyFedoraQcow2(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	qcow2 := filepath.Join(tmp, "fedora.qcow2")
	os.WriteFile(qcow2, []byte("qcow2-content"), 0644)
	// Hash of "qcow2-content" is fa13cb14afd725b7efaa126bd84a2a848fe9a46267251afecc769d7bdd6fcd01

	fetchText = func(url string) string {
		return "SHA256 (fedora.qcow2) = fa13cb14afd725b7efaa126bd84a2a848fe9a46267251afecc769d7bdd6fcd01"
	}

	if !verifyFedoraQcow2(qcow2, "http://checksum-url") {
		t.Error("expected verification to succeed")
	}
}

func TestDownloadFedoraImage(t *testing.T) {
	defer resetMocks()

	// Case 1: curl exists
	hasCmd = func(name string) bool { return name == "curl" }
	var runCmdCalled bool
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "curl" {
			runCmdCalled = true
		}
		return CmdResult{ExitCode: 0}
	}
	downloadFedoraImage("http://url", "/tmp/dest")
	if !runCmdCalled {
		t.Error("expected curl command to be run")
	}

	// Case 2: curl does not exist
	resetMocks()
	hasCmd = func(name string) bool { return false }
	var downloadCalled bool
	download = func(url, dest string) bool {
		downloadCalled = true
		return true
	}
	downloadFedoraImage("http://url", "/tmp/dest")
	if !downloadCalled {
		t.Error("expected download function to be called")
	}
}

func TestWriteCloudInitSeedAndISO(t *testing.T) {
	defer resetMocks()

	var mkdirCalls []string
	osMkdirAll = func(path string, perm os.FileMode) error {
		mkdirCalls = append(mkdirCalls, path)
		return nil
	}

	var writtenFiles []string
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		writtenFiles = append(writtenFiles, name)
		return nil
	}

	err := writeCloudInitSeed("/tmp/seed", "ssh-pubkey")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(mkdirCalls) != 1 || mkdirCalls[0] != "/tmp/seed" {
		t.Errorf("unexpected mkdir calls: %v", mkdirCalls)
	}
	if len(writtenFiles) != 2 {
		t.Errorf("expected user-data and meta-data files to be written, got: %v", writtenFiles)
	}

	// buildSeedISO
	var removed bool
	osRemove = func(path string) error {
		if path == "/tmp/seed.iso" {
			removed = true
		}
		return nil
	}
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	buildSeedISO("/tmp/seed", "/tmp/seed.iso")
	if !removed {
		t.Error("expected osRemove to delete old ISO first")
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "hdiutil" {
		t.Errorf("expected hdiutil call, got %v", runCmdCalls)
	}
}

func TestWriteQEMUStartScript(t *testing.T) {
	defer resetMocks()

	var writtenPath string
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		writtenPath = name
		return nil
	}

	t.Setenv("HOME", "/my/home")
	path := writeQEMUStartScript()
	if !strings.HasSuffix(path, "vm-start.sh") {
		t.Errorf("unexpected script path: %q", path)
	}
	if !strings.HasSuffix(writtenPath, "vm-start.sh") {
		t.Errorf("expected script to be written, got %q", writtenPath)
	}
}

func TestSSHToVMAndHelpers(t *testing.T) {
	defer resetMocks()

	// sshToVM
	var probeCall []string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCall = argv
		return CmdResult{ExitCode: 0}, true
	}
	res := sshToVM("/path/to/key", []string{"ls"}, 0)
	if !res.OK() {
		t.Errorf("expected OK result, got %+v", res)
	}
	if probeCall[0] != "ssh" || !strings.Contains(strings.Join(probeCall, " "), "fc@127.0.0.1") {
		t.Errorf("unexpected probe command: %v", probeCall)
	}

	// waitForVMSSH
	var probeCalls int
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCalls++
		return CmdResult{ExitCode: 0}, true
	}
	if !waitForVMSSH("/path/to/key", time.Second) {
		t.Error("expected wait to succeed")
	}
	if probeCalls != 1 {
		t.Errorf("expected 1 probe call, got %d", probeCalls)
	}

	// Failure case with short timeout
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	if waitForVMSSH("/path/to/key", 10*time.Millisecond) {
		t.Error("expected wait to fail")
	}

	// waitForFirecrackerInVM
	probeCalls = 0
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCalls++
		return CmdResult{ExitCode: 0}, true
	}
	if !waitForFirecrackerInVM("/path/to/key", time.Second) {
		t.Error("expected wait to succeed")
	}

	// Failure case with short timeout
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	if waitForFirecrackerInVM("/path/to/key", 10*time.Millisecond) {
		t.Error("expected wait to fail")
	}
}

func TestEnsureVirtualBoxAndProvision(t *testing.T) {
	defer resetMocks()

	// Case 1: VBoxManage exists
	hasCmd = func(name string) bool { return name == "VBoxManage" }
	if !ensureVirtualBox() {
		t.Error("expected ensureVirtualBox to be true when VBoxManage exists")
	}

	// Case 2: VBoxManage does not exist, brew install succeeds
	resetMocks()
	hasCmdCalls := 0
	hasCmd = func(name string) bool {
		hasCmdCalls++
		// First check (is VBoxManage in path) -> returns false.
		// Second check (is VBoxManage in path after brew install) -> returns true.
		if name == "VBoxManage" {
			return hasCmdCalls > 1
		}
		return false
	}
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	if !ensureVirtualBox() {
		t.Error("expected ensureVirtualBox to be true after install")
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][3] != "virtualbox" {
		t.Errorf("expected brew install virtualbox call, got %v", runCmdCalls)
	}

	// Test provisionVirtualBoxVM
	resetMocks()
	t.Setenv("HOME", "/my/home")
	hasCmd = func(name string) bool { return name == "VBoxManage" }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[1] == "showvminfo" {
			return CmdResult{ExitCode: 1}, true // VM does not exist yet
		}
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // vdi exists
	}
	runCmdCalls = nil
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return nil
	}
	scriptPath := provisionVirtualBoxVM("/tmp/fedora.qcow2", "/tmp/seed.iso")
	if !strings.HasSuffix(scriptPath, "vm-start.sh") {
		t.Errorf("unexpected script path: %q", scriptPath)
	}
	if len(runCmdCalls) < 2 {
		t.Errorf("expected virtualbox setup commands, got %v", runCmdCalls)
	}
}

func TestSetupFirecrackerVM(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true
		}
		if argv[0] == "sysctl" {
			return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true
		}
		if argv[0] == "ssh" {
			return CmdResult{ExitCode: 0}, true // SSH succeeds
		}
		return CmdResult{ExitCode: 0}, true
	}

	osStat = func(name string) (os.FileInfo, error) {
		// Mock files exist
		return nil, nil
	}

	osReadFile = func(name string) ([]byte, error) {
		return []byte("ssh-key"), nil
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return nil
	}

	setupFirecrackerVM()

	if len(runCmdCalls) < 1 {
		t.Errorf("expected VM setup start script execution, got: %v", runCmdCalls)
	}
}

func TestMacosGoEdgeCases(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	hasCmd = func(name string) bool { return false }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	if ensureVirtualBox() {
		t.Error("expected ensureVirtualBox to fail when brew install fails")
	}

	resetMocks()
	isMacOS = true
	hasCmd = func(name string) bool {
		return false
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	if ensureVirtualBox() {
		t.Error("expected ensureVirtualBox to fail when VBoxManage still not in PATH")
	}

	resetMocks()
	isMacOS = true
	hasCmd = func(name string) bool { return false }
	runCmd = func(argv []string, opts CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	if path := provisionVirtualBoxVM("qcow", "iso"); path != "" {
		t.Errorf("expected empty path when VirtualBox setup fails, got %q", path)
	}

	resetMocks()
	isMacOS = true
	t.Setenv("HOME", "/my/home")
	hasCmd = func(name string) bool { return name == "VBoxManage" }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[1] == "clonemedium" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	if path := provisionVirtualBoxVM("qcow", "iso"); path != "" {
		t.Errorf("expected empty path when clonemedium fails, got %q", path)
	}

	resetMocks()
	isMacOS = true
	t.Setenv("HOME", "/my/home")
	hasCmd = func(name string) bool { return name == "VBoxManage" }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[1] == "createvm" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	if path := provisionVirtualBoxVM("qcow", "iso"); path != "" {
		t.Errorf("expected empty path when createvm fails, got %q", path)
	}

	resetMocks()
	isMacOS = false
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("14.0\n")}, true
		}
		return CmdResult{ExitCode: 0}, true
	}
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "x86_64"
	hasCmd = func(name string) bool { return name == "VBoxManage" }
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, "id_ed25519") {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "ssh-keygen" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	setupFirecrackerVM()
}

func TestSetupFirecrackerVMEdgeCases(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" {
			return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true
		}
		if argv[0] == "sysctl" {
			return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true
		}
		return CmdResult{ExitCode: 0}, true
	}

	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, "fedora.qcow2") || strings.HasSuffix(name, "id_ed25519") {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	fetchText = func(url string) string { return "" }
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" { return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true }
		if argv[0] == "sysctl" { return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true }
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, "fedora.qcow2") { return nil, os.ErrNotExist }
		return nil, nil
	}
	fetchText = func(url string) string {
		if strings.Contains(url, "releases") { return "40/" }
		return "<a href=\"Fedora-Cloud-Base-40.qcow2\">Fedora-Cloud-Base-40.qcow2</a>"
	}
	hasCmd = func(name string) bool { return false }
	download = func(url, dest string) bool { return false }
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" { return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true }
		if argv[0] == "sysctl" { return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true }
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, "fedora.qcow2") { return nil, os.ErrNotExist }
		return nil, nil
	}
	fetchText = func(url string) string {
		if strings.Contains(url, "releases") { return "40/" }
		if strings.Contains(url, "CHECKSUM") { return "mismatch-sha  Fedora-Cloud-Base-40.qcow2" }
		return "<a href=\"Fedora-Cloud-Base-40.qcow2\">Fedora-Cloud-Base-40.qcow2</a>"
	}
	download = func(url, dest string) bool { return true }
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" { return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true }
		if argv[0] == "sysctl" { return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true }
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(name string) (os.FileInfo, error) { return nil, nil }
	osReadFile = func(name string) ([]byte, error) { return []byte("ssh-pubkey"), nil }
	hasCmd = func(name string) bool {
		if name == "qemu-system-aarch64" { return false }
		return true
	}
	setupFirecrackerVM()

	resetMocks()
	isMacOS = true
	archName = "aarch64"
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "sw_vers" { return CmdResult{ExitCode: 0, Stdout: []byte("15.0\n")}, true }
		if argv[0] == "sysctl" { return CmdResult{ExitCode: 0, Stdout: []byte("Apple M3\n")}, true }
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(name string) (os.FileInfo, error) { return nil, nil }
	osReadFile = func(name string) ([]byte, error) { return []byte("ssh-pubkey"), nil }
	hasCmd = func(name string) bool { return true }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if strings.HasSuffix(argv[0], "vm-start.sh") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	setupFirecrackerVM()
}
