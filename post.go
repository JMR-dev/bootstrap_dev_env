package main

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── pyenv / Python ──────────────────────────────────────────────────────

func installPyenv() {
	taskPrintln("  Installing pyenv via curl ...")
	if !runShell("curl https://pyenv.run | bash", CmdOpts{Out: taskOut()}).OK() {
		errLog("pyenv installation failed")
		return
	}
	taskPrintln("  pyenv installed to ~/.pyenv")
}

func python3DecimalOK() bool {
	if !hasCmd("python3") {
		return false
	}
	r, ok := probe([]string{"python3", "-c", "from decimal import Decimal"}, 10*time.Second)
	return ok && r.ExitCode == 0
}

func fixPython3Decimal() bool {
	switch pkgMgr {
	case "apt-get":
		runCmd([]string{"apt-get", "install", "-y", "python3-full"}, CmdOpts{AsSudo: true})
	case "dnf":
		runCmd([]string{"dnf", "install", "-y", "python3-libs"}, CmdOpts{AsSudo: true})
	case "pacman":
		runCmd([]string{"pacman", "-S", "--noconfirm", "--needed", "python"}, CmdOpts{AsSudo: true})
	}
	return python3DecimalOK()
}

func installPip() {
	out := taskOut()
	if !hasCmd("python3") {
		errLog("python3 is not installed — cannot install pip")
		return
	}
	if !python3DecimalOK() {
		warn("Python 3 _decimal C extension failed to import — attempting fix ...")
		if fixPython3Decimal() {
			taskPrintln("  Python 3 _decimal extension restored.")
		} else {
			errLog("Python 3 _decimal C extension could not be fixed. " +
				"Run: sudo apt-get install python3-full (Debian/Ubuntu), " +
				"sudo dnf install python3-libs (Fedora/RHEL), or " +
				"sudo pacman -S python (Arch)")
			return
		}
	}

	taskPrintln("  Bootstrapping pip via 'python3 -m ensurepip --upgrade' ...")
	bootstrap := runCmd([]string{"python3", "-m", "ensurepip", "--upgrade"}, CmdOpts{AsSudo: true, Out: out})
	if !bootstrap.OK() {
		switch pkgMgr {
		case "apt-get":
			warn("ensurepip unavailable in system Python — installing python3-pip via apt-get")
			if !runCmd([]string{"apt-get", "install", "-y", "python3-pip"}, CmdOpts{AsSudo: true, Out: out}).OK() {
				errLog("python3-pip failed to install via apt-get — skipping pip bootstrap")
				return
			}
		case "pacman":
			warn("ensurepip unavailable in system Python — installing python-pip via pacman")
			if !runCmd([]string{"pacman", "-S", "--noconfirm", "--needed", "python-pip"}, CmdOpts{AsSudo: true, Out: out}).OK() {
				errLog("python-pip failed to install via pacman — skipping pip bootstrap")
				return
			}
		default:
			errLog("python3 -m ensurepip failed (system Python may need a distro 'python3-pip' package)")
			return
		}
	}
	taskPrintln("  Upgrading pip to the latest version ...")
	upgrade := runCmd([]string{"python3", "-m", "pip", "install", "--upgrade", "pip"}, CmdOpts{AsSudo: true, Out: out})
	if !upgrade.OK() {
		warn("pip self-upgrade failed (likely PEP 668 externally-managed); ensurepip-provided pip remains")
	}
}

func latestStablePython(pyenvBin string) string {
	r, ok := probe([]string{pyenvBin, "install", "--list"}, 2*time.Minute)
	if !ok {
		errLog("pyenv install --list failed")
		return ""
	}
	if r.ExitCode != 0 {
		errLog("pyenv install --list failed")
		return ""
	}
	stableRe := regexp.MustCompile(`^\s*(\d+)\.(\d+)\.(\d+)\s*$`)
	type ver struct{ a, b, c int }
	var versions []ver
	for _, line := range strings.Split(string(r.Stdout), "\n") {
		m := stableRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		if a >= 3 {
			versions = append(versions, ver{a, b, c})
		}
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].a != versions[j].a {
			return versions[i].a < versions[j].a
		}
		if versions[i].b != versions[j].b {
			return versions[i].b < versions[j].b
		}
		return versions[i].c < versions[j].c
	})
	v := versions[len(versions)-1]
	return fmt.Sprintf("%d.%d.%d", v.a, v.b, v.c)
}

