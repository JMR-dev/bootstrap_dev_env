package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var pkgMgr string // "dnf", "apt-get", "pacman", "brew"

func initPkgMgr() {
	pkgMgr = detectPkgMgr()
	isRHELFamily = detectRHELFamily()
	isArchFamily = detectArchFamily()
	ensureWhichInstalled()
}

func ensureWhichInstalled() {
	if hasCmd("which") {
		return
	}
	fmt.Println("[which] 'which' is not installed. Installing it as a prerequisite...")
	var res CmdResult
	switch pkgMgr {
	case "pacman":
		res = runCmd([]string{"pacman", "-Sy", "--noconfirm", "--needed", "which"}, CmdOpts{AsSudo: true})
	case "brew":
		res = runCmd([]string{"brew", "install", "which"}, CmdOpts{})
	default:
		res = runCmd([]string{pkgMgr, "install", "-y", "which"}, CmdOpts{AsSudo: true})
	}
	if !res.OK() {
		fmt.Fprintf(os.Stderr, "Warning: failed to install 'which' prerequisite: %v\n", res.Err)
	} else {
		fmt.Println("[which] 'which' successfully installed.")
	}
}


func detectPkgMgr() string {
	if isMacOS {
		// brew may not be installed yet — ensureHomebrew runs before any
		// call that actually invokes brew.
		return "brew"
	}
	for _, mgr := range []string{"dnf", "apt-get", "pacman"} {
		if hasCmd(mgr) {
			return mgr
		}
	}
	fmt.Fprintln(os.Stderr, "No supported package manager found (expected dnf, apt-get, pacman, or brew on macOS).")
	osExit(1)
	return ""
}

// pkgOverrides maps a (PKG_MGR, generic_name) pair to a distro-specific
// replacement. An empty []string{} means "skip with a warning".
//
// Use overrideEntry to distinguish "skip" (Skip=true) from "replace with
// these packages" (Replacement=[...]).
type overrideEntry struct {
	Skip        bool
	Replacement []string
}

func skipOverride() overrideEntry { return overrideEntry{Skip: true} }
func replace(names ...string) overrideEntry {
	return overrideEntry{Replacement: names}
}

