package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Special packages: installed outside the regular package manager because
// they're not in standard repos, or because they need extra setup. Linux only;
// on macOS brew covers all of these.
func specialPkgs() map[string]bool {
	if isMacOS {
		return map[string]bool{}
	}
	return map[string]bool{
		"github-desktop": true, "zoom": true, "obsidian": true,
		"minikube": true, "pipx": true,
		"poetry": true, "pulumi": true, "semgrep": true,
		"nvidia-drivers": true, "cuda-toolkit": true,
		"nvidia-container-toolkit": true, "ollama": true,
		"huggingface-cli": true,
	}
}

// guiSystemPkgs are skipped by default (headless mode) and included only
// when --gui is passed.
var guiSystemPkgs = map[string]bool{
	"github-desktop":       true,
	"google-chrome-stable": true,
	"obs-studio":           true,
	"obsidian":             true,
	"shutter":              true,
	"virt-manager":         true,
	"vivaldi-stable":       true,
	"webcamoid":            true,
	"wireshark":            true,
	"zoom":                 true,
}

func isSpecialPkgInstalled(pkg string) bool {
	exists := func(p string) bool { _, err := osStat(p); return err == nil }
	switch pkg {
	case "obsidian":
		return exists("/usr/local/bin/obsidian")
	case "minikube":
		return exists("/usr/local/bin/minikube") || hasCmd("minikube")
	case "pulumi":
		return exists("/opt/pulumi/pulumi") || hasCmd("pulumi")
	case "pipx":
		return hasCmd("pipx")
	case "poetry":
		return hasCmd("poetry")
	case "semgrep":
		return hasCmd("semgrep")
	case "nvidia-drivers":
		return hasCmd("nvidia-smi")
	case "cuda-toolkit":
		return hasCmd("nvcc") || exists("/usr/local/cuda/bin/nvcc")
	case "nvidia-container-toolkit":
		return hasCmd("nvidia-ctk")
	case "ollama":
		return hasCmd("ollama")
	case "huggingface-cli":
		return hasCmd("huggingface-cli")
	}
	return isSystemPkgInstalled(pkg)
}

// ── special installers ────────────────────────────────────────────────────

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

