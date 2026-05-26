package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// resolveURL returns the formatted download URL or "" if no template is set.
func (p *CustomPackage) resolveURL() string {
	if p.URLTemplate == "" {
		return ""
	}
	return formatURL(p.URLTemplate, p.Version)
}

func (p *CustomPackage) resolveSHA256URL() string {
	if p.SHA256URLTemplate == "" {
		return ""
	}
	return formatURL(p.SHA256URLTemplate, p.Version)
}

func (p *CustomPackage) resolvedSHA256() string {
	if p.SHA256 != "" {
		return strings.ToLower(p.SHA256)
	}
	if p.SHA256Map != nil {
		key := osName + "-" + archName
		if v, ok := p.SHA256Map[key]; ok {
			return strings.ToLower(v)
		}
	}
	return ""
}

func (p *CustomPackage) displayName() string {
	if p.Version != "" {
		return p.Name + "-" + p.Version
	}
	return p.Name
}

var defaultInstallPaths = map[string]string{
	"go":          "/usr/local/go",
	"firecracker": "/usr/local/bin/firecracker",
	"zig":         "/usr/local/bin/zig",
	"nvm":         "~/.nvm",
	"pyenv":       "~/.pyenv",
	"neovim":      "/usr/local/bin/nvim",
	"oh-my-zsh":   "~/.oh-my-zsh",
	"agy":               "~/.local/bin/agy",
	"gh-repo-bootstrap": "~/.local/share/gh/extensions/gh-repo-bootstrap",
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func defaultInstallPath(pkg *CustomPackage) string {
	if p, ok := defaultInstallPaths[strings.ToLower(pkg.Name)]; ok {
		return p
	}
	return ""
}

func pipInstalled() bool {
	if !hasCmd("python3") {
		return false
	}
	r, ok := probe([]string{"python3", "-m", "pip", "--version"}, 0)
	return ok && r.ExitCode == 0
}

// isCustomPkgInstalled returns (installed, checkPath). pip ships inside the
// Python distribution rather than at a fixed path, so it's detected with
// `python3 -m pip --version`.
func npmInstalled(cmdName string) (bool, string) {
	if hasCmd(cmdName) {
		if p, err := exec.LookPath(cmdName); err == nil {
			return true, p
		}
		return true, ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".local/share/pnpm/bin", cmdName))
	if len(matches) > 0 {
		return true, matches[0]
	}
	matches, _ = filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin", cmdName))
	if len(matches) > 0 {
		return true, matches[0]
	}
	return false, ""
}

func isCustomPkgInstalled(pkg *CustomPackage) (bool, string) {
	name := strings.ToLower(pkg.Name)
	if name == "pip" {
		return pipInstalled(), ""
	}
	if name == "claude" {
		return npmInstalled("claude")
	}
	if name == "codex" {
		return npmInstalled("codex")
	}
	if name == "copilot" {
		return npmInstalled("copilot")
	}
	if name == "playwright" {
		return npmInstalled("playwright")
	}
	raw := pkg.InstallPath
	if raw == "" {
		raw = defaultInstallPath(pkg)
	}
	if raw == "" {
		return false, ""
	}
	check := expandHome(raw)
	if _, err := osStat(check); err == nil {
		return true, check
	}
	return false, check
}

// verifyArchive validates a downloaded archive against either a pinned
// sha256 or a .minisig signature. Returns true when verified or nothing to
// verify (latter case logs a warning).
func verifyArchive(archive string, pkg *CustomPackage) bool {
	if expected := pkg.resolvedSHA256(); expected != "" {
		actual, err := sha256Of(archive)
		if err != nil {
			errLog(fmt.Sprintf("hash failed for %s: %v", pkg.Name, err))
			return false
		}
		if actual != expected {
			errLog(fmt.Sprintf("SHA256 mismatch for %s: expected %s, got %s", pkg.Name, expected, actual))
			return false
		}
		fmt.Println("  SHA256 OK")
		return true
	}
	if sigURL := pkg.resolveSHA256URL(); sigURL != "" {
		sigPath := filepath.Join(filepath.Dir(archive), filepath.Base(sigURL))
		if !download(sigURL, sigPath) {
			return false
		}
		if !hasCmd("minisign") {
			warn(fmt.Sprintf("minisign not installed — skipping signature verification for %s", pkg.Name))
			return true
		}
		cmd := []string{"minisign", "-Vm", archive, "-x", sigPath}
		if pkg.MinisignKey != "" {
			cmd = append(cmd, "-P", pkg.MinisignKey)
		}
		if !runCmd(cmd, CmdOpts{}).OK() {
			errLog(fmt.Sprintf("minisign verification failed for %s", pkg.Name))
			return false
		}
		fmt.Println("  minisign OK")
	}
	return true
}

