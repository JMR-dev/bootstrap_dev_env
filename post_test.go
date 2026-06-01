package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallPyenv(t *testing.T) {
	defer resetMocks()

	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}

	installPyenv()
	if !strings.Contains(runShellCmd, "pyenv.run | bash") {
		t.Errorf("unexpected shell command: %q", runShellCmd)
	}
}

func TestPython3DecimalOK(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	if !python3DecimalOK() {
		t.Error("expected decimal OK")
	}

	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	if python3DecimalOK() {
		t.Error("expected decimal NOT OK")
	}
}

func TestFixPython3Decimal(t *testing.T) {
	defer resetMocks()

	pkgMgr = "apt-get"
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		// Mock python3DecimalOK to return true after fix is called
		return CmdResult{ExitCode: 0}, true
	}

	ok := fixPython3Decimal()
	if !ok {
		t.Error("expected fix to succeed")
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "apt-get" || runCmdCalls[0][3] != "python3-full" {
		t.Errorf("expected apt-get install python3-full, got: %v", runCmdCalls)
	}
}

func TestLatestStablePython(t *testing.T) {
	defer resetMocks()

	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		output := `
  3.10.1
  3.11.2
  3.12.0
  3.12.1
  3.12.2
  2.7.18
  system
`
		return CmdResult{ExitCode: 0, Stdout: []byte(output)}, true
	}

	latest := latestStablePython("/path/to/pyenv")
	if latest != "3.12.2" {
		t.Errorf("expected 3.12.2, got %q", latest)
	}
}

func TestInstallNVM(t *testing.T) {
	defer resetMocks()

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.TagName = "v0.39.5"
		return true
	}

	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}

	installNVM()
	if !strings.Contains(runShellCmd, "nvm/v0.39.5/install.sh") {
		t.Errorf("unexpected NVM install script: %q", runShellCmd)
	}
}

func TestInstallOhMyZsh(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist // Not present, trigger install
	}

	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	zshrc := filepath.Join(tmp, ".zshrc")
	osWriteFile(zshrc, []byte("ZSH_THEME=\"robbyrussell\""), 0644)

	osReadFile = func(name string) ([]byte, error) {
		if name == zshrc {
			return []byte("ZSH_THEME=\"robbyrussell\""), nil
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

	installOhMyZsh()
	if !strings.Contains(runShellCmd, "ohmyzsh/master/tools/install.sh") {
		t.Errorf("unexpected installer: %q", runShellCmd)
	}
	if !strings.Contains(writtenContent, "ZSH_THEME=\"gnzh\"") {
		t.Errorf("expected theme replaced to gnzh, got: %q", writtenContent)
	}
}

func TestEnsureZshDefault(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh")}, true
	}

	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_USER", u.Username)

	passwdPath = filepath.Join(t.TempDir(), "passwd")
	passwdContent := fmt.Sprintf("%s:x:%s:%s::/home/%s:/bin/bash\n", u.Username, u.Uid, u.Gid, u.Username)
	osWriteFile(passwdPath, []byte(passwdContent), 0644)

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	isRHELFamily = false
	ensureZshDefault()

	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "chsh" || runCmdCalls[0][2] != "/bin/zsh" {
		t.Errorf("expected chsh call to zsh, got: %v", runCmdCalls)
	}
}