func installGitHubDesktop(tmp string) {
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/shiftkey/desktop/releases/latest", &rel) {
		return
	}
	var suffix string
	switch pkgMgr {
	case "dnf":
		suffix = ".rpm"
	case "apt-get":
		suffix = ".deb"
	default:
		warn("github-desktop has no installer for this package manager — skipping")
		return
	}
	hostTokens := archTokens[archName]
	excludeTokens := archTokens[otherArch()]

	matches := func(name string) bool {
		n := strings.ToLower(name)
		if !strings.HasSuffix(n, suffix) {
			return false
		}
		matched := false
		for _, t := range hostTokens {
			if strings.Contains(n, t) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
		for _, t := range excludeTokens {
			inHost := false
			for _, h := range hostTokens {
				if h == t {
					inHost = true
					break
				}
			}
			if !inHost && strings.Contains(n, t) {
				return false
			}
		}
		return true
	}

	var asset *ghAsset
	for i := range rel.Assets {
		if matches(rel.Assets[i].Name) {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		errLog(fmt.Sprintf("No GitHub Desktop %s asset found for %s", suffix, archName))
		return
	}
	dest := filepath.Join(tmp, asset.Name)
	if !download(asset.BrowserDownloadURL, dest) {
		return
	}
	installer := pkgMgr
	if pkgMgr == "apt-get" {
		installer = "apt-get"
	}
	runCmd([]string{installer, "install", "-y", dest}, CmdOpts{AsSudo: true})
}

func installZoom(tmp string) {
	if archName != "x86_64" {
		warn("Zoom has no aarch64 Linux client — skipping")
		return
	}
	switch pkgMgr {
	case "dnf":
		dest := filepath.Join(tmp, "zoom.rpm")
		if !download("https://zoom.us/client/latest/zoom_x86_64.rpm", dest) {
			return
		}
		runCmd([]string{"dnf", "install", "-y", dest}, CmdOpts{AsSudo: true})
	case "apt-get":
		dest := filepath.Join(tmp, "zoom.deb")
		if !download("https://zoom.us/client/latest/zoom_amd64.deb", dest) {
			return
		}
		runCmd([]string{"apt-get", "install", "-y", dest}, CmdOpts{AsSudo: true})
	default:
		warn("zoom: no installer for this distro — skipping")
	}
}

func installObsidian(tmp string) {
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest", &rel) {
		return
	}
	hostTokens := archTokens[archName]
	otherTokens := archTokens[otherArch()]

	hasAnyToken := func(n string, tokens []string) bool {
		for _, t := range tokens {
			if strings.Contains(n, t) {
				return true
			}
		}
		return false
	}

	matches := func(name string) bool {
		n := strings.ToLower(name)
		if !strings.HasSuffix(n, ".appimage") {
			return false
		}
		// Obsidian publishes the x86_64 AppImage without an arch suffix
		// (e.g. "Obsidian-1.12.7.AppImage") and the arm64 build as
		// "Obsidian-1.12.7-arm64.AppImage". Treat a token-less AppImage as x86_64.
		if !hasAnyToken(n, hostTokens) && !hasAnyToken(n, otherTokens) {
			return archName == "x86_64"
		}
		matched := hasAnyToken(n, hostTokens)
		if !matched {
			return false
		}
		for _, t := range otherTokens {
			inHost := false
			for _, h := range hostTokens {
				if h == t {
					inHost = true
					break
				}
			}
			if !inHost && strings.Contains(n, t) {
				return false
			}
		}
		return true
	}

	var asset *ghAsset
	for i := range rel.Assets {
		if matches(rel.Assets[i].Name) {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		errLog(fmt.Sprintf("No Obsidian AppImage found for %s", archName))
		return
	}
	dest := filepath.Join(tmp, asset.Name)
	if !download(asset.BrowserDownloadURL, dest) {
		return
	}
	installPath := "/usr/local/bin/obsidian"
	runCmd([]string{"cp", dest, installPath}, CmdOpts{AsSudo: true})
	runCmd([]string{"chmod", "755", installPath}, CmdOpts{AsSudo: true})
	fmt.Printf("  Obsidian AppImage installed at %s\n", installPath)
}

func installMinikube(tmp string) {
	archTok := archMinikube[archName]
	baseURL := fmt.Sprintf("https://storage.googleapis.com/minikube/releases/latest/minikube-linux-%s", archTok)
	dest := filepath.Join(tmp, "minikube")
	if !download(baseURL, dest) {
		return
	}
	fmt.Println("  Fetching SHA256 ...")
	shaText := fetchText(baseURL + ".sha256")
	if shaText == "" {
		return
	}
	expected := strings.Fields(shaText)[0]
	actual, err := sha256Of(dest)
	if err != nil {
		errLog(fmt.Sprintf("minikube hash failed: %v", err))
		return
	}
	if actual != expected {
		errLog(fmt.Sprintf("minikube SHA256 mismatch: expected %s, got %s", expected, actual))
		return
	}
	fmt.Println("  SHA256 OK")
	installPath := "/usr/local/bin/minikube"
	runCmd([]string{"cp", dest, installPath}, CmdOpts{AsSudo: true})
	runCmd([]string{"chmod", "755", installPath}, CmdOpts{AsSudo: true})
	fmt.Printf("  minikube installed to %s\n", installPath)
}

func installPulumi(tmp string) {
	version := fetchText("https://www.pulumi.com/latest-version")
	if version == "" {
		errLog("Could not determine latest Pulumi version")
		return
	}
	osTok := osGo[osName]
	archTok := archPulumi[archName]
	tarball := fmt.Sprintf("pulumi-v%s-%s-%s.tar.gz", version, osTok, archTok)
	base := fmt.Sprintf("https://github.com/pulumi/pulumi/releases/download/v%s", version)
	dest := filepath.Join(tmp, tarball)
	if !download(base+"/"+tarball, dest) {
		return
	}

	checksums := fetchText(fmt.Sprintf("%s/pulumi-%s-checksums.txt", base, version))
	if checksums == "" {
		errLog("Could not fetch Pulumi checksums")
		return
	}
	var expected string
	for _, line := range strings.Split(checksums, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), tarball) {
			expected = strings.Fields(line)[0]
			break
		}
	}
	if expected == "" {
		errLog(fmt.Sprintf("No checksum entry for %s", tarball))
		return
	}
	actual, err := sha256Of(dest)
	if err != nil {
		errLog(fmt.Sprintf("Pulumi hash failed: %v", err))
		return
	}
	if actual != expected {
		errLog(fmt.Sprintf("Pulumi SHA256 mismatch: expected %s, got %s", expected, actual))
		return
	}
	fmt.Println("  SHA256 OK")

	installDir := "/opt/pulumi"
	fmt.Println("  Extracting Pulumi to /opt ...")
	runCmd([]string{"mkdir", "-p", "/opt"}, CmdOpts{AsSudo: true})
	runCmd([]string{"rm", "-rf", installDir}, CmdOpts{AsSudo: true})
	runCmd([]string{"tar", "-C", "/opt", "-xzf", dest}, CmdOpts{AsSudo: true})

	appendProfileLine("pulumi", fmt.Sprintf(`export PATH="$PATH:%s"`, installDir))
	fmt.Printf("  Pulumi %s installed to %s\n", version, installDir)
}