func urlArchOK(pkg *CustomPackage) bool {
	url := pkg.resolveURL()
	if url == "" {
		return true
	}
	if archMatches(url, archName) {
		return true
	}
	if hasOtherArchToken(url) {
		warn(fmt.Sprintf("%s: URL targets %s but host is %s. Update packages.go with a matching URL/SHA256.",
			pkg.Name, otherArch(), archName))
		return false
	}
	return true
}

// ── per-package install handlers ────────────────────────────────────────

func installGo(archive string) {
	goRoot := "/usr/local/go"
	if _, err := osStat(goRoot); err == nil {
		fmt.Printf("  Removing existing Go at %s ...\n", goRoot)
		runCmd([]string{"rm", "-rf", goRoot}, CmdOpts{AsSudo: true})
	}
	runCmd([]string{"tar", "-C", "/usr/local", "-xzf", archive}, CmdOpts{AsSudo: true})
	appendProfileLine("local_go", "export PATH=$PATH:/usr/local/go/bin")
	fmt.Printf("  Go installed to %s\n", goRoot)
}

func installFirecracker(archive, tmp string) {
	if !runCmd([]string{"tar", "-C", tmp, "-xzf", archive}, CmdOpts{}).OK() {
		errLog("firecracker tar extraction failed")
		return
	}
	var binary string
	filepath.Walk(tmp, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".tar.gz") {
			return nil
		}
		if !strings.HasPrefix(name, "firecracker") {
			return nil
		}
		if strings.HasSuffix(name, ".debug") || strings.Contains(name, "debug") {
			return nil
		}
		if binary == "" {
			binary = path
		}
		return nil
	})

	if binary == "" {
		errLog("firecracker binary not found in archive")
		return
	}
	dest := "/usr/local/bin/firecracker"
	runCmd([]string{"cp", binary, dest}, CmdOpts{AsSudo: true})
	runCmd([]string{"chmod", "755", dest}, CmdOpts{AsSudo: true})
	fmt.Printf("  firecracker installed to %s\n", dest)
}

func installZig(pkg *CustomPackage, archive string) {
	parent := "/usr/local"
	zigDir := filepath.Join(parent, "zig-"+pkg.Version)
	if _, err := osStat(zigDir); err == nil {
		runCmd([]string{"rm", "-rf", zigDir}, CmdOpts{AsSudo: true})
	}
	runCmd([]string{"tar", "-C", parent, "-xJf", archive}, CmdOpts{AsSudo: true})

	pattern := filepath.Join(parent, fmt.Sprintf("zig-%s-%s*", archName, osZig[osName]))
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if m != zigDir {
			runCmd([]string{"mv", m, zigDir}, CmdOpts{AsSudo: true})
			break
		}
	}
	symlink := "/usr/local/bin/zig"
	runCmd([]string{"ln", "-sf", filepath.Join(zigDir, "zig"), symlink}, CmdOpts{AsSudo: true})
	fmt.Printf("  Zig installed to %s, symlinked at %s\n", zigDir, symlink)
}