// ensurePythonLatest installs the latest stable Python via pyenv if not
// present, then sets it as global. Returns a wait group if an install was
// kicked off in the background; the caller must call .Wait() before exiting.
func ensurePythonLatest() *sync.WaitGroup {
	home, _ := os.UserHomeDir()
	pyenvDir := filepath.Join(home, ".pyenv")
	if _, err := osStat(pyenvDir); err != nil {
		return nil
	}
	pyenvBin := filepath.Join(pyenvDir, "bin", "pyenv")
	if _, err := osStat(pyenvBin); err != nil {
		warn(fmt.Sprintf("pyenv binary not found at %s", pyenvBin))
		return nil
	}

	latest := latestStablePython(pyenvBin)
	if latest == "" {
		errLog("Could not determine latest stable Python from pyenv")
		return nil
	}

	r, ok := probe([]string{pyenvBin, "versions", "--bare"}, 30*time.Second)
	if !ok {
		return nil
	}
	installed := strings.Fields(string(r.Stdout))
	for _, v := range installed {
		if v == latest {
			fmt.Printf("\n[pyenv] Python %s already installed.\n", latest)
			fmt.Printf("[pyenv] Setting Python %s as global default ...\n", latest)
			if !runCmd([]string{pyenvBin, "global", latest}, CmdOpts{}).OK() {
				errLog(fmt.Sprintf("pyenv global %s failed", latest))
			}
			return nil
		}
	}

	fmt.Printf("\n[pyenv] Backgrounding install of Python %s (compile may take several minutes) ...\n", latest)
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := runCmd([]string{pyenvBin, "install", "--skip-existing", latest},
			CmdOpts{Timeout: 60 * time.Minute, Capture: true})
		elapsed := int(time.Since(start).Seconds())
		if !r.OK() {
			errLog(fmt.Sprintf("pyenv install %s failed after %ds", latest, elapsed))
			if len(r.Stderr) > 0 {
				lines := strings.Split(string(r.Stderr), "\n")
				if len(lines) > 20 {
					lines = lines[len(lines)-20:]
				}
				fmt.Printf("\n[pyenv stderr tail]\n%s\n", strings.Join(lines, "\n"))
			}
			return
		}
		if !runCmd([]string{pyenvBin, "global", latest},
			CmdOpts{Timeout: time.Minute}).OK() {
			errLog(fmt.Sprintf("pyenv global %s failed", latest))
			return
		}
		fmt.Printf("\n[pyenv] Python %s installed and set as global default (%ds).\n", latest, elapsed)
	}()
	return &wg
}

// ── nvm / Node ──────────────────────────────────────────────────────────

func installNVM() {
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/nvm-sh/nvm/releases/latest", &rel) {
		return
	}
	version := rel.TagName
	if version == "" {
		errLog("NVM tag_name missing")
		return
	}
	installURL := fmt.Sprintf("https://raw.githubusercontent.com/nvm-sh/nvm/%s/install.sh", version)
	taskPrintf("  Installing NVM %s via curl ...\n", version)
	if !runShell(fmt.Sprintf("curl -o- %s | bash", installURL), CmdOpts{Out: taskOut()}).OK() {
		errLog("NVM installation failed")
		return
	}
	taskPrintf("  NVM %s installed to ~/.nvm\n", version)
}

func ensureNodeLTS() {
	home, _ := os.UserHomeDir()
	if _, err := osStat(filepath.Join(home, ".nvm")); err != nil {
		return
	}
	check := runShell(`bash -c "source ~/.nvm/nvm.sh 2>/dev/null && nvm version lts/* 2>/dev/null"`,
		CmdOpts{Capture: true})
	installed := strings.TrimSpace(string(check.Stdout))
	if installed != "" && installed != "N/A" {
		fmt.Printf("\n[NVM] Node LTS (%s) already installed.\n", installed)
	} else {
		fmt.Println("\n[NVM] Installing Node.js LTS ...")
		if !runShell(`bash -c "source ~/.nvm/nvm.sh && nvm install --lts"`, CmdOpts{}).OK() {
			errLog("Node.js LTS install via nvm failed")
			return
		}
		fmt.Println("  Node.js LTS installed.")
	}

	fmt.Println("[NVM] Setting Node LTS as default ...")
	if !runShell(`bash -c "source ~/.nvm/nvm.sh && nvm alias default 'lts/*' && nvm use --lts"`, CmdOpts{}).OK() {
		errLog("Setting nvm default to LTS failed")
	}

	fmt.Println("[NVM] Enabling corepack and installing latest pnpm ...")
	if !runShell(`bash -c 'source ~/.nvm/nvm.sh && corepack enable && corepack prepare pnpm@latest --activate'`, CmdOpts{}).OK() {
		errLog("Failed to enable corepack and prepare pnpm")
		return
	}
	fmt.Println("[pnpm] Running pnpm setup to configure PATH ...")
	if !runShell(`bash -c 'export SHELL=/bin/bash && source ~/.nvm/nvm.sh && pnpm setup'`, CmdOpts{}).OK() {
		errLog("pnpm setup failed")
	}
}