func installPipx(_ string) {
	if !hasCmd("python3") {
		errLog("Python 3 is not installed — cannot install pipx")
		return
	}
	pkgInstall("pipx")
	if hasCmd("pipx") {
		runCmd([]string{"pipx", "ensurepath"}, CmdOpts{})
	} else {
		errLog("pipx command not found after install")
	}
}

func installPoetry(_ string) {
	if !hasCmd("pipx") {
		errLog("pipx is not installed — cannot install poetry")
		return
	}
	runCmd([]string{"pipx", "install", "poetry"}, CmdOpts{})
}

func installSemgrep(_ string) {
	if !hasCmd("pipx") {
		errLog("pipx is not installed — cannot install semgrep")
		return
	}
	runCmd([]string{"pipx", "install", "semgrep"}, CmdOpts{})
}

func installSpecialPkg(pkg, tmp string) {
	switch pkg {
	case "github-desktop":
		installGitHubDesktop(tmp)
	case "zoom":
		installZoom(tmp)
	case "obsidian":
		installObsidian(tmp)
	case "minikube":
		installMinikube(tmp)
	case "pulumi":
		installPulumi(tmp)
	case "pipx":
		installPipx(tmp)
	case "poetry":
		installPoetry(tmp)
	case "semgrep":
		installSemgrep(tmp)
	case "nvidia-drivers":
		installNvidiaDrivers()
	case "cuda-toolkit":
		installCUDAToolkit()
	case "nvidia-container-toolkit":
		installNvidiaContainerToolkit()
	case "ollama":
		installOllama()
	case "huggingface-cli":
		installHuggingFaceCLI()
	}
}

func installNvidiaDrivers() {
	if pkgMgr != "apt-get" {
		errLog("nvidia-drivers can only be automatically installed via apt-get on Ubuntu/Debian")
		return
	}
	fmt.Println("  Installing NVIDIA driver (nvidia-driver-550) ...")
	res := pkgInstall("nvidia-driver-550")
	if !res.OK() {
		errLog(fmt.Sprintf("failed to install nvidia-driver-550: %v", res.Err))
	}
}