func installNeovim(_ *CustomPackage, tmp string) {
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/neovim/neovim/releases/latest", &rel) {
		return
	}
	archTok := archNvim[archName]
	osTok := osNvim[osName]
	assetName := fmt.Sprintf("nvim-%s-%s.tar.gz", osTok, archTok)

	var asset *ghAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == assetName {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		errLog(fmt.Sprintf("Neovim asset %s not found", assetName))
		return
	}
	if !strings.HasPrefix(asset.Digest, "sha256:") {
		errLog("Neovim asset digest missing or invalid")
		return
	}
	expected := strings.TrimPrefix(asset.Digest, "sha256:")
	dest := filepath.Join(tmp, assetName)
	if !download(asset.BrowserDownloadURL, dest) {
		return
	}
	actual, err := sha256Of(dest)
	if err != nil {
		errLog(fmt.Sprintf("Neovim hash failed: %v", err))
		return
	}
	if actual != expected {
		errLog(fmt.Sprintf("Neovim SHA256 mismatch: expected %s, got %s", expected, actual))
		return
	}
	fmt.Println("  SHA256 OK")

	installDir := fmt.Sprintf("/opt/nvim-%s-%s", osTok, archTok)
	fmt.Println("  Extracting Neovim to /opt ...")
	runCmd([]string{"mkdir", "-p", "/opt"}, CmdOpts{AsSudo: true})
	runCmd([]string{"rm", "-rf", installDir}, CmdOpts{AsSudo: true})
	runCmd([]string{"tar", "-C", "/opt", "-xzf", dest}, CmdOpts{AsSudo: true})

	runCmd([]string{"mkdir", "-p", "/usr/local/bin"}, CmdOpts{AsSudo: true})
	symlink := "/usr/local/bin/nvim"
	runCmd([]string{"ln", "-sf", filepath.Join(installDir, "bin", "nvim"), symlink}, CmdOpts{AsSudo: true})
	fmt.Printf("  Neovim installed to %s, symlinked at %s\n", installDir, symlink)
}

// ── latest-version resolvers ────────────────────────────────────────────

func resolveLatestGo(_ *CustomPackage) (string, string, bool) {
	var raw json.RawMessage
	if !fetchJSON("https://go.dev/dl/?mode=json", &raw) {
		return "", "", false
	}
	// API returns an array; first element is the latest stable release.
	type goFile struct {
		Filename string `json:"filename"`
		Kind     string `json:"kind"`
		SHA256   string `json:"sha256"`
	}
	type goRelease struct {
		Version string   `json:"version"`
		Files   []goFile `json:"files"`
	}
	var releases []goRelease
	if err := json.Unmarshal(raw, &releases); err != nil || len(releases) == 0 {
		var single goRelease
		if err := json.Unmarshal(raw, &single); err != nil {
			return "", "", false
		}
		releases = []goRelease{single}
	}
	latest := releases[0]
	version := strings.TrimPrefix(latest.Version, "go")
	if version == "" {
		return "", "", false
	}
	archiveName := fmt.Sprintf("go%s.%s-%s.tar.gz", version, osGo[osName], archGo[archName])
	for _, f := range latest.Files {
		if f.Filename == archiveName && f.Kind == "archive" && f.SHA256 != "" {
			return version, f.SHA256, true
		}
	}
	return "", "", false
}

func resolveLatestFirecracker(_ *CustomPackage) (string, string, bool) {
	if isMacOS {
		return "", "", false
	}
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/firecracker-microvm/firecracker/releases/latest", &rel) {
		return "", "", false
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	if version == "" {
		return "", "", false
	}
	archiveName := fmt.Sprintf("firecracker-v%s-%s.tgz", version, archName)
	shaAssetName := archiveName + ".sha256.txt"
	for _, a := range rel.Assets {
		if a.Name == shaAssetName {
			sha := fetchText(a.BrowserDownloadURL)
			if sha == "" {
				return "", "", false
			}
			return version, strings.Fields(sha)[0], true
		}
	}
	return "", "", false
}

var zigVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func resolveLatestZig(_ *CustomPackage) (string, string, bool) {
	var data map[string]map[string]any
	if !fetchJSON("https://ziglang.org/download/index.json", &data) {
		return "", "", false
	}
	var stable []string
	for k := range data {
		if k != "master" && zigVersionRe.MatchString(k) {
			stable = append(stable, k)
		}
	}
	if len(stable) == 0 {
		return "", "", false
	}
	sort.Slice(stable, func(i, j int) bool {
		return cmpSemver(stable[i], stable[j]) < 0
	})
	version := stable[len(stable)-1]
	key := archName + "-" + osZig[osName]
	entry, ok := data[version][key].(map[string]any)
	if !ok {
		return "", "", false
	}
	sha, _ := entry["shasum"].(string)
	if sha == "" {
		return "", "", false
	}
	return version, sha, true
}

func cmpSemver(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		ai, _ := strconv.Atoi(pa[i])
		bi, _ := strconv.Atoi(pb[i])
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return len(pa) - len(pb)
}

