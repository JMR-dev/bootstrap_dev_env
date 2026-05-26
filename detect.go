package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// OS / architecture detection.
//
// Vendors disagree on canonical OS/arch tokens used in download URLs, so we
// keep our own normalized values ("linux"/"macos", "x86_64"/"aarch64") and
// translate at URL-construction time.

var (
	osName       string // "linux" or "macos"
	archName     string // "x86_64" or "aarch64"
	isMacOS      bool
	isRHELFamily bool
	isArchFamily bool
)

func init() {
	osName = detectOS()
	archName = detectArch()
	isMacOS = osName == "macos"
}

func detectOS() string {
	switch runtime.GOOS {
	case "linux":
		return "linux"
	case "darwin":
		return "macos"
	default:
		fmt.Fprintf(os.Stderr, "Unsupported OS: %s (supports Linux, Darwin)\n", runtime.GOOS)
		osExit(1)
		return ""
	}
}

func detectArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		fmt.Fprintf(os.Stderr, "Unsupported architecture: %s (supports x86_64, aarch64)\n", runtime.GOARCH)
		osExit(1)
		return ""
	}
}

var (
	archGo       = map[string]string{"x86_64": "amd64", "aarch64": "arm64"}
	archMinikube = map[string]string{"x86_64": "amd64", "aarch64": "arm64"}
	archDeb      = map[string]string{"x86_64": "amd64", "aarch64": "arm64"}
	archNvim     = map[string]string{"x86_64": "x86_64", "aarch64": "arm64"}
	archPulumi   = map[string]string{"x86_64": "x64", "aarch64": "arm64"}

	osGo   = map[string]string{"linux": "linux", "macos": "darwin"}
	osZig  = map[string]string{"linux": "linux", "macos": "macos"}
	osNvim = map[string]string{"linux": "linux", "macos": "macos"}

	archTokens = map[string][]string{
		"x86_64":  {"x86_64", "amd64", "x64"},
		"aarch64": {"aarch64", "arm64"},
	}
)

// formatURL interpolates {version}, {arch}, {arch_go}, {os}, {os_go},
// {os_zig}, {os_nvim} into a download URL template.
func formatURL(template, version string) string {
	r := strings.NewReplacer(
		"{version}", version,
		"{arch}", archName,
		"{arch_go}", archGo[archName],
		"{os}", osName,
		"{os_go}", osGo[osName],
		"{os_zig}", osZig[osName],
		"{os_nvim}", osNvim[osName],
	)
	return r.Replace(template)
}

func otherArch() string {
	if archName == "x86_64" {
		return "aarch64"
	}
	return "x86_64"
}

func archMatches(name, arch string) bool {
	n := strings.ToLower(name)
	for _, tok := range archTokens[arch] {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}

func hasOtherArchToken(name string) bool {
	n := strings.ToLower(name)
	for _, tok := range archTokens[otherArch()] {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}

// osReleaseField returns the value of /etc/os-release's NAME=value pair,
// stripped of surrounding quotes. Returns "" if the file is missing or the
// field is absent.
func osReleaseField(field string) string {
	data, err := osReadFile(osReleasePath)
	if err != nil {
		return ""
	}
	prefix := field + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), `"`)
		}
	}
	return ""
}

func detectRHELFamily() bool {
	data, err := osReadFile(osReleasePath)
	if err != nil {
		return pkgMgr == "dnf"
	}
	tokens := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") || strings.HasPrefix(line, "ID_LIKE=") {
			_, val, _ := strings.Cut(line, "=")
			val = strings.Trim(val, `"`)
			tokens = append(tokens, strings.Fields(val)...)
		}
	}
	rhel := map[string]bool{"rhel": true, "fedora": true, "centos": true, "rocky": true, "almalinux": true}
	for _, t := range tokens {
		if rhel[t] {
			return true
		}
	}
	return false
}

func detectArchFamily() bool {
	data, err := osReadFile(osReleasePath)
	if err != nil {
		return pkgMgr == "pacman"
	}
	tokens := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") || strings.HasPrefix(line, "ID_LIKE=") {
			_, val, _ := strings.Cut(line, "=")
			val = strings.Trim(val, `"`)
			tokens = append(tokens, strings.Fields(val)...)
		}
	}
	arch := map[string]bool{"arch": true, "manjaro": true, "endeavouros": true, "artix": true}
	for _, t := range tokens {
		if arch[t] {
			return true
		}
	}
	return false
}