func TestCloneNvimConfig(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var statCalls []string
	osStat = func(name string) (os.FileInfo, error) {
		statCalls = append(statCalls, name)
		if strings.HasSuffix(name, "nvim") {
			return nil, nil // config directory exists
		}
		return nil, os.ErrNotExist // backup folder doesn't exist
	}

	var renameCalls [][]string
	osRename = func(oldpath, newpath string) error {
		renameCalls = append(renameCalls, []string{oldpath, newpath})
		return nil
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	cloneNvimConfig()

	// Should have at least one rename call (for backup or tempClone rename)
	if len(renameCalls) < 2 {
		t.Fatalf("expected at least 2 rename calls, got: %v", renameCalls)
	}
	if !strings.HasSuffix(renameCalls[0][0], "nvim") || !strings.Contains(renameCalls[0][1], "nvim-1") {
		t.Errorf("expected backup rename first, got: %v", renameCalls[0])
	}
	if !strings.HasSuffix(renameCalls[1][0], "nvim-config") || !strings.HasSuffix(renameCalls[1][1], "nvim") {
		t.Errorf("expected temp clone rename second, got: %v", renameCalls[1])
	}

	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "git" || runCmdCalls[0][1] != "clone" {
		t.Errorf("expected git clone call, got: %v", runCmdCalls)
	}
}

func TestCloneNvimConfigSSHFallback(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist // nothing exists
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		// First clone (SSH) fails; second (HTTPS) succeeds.
		if len(runCmdCalls) == 1 {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}

	cloneNvimConfig()

	if len(runCmdCalls) != 2 {
		t.Fatalf("expected 2 git clone attempts (SSH then HTTPS), got: %v", runCmdCalls)
	}
	if !strings.HasPrefix(runCmdCalls[0][2], "git@github.com:") {
		t.Errorf("expected first attempt to use SSH URL, got: %v", runCmdCalls[0])
	}
	if !strings.HasPrefix(runCmdCalls[1][2], "https://github.com/") {
		t.Errorf("expected fallback to use HTTPS URL, got: %v", runCmdCalls[1])
	}
	if hasErrors() {
		t.Errorf("expected no errors logged when HTTPS fallback succeeds")
	}
}

func TestCloneNvimConfigBothFail(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}

	cloneNvimConfig()

	if !hasErrors() {
		t.Errorf("expected error logged when both SSH and HTTPS clones fail")
	}
}

func TestCheckAndSetupSSH(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		// Mock ghLoggedIn to return false (requires auth)
		return CmdResult{ExitCode: 1}, true
	}

	stdin = strings.NewReader("y\n") // agree to login

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	checkAndSetupSSH()

	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "gh" || runCmdCalls[0][2] != "login" {
		t.Errorf("expected gh auth login command, got: %v", runCmdCalls)
	}
}

func TestInstallPipAgyNpm(t *testing.T) {
	defer resetMocks()

	// Case 1: python3 not installed for pip
	hasCmd = func(name string) bool { return false }
	installPip() // should log error and return

	// Case 2: installPip when python3 is installed
	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true // decimal OK and pip check OK
	}
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	installPip()
	if len(runCmdCalls) < 2 {
		t.Errorf("expected ensurepip and upgrade pip calls, got: %v", runCmdCalls)
	}

	// Test installAgy
	resetMocks()
	var runShellCmd string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmd = cmd
		return CmdResult{ExitCode: 0}
	}
	installAgy()
	if !strings.Contains(runShellCmd, "antigravity.google/cli/install.sh") {
		t.Errorf("unexpected agy install command: %q", runShellCmd)
	}

	// Test installNpmPackage
	resetMocks()
	var runShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmds = append(runShellCmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // nvm exists
	}
	installNpmPackage("my-package")
	if len(runShellCmds) < 1 || !strings.Contains(runShellCmds[len(runShellCmds)-1], "pnpm add -g my-package") {
		t.Errorf("unexpected npm install command: %v", runShellCmds)
	}
	if !strings.Contains(runShellCmds[len(runShellCmds)-1], `PNPM_HOME=`) {
		t.Errorf("expected PNPM_HOME export, got: %v", runShellCmds)
	}

	// macOS uses ~/Library/pnpm
	resetMocks()
	isMacOS = true
	runShellCmds = nil
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmds = append(runShellCmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	installNpmPackage("my-package")
	if len(runShellCmds) < 1 || !strings.Contains(runShellCmds[len(runShellCmds)-1], `Library/pnpm`) {
		t.Errorf("expected macOS PNPM_HOME path, got: %v", runShellCmds)
	}
}