// ── oh-my-zsh ───────────────────────────────────────────────────────────

func installOhMyZsh() {
	if !hasCmd("zsh") {
		errLog("zsh is not installed — required by oh-my-zsh")
		return
	}
	if !hasCmd("git") {
		errLog("git is not installed — required by oh-my-zsh")
		return
	}
	home, _ := os.UserHomeDir()
	target := filepath.Join(home, ".oh-my-zsh")
	if _, err := osStat(target); err == nil {
		taskPrintf("  oh-my-zsh already present at %s; updating theme only\n", target)
	} else {
		taskPrintln("  Installing oh-my-zsh via the official installer ...")
		installer := `sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended`
		if !runShell(installer, CmdOpts{Out: taskOut()}).OK() {
			errLog("oh-my-zsh installer failed")
			return
		}
	}

	zshrc := filepath.Join(home, ".zshrc")
	data, err := osReadFile(zshrc)
	if err != nil {
		warn("~/.zshrc not present after oh-my-zsh install; cannot set theme")
		return
	}
	text := string(data)
	re := regexp.MustCompile(`(?m)^\s*ZSH_THEME=.*$`)
	var newText string
	if re.MatchString(text) {
		newText = re.ReplaceAllString(text, `ZSH_THEME="gnzh"`)
	} else {
		newText = strings.TrimRight(text, "\n") + "\nZSH_THEME=\"gnzh\"\n"
	}
	if newText != text {
		if err := osWriteFile(zshrc, []byte(newText), 0o644); err != nil {
			errLog(fmt.Sprintf("could not write ~/.zshrc: %v", err))
			return
		}
		taskPrintln(`  Set ZSH_THEME="gnzh" in ~/.zshrc`)
	} else {
		taskPrintln(`  ~/.zshrc already has ZSH_THEME="gnzh"`)
	}
}

// ── default-shell + neovim config + gh auth ─────────────────────────────

func invokingUser() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

func ensureZshDefault() {
	if !hasCmd("zsh") {
		warn("zsh not installed — skipping default-shell change")
		return
	}
	zshPath := "/bin/zsh"
	if r, ok := probe([]string{"which", "zsh"}, 5*time.Second); ok && r.ExitCode == 0 {
		if p := strings.TrimSpace(string(r.Stdout)); p != "" {
			zshPath = p
		}
	}
	username := invokingUser()
	if username == "" {
		warn("could not determine invoking user; skipping default-shell change")
		return
	}
	u, err := user.Lookup(username)
	if err != nil {
		warn(fmt.Sprintf("user %s not found in passwd; skipping default-shell change", username))
		return
	}
	current := userLoginShell(u.Uid)
	if current == zshPath {
		fmt.Printf("\n[zsh] %s's default shell is already %s.\n", username, zshPath)
		return
	}

	family := "Debian-family"
	if isRHELFamily {
		family = "RHEL-family"
	} else if isArchFamily {
		family = "Arch-family"
	} else if isMacOS {
		family = "macOS"
	}
	fmt.Printf("\n[zsh] Setting default shell for %s to %s (%s) ...\n", username, zshPath, family)

	var cmd []string
	if isRHELFamily {
		cmd = []string{"usermod", "-s", zshPath, username}
	} else {
		cmd = []string{"chsh", "-s", zshPath, username}
	}
	if !runCmd(cmd, CmdOpts{AsSudo: true}).OK() {
		errLog(fmt.Sprintf("Failed to set default shell to zsh for %s", username))
	} else {
		fmt.Println("[zsh] Default shell updated. Log out and back in for it to take effect.")
	}
}

