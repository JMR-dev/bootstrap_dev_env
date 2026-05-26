package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		"minikube": true, "bashtop": true, "pipx": true,
		"poetry": true, "pulumi": true,
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
	home, _ := os.UserHomeDir()
	exists := func(p string) bool { _, err := osStat(p); return err == nil }
	switch pkg {
	case "obsidian":
		return exists("/usr/local/bin/obsidian")
	case "minikube":
		return exists("/usr/local/bin/minikube") || hasCmd("minikube")
	case "bashtop":
		return exists("/usr/local/bin/bashtop") || exists(filepath.Join(home, "bashtop"))
	case "pulumi":
		return exists("/opt/pulumi/pulumi") || hasCmd("pulumi")
	case "pipx":
		return hasCmd("pipx")
	case "poetry":
		return hasCmd("poetry")
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

	matches := func(name string) bool {
		n := strings.ToLower(name)
		if !strings.HasSuffix(n, ".appimage") {
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

func installBashtop(_ string) {
	home, _ := os.UserHomeDir()
	cloneDir := filepath.Join(home, "bashtop")
	if _, err := osStat(cloneDir); err == nil {
		fmt.Printf("  Updating existing clone at %s ...\n", cloneDir)
		if !runCmd([]string{"git", "-C", cloneDir, "pull"}, CmdOpts{}).OK() {
			errLog("bashtop git pull failed")
			return
		}
	} else {
		fmt.Printf("  Cloning bashtop to %s ...\n", cloneDir)
		if !runCmd([]string{"git", "clone", "https://github.com/aristocratos/bashtop.git", cloneDir}, CmdOpts{}).OK() {
			errLog("bashtop git clone failed")
			return
		}
	}
	if !runCmd([]string{"make", "install"}, CmdOpts{AsSudo: true, Cwd: cloneDir}).OK() {
		errLog("bashtop 'make install' failed")
		return
	}
	appendProfileLine("bashtop", "export PATH=$PATH:"+cloneDir)
	fmt.Printf("  bashtop installed. Clone at %s, binary at /usr/local/bin/bashtop\n", cloneDir)
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
	case "bashtop":
		installBashtop(tmp)
	case "pulumi":
		installPulumi(tmp)
	case "pipx":
		installPipx(tmp)
	case "poetry":
		installPoetry(tmp)
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

// installSystemPackages installs the regular + special package lists.
func installSystemPackages(regular, special []string) {
	fmt.Println("\n=== System Packages ===")

	if pkgMgr == "brew" {
		for _, pkg := range regular {
			res := pkgInstall(pkg)
			if !res.OK() {
				errLog(fmt.Sprintf("System package failed to install: %s", pkg))
			}
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

	for _, pkg := range regular {
		res := pkgInstall(pkg)
		if !res.OK() {
			errLog(fmt.Sprintf("System package failed to install: %s", pkg))
		}
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