func TestEnsurePythonAndNode(t *testing.T) {
	defer resetMocks()

	// Test ensurePythonLatest
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // pyenvDir and pyenvBin exist
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[1] == "install" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.12.0\n")}, true
		}
		if argv[1] == "versions" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.12.0\n")}, true // already installed
		}
		return CmdResult{ExitCode: 0}, true
	}
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	wg := ensurePythonLatest()
	if wg != nil {
		t.Error("expected nil waitgroup since Python 3.12.0 is already installed")
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][1] != "global" {
		t.Errorf("expected global 3.12.0 command, got: %v", runCmdCalls)
	}

	// Test ensureNodeLTS
	resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // nvm exists
	}
	var runShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmds = append(runShellCmds, cmd)
		if strings.Contains(cmd, "nvm version lts/*") {
			return CmdResult{ExitCode: 0, Stdout: []byte("N/A")} // Not installed yet
		}
		return CmdResult{ExitCode: 0}
	}
	ensureNodeLTS()
	hasInstall := false
	hasPnpmSetup := false
	for _, cmd := range runShellCmds {
		if strings.Contains(cmd, "nvm install --lts") {
			hasInstall = true
		}
		if strings.Contains(cmd, "pnpm setup") {
			hasPnpmSetup = true
		}
	}
	if !hasInstall {
		t.Errorf("expected nvm install --lts, got: %v", runShellCmds)
	}
	if !hasPnpmSetup {
		t.Errorf("expected pnpm setup, got: %v", runShellCmds)
	}
}

func TestInstallPipEdgeCases(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	var probeCalls [][]string
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		probeCalls = append(probeCalls, argv)
		if len(probeCalls) == 1 {
			return CmdResult{ExitCode: 1}, true
		}
		return CmdResult{ExitCode: 0}, true
	}
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	pkgMgr = "dnf"
	installPip()
	if len(runCmdCalls) < 3 {
		t.Errorf("expected python3-libs fix, ensurepip, and upgrade, got: %v", runCmdCalls)
	}

	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	installPip()

	resetMocks()
	pkgMgr = "apt-get"
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "python3" && argv[2] == "ensurepip" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPip()

	resetMocks()
	pkgMgr = "pacman"
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "python3" && argv[2] == "ensurepip" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPip()

	resetMocks()
	pkgMgr = "brew"
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "python3" && argv[2] == "ensurepip" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPip()

	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "python3" && argv[3] == "install" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPip()
}

func TestEnsureZshDefaultEdgeCases(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return false }
	ensureZshDefault()

	resetMocks()
	hasCmd = func(name string) bool {
		return name == "zsh"
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[0] == "which" {
			return CmdResult{ExitCode: 1}, true
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("")}, true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	passwdPath = filepath.Join(t.TempDir(), "passwd")
	ensureZshDefault()

	resetMocks()
	isRHELFamily = true
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh")}, true
	}
	u, _ := user.Current()
	t.Setenv("SUDO_USER", u.Username)
	passwdPath = filepath.Join(t.TempDir(), "passwd")
	passwdContent := fmt.Sprintf("%s:x:%s:%s::/home/%s:/bin/zsh\n", u.Username, u.Uid, u.Gid, u.Username)
	osWriteFile(passwdPath, []byte(passwdContent), 0644)
	ensureZshDefault()

	resetMocks()
	isRHELFamily = true
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh")}, true
	}
	t.Setenv("SUDO_USER", u.Username)
	passwdContent = fmt.Sprintf("%s:x:%s:%s::/home/%s:/bin/bash\n", u.Username, u.Uid, u.Gid, u.Username)
	osWriteFile(passwdPath, []byte(passwdContent), 0644)
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	ensureZshDefault()
}

func TestUserLoginShellEdgeCases(t *testing.T) {
	defer resetMocks()

	passwdPath = "/nonexistent"
	if shell := userLoginShell("1000"); shell != "" {
		t.Errorf("expected empty shell, got %q", shell)
	}

	passwdPath = filepath.Join(t.TempDir(), "passwd")
	content := "root:x:0:0:root:/root\nmalformed:line\n"
	osWriteFile(passwdPath, []byte(content), 0644)
	if shell := userLoginShell("0"); shell != "" {
		t.Errorf("expected empty shell for malformed/short lines, got %q", shell)
	}
}

func TestCloneNvimConfigEdgeCases(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, "nvim") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	osRename = func(oldpath, newpath string) error {
		return fmt.Errorf("rename failed error")
	}
	cloneNvimConfig()

	resetMocks()
	t.Setenv("HOME", tmp)
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	cloneNvimConfig()
}

func TestCheckAndSetupSSHEdgeCases(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool {
		return name != "gh"
	}
	checkAndSetupSSH()

	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	checkAndSetupSSH()

	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	stdin = strings.NewReader("n\n")
	checkAndSetupSSH()

	resetMocks()
	hasCmd = func(name string) bool { return true }
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	stdin = strings.NewReader("y\n")
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	checkAndSetupSSH()
}