// userLoginShell returns the login shell for uid by parsing /etc/passwd. On
// macOS the shell may be set by dscl; getent isn't available either, so we
// just read passwd directly which works on every supported platform.
func userLoginShell(uid string) string {
	data, err := osReadFile(passwdPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}
		if parts[2] == uid {
			return parts[6]
		}
	}
	return ""
}

func isZshDefault() bool {
	if !hasCmd("zsh") {
		return false
	}
	zshPath := "/bin/zsh"
	if r, ok := probe([]string{"which", "zsh"}, 5*time.Second); ok && r.ExitCode == 0 {
		if p := strings.TrimSpace(string(r.Stdout)); p != "" {
			zshPath = p
		}
	}
	username := invokingUser()
	if username == "" {
		return false
	}
	u, err := user.Lookup(username)
	if err != nil {
		return false
	}
	current := userLoginShell(u.Uid)
	return current == zshPath
}

func cloneNvimConfig() {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "nvim")
	const sshURL = "git@github.com:JMR-dev/nvim-config.git"
	const httpsURL = "https://github.com/JMR-dev/nvim-config.git"

	fmt.Printf("\n[Neovim] Setting up configuration from %s ...\n", sshURL)

	if _, err := osStat(configDir); err == nil {
		n := 1
		var backup string
		for {
			backup = filepath.Join(filepath.Dir(configDir), fmt.Sprintf("nvim-%d", n))
			if _, err := osStat(backup); os.IsNotExist(err) {
				break
			}
			n++
		}
		fmt.Printf("  Renaming existing %s → %s ...\n", configDir, backup)
		if err := osRename(configDir, backup); err != nil {
			errLog(fmt.Sprintf("could not back up existing nvim config: %v", err))
			return
		}
		notice(fmt.Sprintf("Previous Neovim config preserved at %s", backup))
	}

	osMkdirAll(filepath.Dir(configDir), 0o755)

	repoName := strings.TrimSuffix(filepath.Base(sshURL), ".git")
	tempClone := filepath.Join(filepath.Dir(configDir), repoName)
	osRemoveAll(tempClone)

	fmt.Printf("  Cloning to %s ...\n", configDir)
	if !runCmd([]string{"git", "clone", sshURL, tempClone}, CmdOpts{}).OK() {
		fmt.Printf("  SSH clone failed; falling back to HTTPS (%s) ...\n", httpsURL)
		osRemoveAll(tempClone)
		if !runCmd([]string{"git", "clone", httpsURL, tempClone}, CmdOpts{}).OK() {
			errLog("Neovim configuration clone failed")
			return
		}
	}
	if tempClone != configDir {
		fmt.Printf("  Renaming %s to %s ...\n", filepath.Base(tempClone), filepath.Base(configDir))
		osRename(tempClone, configDir)
	}
	fmt.Printf("  Neovim configuration ready at %s\n", configDir)
}

func ghLoggedIn() bool {
	r, ok := probe([]string{"gh", "auth", "status"}, 30*time.Second)
	return ok && r.ExitCode == 0
}

func checkAndSetupSSH() {
	if !hasCmd("gh") {
		fmt.Println("\n[GitHub CLI] gh not installed — skipping authentication.")
		return
	}
	if ghLoggedIn() {
		fmt.Println("\n[GitHub CLI] Already authenticated.")
		return
	}
	if !askYN("\n[GitHub CLI] Would you like to authenticate the GitHub CLI? [y/N] ") {
		return
	}
	res := runCmd([]string{"gh", "auth", "login"}, CmdOpts{Timeout: 15 * time.Minute})
	if !res.OK() {
		errLog("gh auth login failed — skipping key upload.")
		return
	}
}