var packageOverrides = map[string]map[string]overrideEntry{
	"dnf": {
		"build-essential": replace("gcc", "gcc-c++", "make"),
		"rg":              replace("ripgrep"),
		"docker-compose":  skipOverride(),
		"webcamoid":       skipOverride(),
	},
	"apt-get": {
		"fd":             replace("fd-find"),
		"ffmpeg-free":    replace("ffmpeg"),
		"lua":            replace("lua5.4"),
		"qemu":           replace("qemu-system"),
		"rg":             replace("ripgrep"),
		"bzip2-devel":    replace("libbz2-dev"),
		"gdbm-libs":      replace("libgdbm-dev"),
		"libffi-devel":   replace("libffi-dev"),
		"libnsl2":        replace("libnsl-dev"),
		"libuuid-devel":  replace("uuid-dev"),
		"libxml2-devel":  replace("libxml2-dev"),
		"libzstd-devel":  replace("libzstd-dev"),
		"ncurses-devel":  replace("libncursesw5-dev"),
		"openssl-devel":  replace("libssl-dev"),
		"readline-devel": replace("libreadline-dev"),
		"sqlite":         replace("sqlite3"),
		"sqlite-devel":   replace("libsqlite3-dev"),
		"tk-devel":       replace("tk-dev"),
		"xmlsec1-devel":  replace("libxmlsec1-dev"),
		"xz":             replace("xz-utils"),
		"xz-devel":       replace("liblzma-dev"),
		"zlib-devel":     replace("zlib1g-dev"),
	},
	"pacman": {
		"build-essential":           replace("base-devel"),
		"ansible-core":              skipOverride(), // bundled with ansible
		"containerd.io":             replace("containerd"),
		"docker-ce":                 replace("docker"),
		"docker-ce-cli":             skipOverride(), // covered by docker
		"docker-ce-rootless-extras": skipOverride(), // AUR-only
		"pipx":                      replace("python-pipx"),
		"docker-buildx-plugin":      replace("docker-buildx"),
		"docker-compose-plugin":     replace("docker-compose"),
		"dotnet-sdk-10.0":           replace("dotnet-sdk"),
		"ffmpeg-free":               replace("ffmpeg"),
		"gh":                        replace("github-cli"),
		"github-desktop":            skipOverride(), // AUR-only
		"google-chrome-stable":      skipOverride(), // AUR-only
		"lua":                       replace("lua"),
		"obs-studio":                replace("obs-studio"),
		"obsidian":                  skipOverride(), // AUR-only; provided via Flatpak when --gui
		"pulumi":                    skipOverride(), // AUR-only; installed via custom path
		"qemu":                      replace("qemu-full"),
		"rg":                        replace("ripgrep"),
		"shutter":                   skipOverride(), // AUR-only
		"temurin-25-jdk":            replace("jdk-openjdk"),
		"vagrant":                   skipOverride(), // AUR-only
		"vivaldi-stable":            replace("vivaldi"),
		"webcamoid":                 skipOverride(), // AUR-only; provided via Flatpak when --gui
		"wireshark":                 replace("wireshark-qt"),
		"yt-dlp":                    replace("yt-dlp"),
		"zoom":                      skipOverride(), // AUR-only
		"bzip2-devel":               skipOverride(),
		"gdbm-libs":                 replace("gdbm"),
		"libffi-devel":              replace("libffi"),
		"libnsl2":                   replace("libnsl"),
		"libuuid-devel":             replace("util-linux-libs"),
		"libxml2-devel":             replace("libxml2"),
		"libzstd-devel":             replace("zstd"),
		"ncurses-devel":             replace("ncurses"),
		"openssl-devel":             replace("openssl"),
		"readline-devel":            replace("readline"),
		"sqlite-devel":              skipOverride(),
		"tk-devel":                  replace("tk"),
		"xmlsec1-devel":             replace("xmlsec"),
		"xz-devel":                  skipOverride(),
		"zlib-devel":                replace("zlib"),
	},
	"brew": {
		"build-essential":           skipOverride(),
		"gcc":                       skipOverride(),
		"make":                      skipOverride(),
		"patch":                     skipOverride(),
		"zsh":                       skipOverride(),
		"ansible-core":              skipOverride(),
		"containerd.io":             skipOverride(),
		"docker-buildx-plugin":      skipOverride(),
		"docker-ce-cli":             skipOverride(),
		"docker-ce-rootless-extras": skipOverride(),
		"docker-ce":                 replace("docker"),
		"docker-compose-plugin":     replace("docker-compose"),
		"dotnet-sdk-10.0":           replace("dotnet"),
		"ffmpeg-free":               replace("ffmpeg"),
		"github-desktop":            replace("github"),
		"google-chrome-stable":      replace("google-chrome"),
		"obs-studio":                replace("obs"),
		"rg":                        replace("ripgrep"),
		"temurin-25-jdk":            replace("temurin"),
		"vivaldi-stable":            replace("vivaldi"),
		"buildah":                   skipOverride(),
		"shutter":                   skipOverride(),
		"virt-manager":              skipOverride(),
		"webcamoid":                 skipOverride(),
		"bzip2-devel":               skipOverride(),
		"curl":                      skipOverride(),
		"gdbm-libs":                 replace("gdbm"),
		"libffi-devel":              replace("libffi"),
		"libnsl2":                   skipOverride(),
		"libuuid-devel":             skipOverride(),
		"libxml2-devel":             replace("libxml2"),
		"libzstd-devel":             replace("zstd"),
		"ncurses-devel":             skipOverride(),
		"openssl-devel":             replace("openssl@3"),
		"readline-devel":            replace("readline"),
		"sqlite-devel":              skipOverride(),
		"tk-devel":                  replace("tcl-tk"),
		"xmlsec1-devel":             replace("libxmlsec1"),
		"xz":                        replace("xz"),
		"xz-devel":                  skipOverride(),
		"zlib-devel":                skipOverride(),
	},
}

// brewCasks: brew packages that must be installed with `brew install --cask`.
// Names are post-override.
var brewCasks = map[string]bool{
	"docker":        true,
	"github":        true,
	"google-chrome": true,
	"obs":           true,
	"obsidian":      true,
	"temurin":       true,
	"vagrant":       true,
	"vivaldi":       true,
	"zoom":          true,
}

// resolveSystemPkgs applies distro overrides. Returns (resolved, skipped).
func resolveSystemPkgs(names []string) ([]string, []string) {
	overrides := packageOverrides[pkgMgr]
	var resolved, skipped []string
	for _, pkg := range names {
		ov, ok := overrides[pkg]
		if !ok {
			resolved = append(resolved, pkg)
			continue
		}
		if ov.Skip {
			skipped = append(skipped, pkg)
			continue
		}
		resolved = append(resolved, ov.Replacement...)
	}
	return resolved, skipped
}

func installSystemPackages(pkgs []string, special []string) {
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

// isSystemPkgInstalled queries the host package manager.
func isSystemPkgInstalled(pkg string) bool {
	switch pkgMgr {
	case "dnf":
		r, ok := probe([]string{"rpm", "-q", pkg}, 0)
		return ok && r.ExitCode == 0
	case "apt-get":
		r, ok := probe([]string{"dpkg-query", "-W", "-f=${Status}", pkg}, 0)
		return ok && strings.Contains(string(r.Stdout), "install ok installed")
	case "pacman":
		r, ok := probe([]string{"pacman", "-Qi", pkg}, 0)
		return ok && r.ExitCode == 0
	case "brew":
		if !hasCmd("brew") {
			return false
		}
		for _, kind := range []string{"--formula", "--cask"} {
			r, ok := probe([]string{"brew", "list", kind, pkg}, 60*time.Second)
			if ok && r.ExitCode == 0 {
				return true
			}
		}
		return false
	}
	return false
}

func isFlatpakInstalled(appID string) bool {
	if !hasCmd("flatpak") {
		return false
	}
	r, ok := probe([]string{"flatpak", "info", appID}, 0)
	return ok && r.ExitCode == 0
}