func TestAskYNEdgeCases(t *testing.T) {
	defer resetMocks()

	stdin = &errReader{}
	if askYN("Prompt: ") {
		t.Error("expected false for stdin error")
	}
}

type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated read error")
}

func TestInstallGHExtension(t *testing.T) {
	defer resetMocks()

	// gh not installed
	hasCmd = func(name string) bool { return false }
	installGHExtension("JMR-dev/gh-repo-bootstrap") // should log error and return

	// gh installed, install succeeds
	resetMocks()
	hasCmd = func(name string) bool { return true }
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	installGHExtension("JMR-dev/gh-repo-bootstrap")
	if len(runCmdCalls) != 1 ||
		runCmdCalls[0][0] != "gh" ||
		runCmdCalls[0][1] != "extension" ||
		runCmdCalls[0][2] != "install" ||
		runCmdCalls[0][3] != "JMR-dev/gh-repo-bootstrap" {
		t.Errorf("expected gh extension install command, got: %v", runCmdCalls)
	}

	// gh installed, install fails
	resetMocks()
	hasCmd = func(name string) bool { return true }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	installGHExtension("JMR-dev/gh-repo-bootstrap") // should log error
}

func TestInstallNpmPackageEdgeCases(t *testing.T) {
	defer resetMocks()

	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	installNpmPackage("test-pkg")
}

func TestInstallPlaywright(t *testing.T) {
	defer resetMocks()

	// Edge case: NVM not installed
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	installPlaywright() // should log error and return

	// Happy path on apt-get: should pass --with-deps
	resetMocks()
	pkgMgr = "apt-get"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil // nvm exists
	}
	var runShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCmds = append(runShellCmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	installPlaywright()
	hasPnpmInstall := false
	hasPlaywrightBrowsers := false
	for _, cmd := range runShellCmds {
		if strings.Contains(cmd, "pnpm add -g playwright") {
			hasPnpmInstall = true
		}
		if strings.Contains(cmd, "pnpx playwright install --with-deps") {
			hasPlaywrightBrowsers = true
		}
	}
	if !hasPnpmInstall {
		t.Errorf("expected pnpm add -g playwright, got: %v", runShellCmds)
	}
	if !hasPlaywrightBrowsers {
		t.Errorf("expected pnpx playwright install --with-deps on apt-get, got: %v", runShellCmds)
	}

	// Happy path on non-apt (dnf/pacman/brew): must NOT pass --with-deps
	resetMocks()
	pkgMgr = "dnf"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	var dnfShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		dnfShellCmds = append(dnfShellCmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	installPlaywright()
	hasPlainBrowsers := false
	for _, cmd := range dnfShellCmds {
		if strings.Contains(cmd, "--with-deps") {
			t.Errorf("did not expect --with-deps on dnf, got: %v", dnfShellCmds)
		}
		if strings.Contains(cmd, "pnpx playwright install") {
			hasPlainBrowsers = true
		}
	}
	if !hasPlainBrowsers {
		t.Errorf("expected pnpx playwright install (without --with-deps) on dnf, got: %v", dnfShellCmds)
	}

	// Failure path: pnpm install fails, browser install should be skipped
	resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	var failShellCmds []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		failShellCmds = append(failShellCmds, cmd)
		if strings.Contains(cmd, "pnpm add -g playwright") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPlaywright()
	for _, cmd := range failShellCmds {
		if strings.Contains(cmd, "pnpx playwright install") {
			t.Errorf("browser install should be skipped when pnpm install fails, got: %v", failShellCmds)
		}
	}
}

func TestEnsureNodeLTSEdgeCases(t *testing.T) {
	defer resetMocks()

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}

	var runShellCalls []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCalls = append(runShellCalls, cmd)
		if strings.Contains(cmd, "nvm version lts/*") {
			return CmdResult{ExitCode: 0, Stdout: []byte("N/A")}
		}
		if strings.Contains(cmd, "nvm install --lts") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	ensureNodeLTS()

	resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		if strings.Contains(cmd, "nvm version lts/*") {
			return CmdResult{ExitCode: 0, Stdout: []byte("v20.0.0")}
		}
		if strings.Contains(cmd, "nvm alias default") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	ensureNodeLTS()

	resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		if strings.Contains(cmd, "nvm version lts/*") {
			return CmdResult{ExitCode: 0, Stdout: []byte("v20.0.0")}
		}
		if strings.Contains(cmd, "corepack enable") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	ensureNodeLTS()
}