func installCUDAToolkit() {
	if pkgMgr != "apt-get" {
		errLog("cuda-toolkit can only be automatically installed via apt-get on Ubuntu/Debian")
		return
	}
	fmt.Println("  Setting up CUDA repository keyring ...")
	setupCUDARepo()
	fmt.Println("  Installing cuda-toolkit ...")
	res := pkgInstall("cuda-toolkit")
	if !res.OK() {
		errLog(fmt.Sprintf("failed to install cuda-toolkit: %v", res.Err))
		return
	}
	fmt.Println("  Configuring system path for CUDA ...")
	appendProfileLine("cuda", `export PATH="/usr/local/cuda/bin:$PATH"`)
	appendProfileLine("cuda", `export LD_LIBRARY_PATH="/usr/local/cuda/lib64:$LD_LIBRARY_PATH"`)
}

func installNvidiaContainerToolkit() {
	if pkgMgr != "apt-get" {
		errLog("nvidia-container-toolkit can only be automatically installed via apt-get on Ubuntu/Debian")
		return
	}
	fmt.Println("  Setting up NVIDIA Container Toolkit repository ...")
	setupNvidiaContainerToolkitRepo()
	fmt.Println("  Installing nvidia-container-toolkit ...")
	res := pkgInstall("nvidia-container-toolkit")
	if !res.OK() {
		errLog(fmt.Sprintf("failed to install nvidia-container-toolkit: %v", res.Err))
		return
	}
	fmt.Println("  Configuring Docker runtime for NVIDIA Container Toolkit ...")
	configureRes := runCmd([]string{"nvidia-ctk", "runtime", "configure", "--runtime=docker"}, CmdOpts{AsSudo: true})
	if !configureRes.OK() {
		warn(fmt.Sprintf("failed to configure docker runtime: %v", configureRes.Err))
	}
	fmt.Println("  Restarting Docker service ...")
	restartRes := runCmd([]string{"systemctl", "restart", "docker"}, CmdOpts{AsSudo: true})
	if !restartRes.OK() {
		warn(fmt.Sprintf("failed to restart docker service: %v", restartRes.Err))
	}
}

func installOllama() {
	fmt.Println("  Installing Ollama via official install script ...")
	res := runShell("curl -fsSL https://ollama.com/install.sh | sh", CmdOpts{})
	if !res.OK() {
		errLog(fmt.Sprintf("Ollama installation failed: %v", res.Err))
		return
	}

	isCI := os.Getenv("BOOTSTRAP_CI") == "true"
	if isCI {
		fmt.Println("  [CI] Skipping pulling large Ollama models in integration test container.")
		return
	}

	// Pull Gemma 4 E4B and Qwen2.5-Coder 7B
	fmt.Println("  Pulling Gemma 4 E4B and Qwen2.5-Coder 7B models ...")

	serverRunning := false

	// Check if Ollama server is already responding
	for i := 0; i < 5; i++ {
		r := runCmd([]string{"ollama", "list"}, CmdOpts{Capture: true})
		if r.OK() {
			serverRunning = true
			break
		}
		if isTesting {
			break
		}
		time.Sleep(1 * time.Second)
	}

	var cmd *exec.Cmd
	if !serverRunning && !isTesting {
		fmt.Println("  Ollama server not running. Starting in background for model pulling ...")
		cmd = exec.Command("ollama", "serve")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			warn(fmt.Sprintf("Failed to start Ollama server in background: %v", err))
		} else {
			// Wait up to 30 seconds for server to start
			for i := 0; i < 30; i++ {
				r := runCmd([]string{"ollama", "list"}, CmdOpts{Capture: true})
				if r.OK() {
					serverRunning = true
					break
				}
				time.Sleep(1 * time.Second)
			}
		}
	}

	if serverRunning || isTesting {
		fmt.Println("  Pulling Gemma 4 E4B (gemma4:e4b) ...")
		pull1 := runCmd([]string{"ollama", "pull", "gemma4:e4b"}, CmdOpts{})
		if !pull1.OK() {
			errLog(fmt.Sprintf("Failed to pull gemma4:e4b: %v", pull1.Err))
		}
		fmt.Println("  Pulling Qwen2.5-Coder 7B (qwen2.5-coder:7b) ...")
		pull2 := runCmd([]string{"ollama", "pull", "qwen2.5-coder:7b"}, CmdOpts{})
		if !pull2.OK() {
			errLog(fmt.Sprintf("Failed to pull qwen2.5-coder:7b: %v", pull2.Err))
		}
	} else {
		errLog("Ollama server failed to start — cannot pull models")
	}

	if cmd != nil && cmd.Process != nil {
		fmt.Println("  Stopping background Ollama server ...")
		cmd.Process.Kill()
	}
}