// askYN prompts on stdin. Returns true only for an exact "y" (case-insensitive).
func askYN(prompt string) bool {
	fmt.Print(prompt)
	var buf [256]byte
	n, err := stdin.Read(buf[:])
	if err != nil && err != io.EOF {
		fmt.Println()
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	return answer == "y"
}

func installAgy() {
	taskPrintln("  Installing agy via curl ...")
	if !runShell("curl -fsSL https://antigravity.google/cli/install.sh | bash", CmdOpts{Out: taskOut()}).OK() {
		errLog("agy installation failed")
	}
}

func pnpmEnvPrefix() string {
	if isMacOS {
		return `export PNPM_HOME="$HOME/Library/pnpm"; export PATH="$PNPM_HOME/bin:$PNPM_HOME:$PATH"; `
	}
	return `export PNPM_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/pnpm"; export PATH="$PNPM_HOME/bin:$PNPM_HOME:$PATH"; `
}

func installNpmPackage(pkgName string) {
	home, _ := os.UserHomeDir()
	if _, err := osStat(filepath.Join(home, ".nvm")); err != nil {
		errLog("NVM is not installed — cannot install " + pkgName)
		return
	}
	ensureNodeLTS()
	taskPrintf("  Installing %s via pnpm ...\n", pkgName)
	cmd := fmt.Sprintf(`bash -c '%ssource ~/.nvm/nvm.sh && pnpm add -g %s'`, pnpmEnvPrefix(), pkgName)
	if !runShell(cmd, CmdOpts{Out: taskOut()}).OK() {
		errLog(fmt.Sprintf("%s installation failed", pkgName))
	}
}

// installPlaywrightBrowsers runs `pnpx playwright install` (with --with-deps
// on apt-get). Separated from the npm-side install so that installNpmToolsBatch
// can do all `pnpm add -g` work in one call and then just provision browsers
// once if playwright was in the batch.
func installPlaywrightBrowsers() {
	installCmd := "pnpx playwright install"
	if pkgMgr == "apt-get" {
		fmt.Println("  Installing Playwright browsers with dependencies ...")
		installCmd += " --with-deps"
	} else {
		fmt.Println("  Installing Playwright browsers ...")
	}
	if !runShell(fmt.Sprintf(`bash -c '%ssource ~/.nvm/nvm.sh && %s'`, pnpmEnvPrefix(), installCmd), CmdOpts{}).OK() {
		errLog("playwright browser installation failed")
	}
}

func installPlaywright() {
	home, _ := os.UserHomeDir()
	if _, err := osStat(filepath.Join(home, ".nvm")); err != nil {
		errLog("NVM is not installed — cannot install playwright")
		return
	}
	ensureNodeLTS()
	taskPrintln("  Installing playwright via pnpm ...")
	addCmd := fmt.Sprintf(`bash -c '%ssource ~/.nvm/nvm.sh && pnpm add -g playwright'`, pnpmEnvPrefix())
	if !runShell(addCmd, CmdOpts{Out: taskOut()}).OK() {
		errLog("playwright installation failed")
		return
	}
	installPlaywrightBrowsers()
}

func installGHExtension(repo string) {
	if !hasCmd("gh") {
		errLog("gh CLI is not installed — cannot install extension " + repo)
		return
	}
	taskPrintf("  Installing gh extension %s ...\n", repo)
	if !runCmd([]string{"gh", "extension", "install", repo}, CmdOpts{Out: taskOut()}).OK() {
		errLog(fmt.Sprintf("gh extension install %s failed", repo))
	}
}

// ── LibreOffice AutoSave Extension ──────────────────────────────────────

func ensureLibreOfficeAutoSave() {
	var unopkgPath string
	if isMacOS {
		unopkgPath = "/Applications/LibreOffice.app/Contents/MacOS/unopkg"
		if _, err := osStat(unopkgPath); err != nil {
			return
		}
	} else {
		if !hasCmd("unopkg") {
			return
		}
		unopkgPath = "unopkg"
	}

	fmt.Println("\n[LibreOffice] LibreOffice detected. Installing AutoSave extension ...")

	tmp, err := os.MkdirTemp("", "libreoffice-autosave-")
	if err != nil {
		errLog(fmt.Sprintf("LibreOffice AutoSave temp dir failed: %v", err))
		return
	}
	defer osRemoveAll(tmp)

	url := "https://github.com/JMR-dev/LibreOfficeAutoSave/releases/latest/download/AutoSave.oxt"
	dest := filepath.Join(tmp, "AutoSave.oxt")

	if !download(url, dest) {
		errLog("Failed to download LibreOffice AutoSave extension")
		return
	}

	if !runCmd([]string{unopkgPath, "add", "-f", dest}, CmdOpts{}).OK() {
		errLog("Failed to install LibreOffice AutoSave extension")
		return
	}

	fmt.Println("  LibreOffice AutoSave extension installed successfully.")
}