var latestResolvers = map[string]func(*CustomPackage) (string, string, bool){
	"go":          resolveLatestGo,
	"firecracker": resolveLatestFirecracker,
	"zig":         resolveLatestZig,
}

// resolveLatest best-effort upgrades pkg.Version/SHA256 to the latest release.
// On any failure, warns and leaves the pinned values in place.
func resolveLatest(pkg *CustomPackage) {
	resolver, ok := latestResolvers[pkg.FetchLatest]
	if !ok {
		return
	}
	fmt.Printf("  Checking latest version for %s ...\n", pkg.Name)
	defer func() {
		if r := recover(); r != nil {
			warn(fmt.Sprintf("%s: latest-version lookup panicked %v; falling back to pinned version %s",
				pkg.Name, r, pkg.Version))
		}
	}()
	version, sha, found := resolver(pkg)
	if !found {
		warn(fmt.Sprintf("%s: could not resolve latest version; falling back to pinned version %s",
			pkg.Name, pkg.Version))
		return
	}
	if version == pkg.Version {
		fmt.Printf("  Pinned version %s is already the latest.\n", pkg.Version)
		return
	}
	fmt.Printf("  Latest is %s (pinned was %s); using latest.\n", version, pkg.Version)
	pkg.Version = version
	pkg.SHA256 = strings.ToLower(sha)
	pkg.SHA256URLTemplate = "" // prefer the freshly resolved sha256
}

// ── orchestration ───────────────────────────────────────────────────────

func installCustomPackages(toInstall []*CustomPackage) {
	fmt.Println("\n=== Custom Packages ===")
	ensureNodeLTS()
	for _, pkg := range toInstall {
		name := strings.ToLower(pkg.Name)
		_, checkPath := isCustomPkgInstalled(pkg)
		extra := ""
		if checkPath != "" {
			extra = fmt.Sprintf(" (install path: %s)", checkPath)
		}
		fmt.Printf("\n  Installing %s ...%s\n", pkg.displayName(), extra)
		if checkPath == "" && name != "pip" {
			warn(fmt.Sprintf("%s: no known install path — script will not detect future installs", pkg.Name))
		}

		if name == "firecracker" && isMacOS {
			warn(fmt.Sprintf("%s: Linux-only — skipping on macOS", pkg.Name))
			continue
		}

		switch name {
		case "nvm":
			installNVM()
			ensureNodeLTS()
			continue
		case "pyenv":
			installPyenv()
			continue
		case "pip":
			installPip()
			continue
		case "oh-my-zsh":
			installOhMyZsh()
			continue
		case "neovim":
			tmp, err := os.MkdirTemp("", "bootstrap-nvim-")
			if err != nil {
				errLog(fmt.Sprintf("neovim tmp dir failed: %v", err))
				continue
			}
			installNeovim(pkg, tmp)
			osRemoveAll(tmp)
			continue
		case "agy":
			installAgy()
			continue
		case "claude":
			installNpmPackage("@anthropic-ai/claude-code")
			continue
		case "codex":
			installNpmPackage("@openai/codex")
			continue
		case "copilot":
			installNpmPackage("@github/copilot")
			continue
		case "playwright":
			installPlaywright()
			continue
		case "gh-repo-bootstrap":
			installGHExtension("JMR-dev/gh-repo-bootstrap")
			continue
		}

		resolveLatest(pkg)

		url := pkg.resolveURL()
		if url == "" {
			warn(fmt.Sprintf("No URL or install handler for '%s' — skipping", pkg.Name))
			continue
		}
		if !urlArchOK(pkg) {
			continue
		}

		tmp, err := os.MkdirTemp("", "bootstrap-custom-")
		if err != nil {
			errLog(fmt.Sprintf("tmp dir failed for %s: %v", pkg.Name, err))
			continue
		}
		archive := filepath.Join(tmp, filepath.Base(url))
		if !download(url, archive) {
			osRemoveAll(tmp)
			continue
		}
		if !verifyArchive(archive, pkg) {
			osRemoveAll(tmp)
			continue
		}
		switch name {
		case "go":
			installGo(archive)
		case "firecracker":
			installFirecracker(archive, tmp)
		case "zig":
			installZig(pkg, archive)
		default:
			warn(fmt.Sprintf("No install handler for '%s' — skipping", pkg.Name))
		}
		osRemoveAll(tmp)
	}
}