func installHuggingFaceCLI() {
	if !hasCmd("pipx") {
		errLog("pipx is not installed — cannot install huggingface-cli")
		return
	}
	fmt.Println("  Installing huggingface-cli via pipx ...")
	res := runCmd([]string{"pipx", "install", "huggingface_hub[cli]"}, CmdOpts{})
	if !res.OK() {
		errLog(fmt.Sprintf("failed to install huggingface-cli: %v", res.Err))
	}
}

// pkgInstall invokes the host package manager to install a single name.
// Centralized so pacman's "--noconfirm" doesn't leak everywhere.
func pkgInstall(pkg string) CmdResult {
	switch pkgMgr {
	case "pacman":
		return runCmd([]string{"pacman", "-S", "--noconfirm", "--needed", pkg}, CmdOpts{AsSudo: true})
	case "brew":
		if brewCasks[pkg] {
			return runCmd([]string{"brew", "install", "--cask", pkg}, CmdOpts{})
		}
		return runCmd([]string{"brew", "install", pkg}, CmdOpts{})
	default:
		return runCmd([]string{pkgMgr, "install", "-y", pkg}, CmdOpts{AsSudo: true})
	}
}

// pkgInstallMany installs all named packages in a single invocation of the
// host package manager. This is dramatically faster than per-package install
// loops because apt/dnf/pacman/brew amortize metadata refresh, dependency
// resolution, and (most importantly) only acquire the install lock once.
//
// On batch failure we fall back to per-package installs so callers can
// continue to report which specific packages failed via errLog. brew is
// split into formula vs cask batches because `--cask` is mutually exclusive
// with formula installs in one invocation. We deliberately do NOT run brew
// invocations in parallel — brew acquires per-Cellar locks on transitive
// dependencies (cmake, ninja, libsodium, etc.), and concurrent invocations
// that both pull in the same dep abort with "process has already locked".
func pkgInstallMany(pkgs []string) (failed []string) {
	if len(pkgs) == 0 {
		return nil
	}
	if pkgMgr == "brew" {
		return brewInstallMany(pkgs)
	}
	var argv []string
	switch pkgMgr {
	case "pacman":
		argv = append([]string{"pacman", "-S", "--noconfirm", "--needed"}, pkgs...)
	default:
		argv = append([]string{pkgMgr, "install", "-y"}, pkgs...)
	}
	if runCmd(argv, CmdOpts{AsSudo: true}).OK() {
		return nil
	}
	// Batch failed — retry per-package so we can report exactly which
	// packages broke. Slower, but only happens on the error path.
	warn(fmt.Sprintf("Batched install failed; retrying %d packages individually to isolate failures ...", len(pkgs)))
	for _, p := range pkgs {
		if !pkgInstall(p).OK() {
			failed = append(failed, p)
		}
	}
	return failed
}

// brewInstallMany installs pkgs via brew, batching formulas and casks into
// two single invocations (`brew install f1 f2 …` and `brew install --cask
// c1 c2 …`). Brew resolves and parallelizes the internal dep graph itself,
// so a single batched call is both faster and lock-safe — multiple
// concurrent `brew install` processes deadlock on shared deps. On batch
// failure we retry per-package serially to identify which specific package
// broke.
func brewInstallMany(pkgs []string) (failed []string) {
	var formulas, casks []string
	for _, p := range pkgs {
		if brewCasks[p] {
			casks = append(casks, p)
		} else {
			formulas = append(formulas, p)
		}
	}
	tryBatch := func(label string, names []string, extra ...string) (batchFailed []string) {
		if len(names) == 0 {
			return nil
		}
		argv := append([]string{"brew", "install"}, extra...)
		argv = append(argv, names...)
		if runCmd(argv, CmdOpts{}).OK() {
			return nil
		}
		warn(fmt.Sprintf("Batched brew %s install failed; retrying %d packages individually ...", label, len(names)))
		for _, p := range names {
			if !pkgInstall(p).OK() {
				batchFailed = append(batchFailed, p)
			}
		}
		return batchFailed
	}
	failed = append(failed, tryBatch("formula", formulas)...)
	failed = append(failed, tryBatch("cask", casks, "--cask")...)
	return failed
}