func TestEnsurePythonLatestBackgroundInstall(t *testing.T) {
	defer resetMocks()

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[1] == "install" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.12.0\n")}, true
		}
		if argv[1] == "versions" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.11.0\n")}, true
		}
		return CmdResult{ExitCode: 0}, true
	}
	
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	
	wg := ensurePythonLatest()
	if wg == nil {
		t.Fatal("expected waitgroup since Python 3.12.0 needs install")
	}
	wg.Wait()

	// Failure case
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		if argv[1] == "install" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.12.0\n")}, true
		}
		if argv[1] == "versions" {
			return CmdResult{ExitCode: 0, Stdout: []byte("  3.11.0\n")}, true
		}
		return CmdResult{ExitCode: 0}, true
	}
	
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[1] == "install" {
			return CmdResult{ExitCode: 1, Stderr: []byte("error lines\nline 2")}
		}
		return CmdResult{ExitCode: 0}
	}
	
	wg = ensurePythonLatest()
	if wg != nil {
		wg.Wait()
	}
}

func TestEnsureLibreOfficeAutoSave(t *testing.T) {
	defer resetMocks()

	// 1. isMacOS = false, LibreOffice not installed
	isMacOS = false
	hasCmd = func(name string) bool {
		if name == "unopkg" {
			return false
		}
		return true
	}
	var calledDownload bool
	download = func(url, dest string) bool {
		calledDownload = true
		return true
	}
	ensureLibreOfficeAutoSave()
	if calledDownload {
		t.Error("expected download not to be called when unopkg is missing")
	}

	// 2. isMacOS = true, LibreOffice not installed
	isMacOS = true
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	calledDownload = false
	ensureLibreOfficeAutoSave()
	if calledDownload {
		t.Error("expected download not to be called when unopkg is missing on macOS")
	}

	// 3. isMacOS = false, LibreOffice installed, download fails
	isMacOS = false
	hasCmd = func(name string) bool {
		return name == "unopkg"
	}
	download = func(url, dest string) bool {
		return false
	}
	var runCmdCalled bool
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalled = true
		return CmdResult{ExitCode: 0}
	}
	ensureLibreOfficeAutoSave()
	if runCmdCalled {
		t.Error("expected runCmd not to be called when download fails")
	}

	// 4. isMacOS = false, LibreOffice installed, download succeeds, runCmd fails
	download = func(url, dest string) bool {
		return true
	}
	var unopkgArgv []string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		unopkgArgv = argv
		return CmdResult{ExitCode: 1}
	}
	ensureLibreOfficeAutoSave()
	if len(unopkgArgv) == 0 || unopkgArgv[0] != "unopkg" {
		t.Errorf("expected runCmd with unopkg, got: %v", unopkgArgv)
	}

	// 5. isMacOS = false, LibreOffice installed, download succeeds, runCmd succeeds
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		unopkgArgv = argv
		return CmdResult{ExitCode: 0}
	}
	ensureLibreOfficeAutoSave()
	if len(unopkgArgv) == 0 || unopkgArgv[0] != "unopkg" || unopkgArgv[1] != "add" || unopkgArgv[2] != "-f" {
		t.Errorf("expected runCmd with unopkg add -f, got: %v", unopkgArgv)
	}

	// 6. isMacOS = true, LibreOffice installed, download succeeds, runCmd succeeds
	isMacOS = true
	osStat = func(name string) (os.FileInfo, error) {
		if name == "/Applications/LibreOffice.app/Contents/MacOS/unopkg" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		unopkgArgv = argv
		return CmdResult{ExitCode: 0}
	}
	ensureLibreOfficeAutoSave()
	expectedMacPath := "/Applications/LibreOffice.app/Contents/MacOS/unopkg"
	if len(unopkgArgv) == 0 || unopkgArgv[0] != expectedMacPath {
		t.Errorf("expected runCmd with %s, got: %v", expectedMacPath, unopkgArgv)
	}
}