// installSystemPackages installs the regular + special package lists.
func installSystemPackages(regular, special []string) {
	fmt.Println("\n=== System Packages ===")

	// Ensure aria2 is installed first and on the system path
	var installAria2 bool
	var remainingRegular []string
	for _, p := range regular {
		if p == "aria2" {
			installAria2 = true
		} else {
			remainingRegular = append(remainingRegular, p)
		}
	}

	if installAria2 || !hasCmd("aria2c") {
		fmt.Println("  Ensuring aria2 is installed first and on the system path ...")
		var res CmdResult
		switch pkgMgr {
		case "brew":
			res = runCmd([]string{"brew", "install", "aria2"}, CmdOpts{})
		case "pacman":
			res = runCmd([]string{"pacman", "-S", "--noconfirm", "--needed", "aria2"}, CmdOpts{AsSudo: true})
		default: // dnf, apt-get
			res = runCmd([]string{pkgMgr, "install", "-y", "aria2"}, CmdOpts{AsSudo: true})
		}
		if !res.OK() {
			warn(fmt.Sprintf("Failed to install aria2: %v", res.Err))
		} else if !hasCmd("aria2c") {
			warn("aria2 was installed but 'aria2c' is not found on the system path")
		} else {
			fmt.Println("  aria2 is installed and on the system path.")
		}
		regular = remainingRegular
	}

	if pkgMgr == "brew" {
		failed := pkgInstallMany(regular)
		for _, p := range failed {
			taskPrintf("  [WARN] System package failed to install: %s\n", p)
		}
		// No special packages on macOS — brew covers all of them.
		return
	}

	seenRepos := map[int]bool{}
	groups := repoGroups()
	for _, pkg := range regular {
		for i, g := range groups {
			if g.members[pkg] && !seenRepos[i] {
				fmt.Printf("  [REPO] Setting up repository for %s ...\n", pkg)
				g.setup()
				seenRepos[i] = true
			}
		}
	}

	failed := pkgInstallMany(regular)
	for _, p := range failed {
		taskPrintf("  [WARN] System package failed to install: %s\n", p)
	}

	if len(special) > 0 {
		tmp, err := os.MkdirTemp("", "bootstrap-special-")
		if err != nil {
			errLog(fmt.Sprintf("could not create temp dir for special packages: %v", err))
			return
		}
		defer osRemoveAll(tmp)
		for _, pkg := range special {
			fmt.Printf("\n  [SPECIAL] Installing %s ...\n", pkg)
			installSpecialPkg(pkg, tmp)
		}
	}

	if pkgMgr == "apt-get" && hasCmd("fdfind") {
		runCmd([]string{"ln", "-sf", "/usr/bin/fdfind", "/usr/local/bin/fd"}, CmdOpts{AsSudo: true})
	}
}

// appendProfileLine adds a PATH/env line to a system-wide login-shell profile,
// idempotently. On Linux uses /etc/profile.d/<name>.sh; macOS uses /etc/zprofile.
func appendProfileLine(scriptName, line string) {
	target := fmt.Sprintf("/etc/profile.d/%s.sh", scriptName)
	if isMacOS {
		target = "/etc/zprofile"
	}
	cmd := fmt.Sprintf("grep -qxF %q %s 2>/dev/null || echo %q >> %s", line, target, line, target)
	runCmd([]string{"bash", "-c", cmd}, CmdOpts{AsSudo: true})
}
