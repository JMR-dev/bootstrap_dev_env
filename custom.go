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
	"neovim":            "/usr/local/bin/nvim",
	"oh-my-zsh":         "~/.oh-my-zsh",
	"agy":               "~/.local/bin/agy",
	"gh-repo-bootstrap": "~/.local/share/gh/extensions/gh-repo-bootstrap",
	"yq":                "/usr/local/bin/yq",
	"rustup":            "~/.cargo/bin/rustup",
	"dagger":            "/usr/local/bin/dagger",
	"trivy":             "/usr/local/bin/trivy",
	"cosign":            "/usr/local/bin/cosign",
	"gitleaks":          "/usr/local/bin/gitleaks",
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
	if name == "mdts" {
		return npmInstalled("mdts")
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
		taskPrintln("  SHA256 OK")
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
		if !runCmd(cmd, CmdOpts{Out: taskOut()}).OK() {
			errLog(fmt.Sprintf("minisign verification failed for %s", pkg.Name))
			return false
		}
		taskPrintln("  minisign OK")
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
	out := taskOut()
	goRoot := "/usr/local/go"
	if _, err := osStat(goRoot); err == nil {
		taskPrintf("  Removing existing Go at %s ...\n", goRoot)
		runCmd([]string{"rm", "-rf", goRoot}, CmdOpts{AsSudo: true, Out: out})
	}
	runCmd([]string{"tar", "-C", "/usr/local", "-xzf", archive}, CmdOpts{AsSudo: true, Out: out})
	appendProfileLine("local_go", "export PATH=$PATH:/usr/local/go/bin")
	taskPrintf("  Go installed to %s\n", goRoot)
}

func installFirecracker(archive, tmp string) {
	out := taskOut()
	if !runCmd([]string{"tar", "-C", tmp, "-xzf", archive}, CmdOpts{Out: out}).OK() {
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
		if !strings.HasPrefix(name, "firecracker-v") || strings.Contains(name, "debug") {
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
	runCmd([]string{"cp", binary, dest}, CmdOpts{AsSudo: true, Out: out})
	runCmd([]string{"chmod", "755", dest}, CmdOpts{AsSudo: true, Out: out})
	taskPrintf("  firecracker installed to %s\n", dest)
}

func installZig(pkg *CustomPackage, archive string) {
	out := taskOut()
	parent := "/usr/local"
	zigDir := filepath.Join(parent, "zig-"+pkg.Version)
	if _, err := osStat(zigDir); err == nil {
		runCmd([]string{"rm", "-rf", zigDir}, CmdOpts{AsSudo: true, Out: out})
	}
	runCmd([]string{"tar", "-C", parent, "-xJf", archive}, CmdOpts{AsSudo: true, Out: out})

	pattern := filepath.Join(parent, fmt.Sprintf("zig-%s-%s*", archName, osZig[osName]))
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if m != zigDir {
			runCmd([]string{"mv", m, zigDir}, CmdOpts{AsSudo: true, Out: out})
			break
		}
	}
	symlink := "/usr/local/bin/zig"
	runCmd([]string{"ln", "-sf", filepath.Join(zigDir, "zig"), symlink}, CmdOpts{AsSudo: true, Out: out})
	taskPrintf("  Zig installed to %s, symlinked at %s\n", zigDir, symlink)
}

func installNeovim(_ *CustomPackage, tmp string) {
	out := taskOut()
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
	taskPrintln("  SHA256 OK")

	installDir := fmt.Sprintf("/opt/nvim-%s-%s", osTok, archTok)
	taskPrintln("  Extracting Neovim to /opt ...")
	runCmd([]string{"mkdir", "-p", "/opt"}, CmdOpts{AsSudo: true, Out: out})
	runCmd([]string{"rm", "-rf", installDir}, CmdOpts{AsSudo: true, Out: out})
	runCmd([]string{"tar", "-C", "/opt", "-xzf", dest}, CmdOpts{AsSudo: true, Out: out})

	runCmd([]string{"mkdir", "-p", "/usr/local/bin"}, CmdOpts{AsSudo: true, Out: out})
	symlink := "/usr/local/bin/nvim"
	runCmd([]string{"ln", "-sf", filepath.Join(installDir, "bin", "nvim"), symlink}, CmdOpts{AsSudo: true, Out: out})
	taskPrintf("  Neovim installed to %s, symlinked at %s\n", installDir, symlink)
}

func installRustup(archive string) {
	out := taskOut()
	runCmd([]string{"chmod", "+x", archive}, CmdOpts{Out: out})
	runCmd([]string{archive, "-y", "--no-modify-path"}, CmdOpts{Out: out})
	taskPrintln("  rustup installed.")
}

func installYq(archive string) {
	out := taskOut()
	runCmd([]string{"cp", archive, "/usr/local/bin/yq"}, CmdOpts{AsSudo: true, Out: out})
	runCmd([]string{"chmod", "+x", "/usr/local/bin/yq"}, CmdOpts{AsSudo: true, Out: out})
	taskPrintln("  yq installed.")
}

func installDagger(archive, tmp string) {
	out := taskOut()
	runCmd([]string{"tar", "-C", tmp, "-xzf", archive}, CmdOpts{Out: out})
	runCmd([]string{"mv", filepath.Join(tmp, "dagger"), "/usr/local/bin/dagger"}, CmdOpts{AsSudo: true, Out: out})
	taskPrintln("  dagger installed.")
}

func installTrivy(archive, tmp string) {
	out := taskOut()
	runCmd([]string{"tar", "-C", tmp, "-xzf", archive}, CmdOpts{Out: out})
	runCmd([]string{"mv", filepath.Join(tmp, "trivy"), "/usr/local/bin/trivy"}, CmdOpts{AsSudo: true, Out: out})
	taskPrintln("  trivy installed.")
}

func installCosign(archive string) {
	out := taskOut()
	runCmd([]string{"cp", archive, "/usr/local/bin/cosign"}, CmdOpts{AsSudo: true, Out: out})
	runCmd([]string{"chmod", "+x", "/usr/local/bin/cosign"}, CmdOpts{AsSudo: true, Out: out})
	taskPrintln("  cosign installed.")
}

func installGitleaks(archive, tmp string) {
	out := taskOut()
	runCmd([]string{"tar", "-C", tmp, "-xzf", archive}, CmdOpts{Out: out})
	runCmd([]string{"mv", filepath.Join(tmp, "gitleaks"), "/usr/local/bin/gitleaks"}, CmdOpts{AsSudo: true, Out: out})
	taskPrintln("  gitleaks installed.")
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

func resolveLatestYq(_ *CustomPackage) (string, string, bool) {
	var rel ghRelease
	if !fetchJSON("https://api.github.com/repos/mikefarah/yq/releases/latest", &rel) {
		return "", "", false
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	if version == "" {
		return "", "", false
	}
	return version, "", true
}

var latestResolvers = map[string]func(*CustomPackage) (string, string, bool){
	"go":          resolveLatestGo,
	"firecracker": resolveLatestFirecracker,
	"zig":         resolveLatestZig,
	"yq":          resolveLatestYq,
}

// resolveLatest best-effort upgrades pkg.Version/SHA256 to the latest release.
// On any failure, warns and leaves the pinned values in place.
func resolveLatest(pkg *CustomPackage) {
	resolver, ok := latestResolvers[pkg.FetchLatest]
	if !ok {
		return
	}
	taskPrintf("  Checking latest version for %s ...\n", pkg.Name)
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
		taskPrintf("  Pinned version %s is already the latest.\n", pkg.Version)
		return
	}
	taskPrintf("  Latest is %s (pinned was %s); using latest.\n", version, pkg.Version)
	pkg.Version = version
	pkg.SHA256 = strings.ToLower(sha)
	pkg.SHA256URLTemplate = "" // prefer the freshly resolved sha256
}

// resolveLatestAll fetches latest versions for all packages with a
// FetchLatest hint in parallel — three small HTTP calls today, but enough to
// matter on slower connections. Each lookup is independent and idempotent.
func resolveLatestAll(pkgs []*CustomPackage) {
	var withLatest []*CustomPackage
	for _, p := range pkgs {
		if _, ok := latestResolvers[p.FetchLatest]; ok {
			withLatest = append(withLatest, p)
		}
	}
	if len(withLatest) == 0 {
		return
	}
	parallelDo(withLatest, httpWorkers(), func(_ int, p *CustomPackage) {
		resolveLatest(p)
	})
}

// ── orchestration ───────────────────────────────────────────────────────

// Dependency map for custom packages:
//
//	nvm      → claude, codex, copilot, playwright   (need node from nvm)
//	(none)   → go, firecracker, zig, neovim, pyenv, pip, oh-my-zsh, agy,
//	           gh-repo-bootstrap  (independent)
//
// Within "independent", we further split:
//
//	Wave A (parallel, idempotent on disk targets that don't overlap):
//	    go, firecracker, zig, neovim, pyenv, pip, oh-my-zsh, agy, nvm,
//	    gh-repo-bootstrap
//
//	Wave B (after Wave A; needs nvm/node to exist):
//	    claude, codex, copilot, playwright — batched into one pnpm call
//
// We parallelize Wave A up to cpuWorkers(). Each install runs under its own
// taskOutput so output stays grouped per-package. Wave B runs after Wave A
// has produced ~/.nvm; it batches the npm tools into a single `pnpm add -g`
// call (single Node startup, single pnpm dep solve).

func nodeDependentPkgs() map[string]bool {
	return map[string]bool{
		"claude":     true,
		"codex":      true,
		"copilot":    true,
		"playwright": true,
		"mdts":       true,
	}
}

// runOneCustomInstall executes a single custom package's install handler.
// The caller is responsible for setting up the goroutine-local task output
// when running in parallel. extracted from the old switch statement.
func runOneCustomInstall(pkg *CustomPackage) {
	name := strings.ToLower(pkg.Name)
	_, checkPath := isCustomPkgInstalled(pkg)
	extra := ""
	if checkPath != "" {
		extra = fmt.Sprintf(" (install path: %s)", checkPath)
	}
	taskPrintf("\n  Installing %s ...%s\n", pkg.displayName(), extra)
	if checkPath == "" && name != "pip" {
		warn(fmt.Sprintf("%s: no known install path — script will not detect future installs", pkg.Name))
	}

	if name == "firecracker" && isMacOS {
		warn(fmt.Sprintf("%s: Linux-only — skipping on macOS", pkg.Name))
		return
	}

	switch name {
	case "nvm":
		installNVM()
		return
	case "pyenv":
		installPyenv()
		return
	case "pip":
		installPip()
		return
	case "oh-my-zsh":
		installOhMyZsh()
		return
	case "neovim":
		tmp, err := os.MkdirTemp("", "bootstrap-nvim-")
		if err != nil {
			errLog(fmt.Sprintf("neovim tmp dir failed: %v", err))
			return
		}
		installNeovim(pkg, tmp)
		osRemoveAll(tmp)
		return
	case "agy":
		installAgy()
		return
	case "gh-repo-bootstrap":
		installGHExtension("JMR-dev/gh-repo-bootstrap")
		return
	}

	url := pkg.resolveURL()
	if url == "" {
		warn(fmt.Sprintf("No URL or install handler for '%s' — skipping", pkg.Name))
		return
	}
	if !urlArchOK(pkg) {
		return
	}

	tmp, err := os.MkdirTemp("", "bootstrap-custom-")
	if err != nil {
		errLog(fmt.Sprintf("tmp dir failed for %s: %v", pkg.Name, err))
		return
	}
	defer osRemoveAll(tmp)
	archive := filepath.Join(tmp, filepath.Base(url))
	if !download(url, archive) {
		return
	}
	if !verifyArchive(archive, pkg) {
		return
	}
	switch name {
	case "go":
		installGo(archive)
	case "firecracker":
		installFirecracker(archive, tmp)
	case "zig":
		installZig(pkg, archive)
	case "rustup":
		installRustup(archive)
	case "yq":
		installYq(archive)
	case "dagger":
		installDagger(archive, tmp)
	case "trivy":
		installTrivy(archive, tmp)
	case "cosign":
		installCosign(archive)
	case "gitleaks":
		installGitleaks(archive, tmp)
	default:
		warn(fmt.Sprintf("No install handler for '%s' — skipping", pkg.Name))
	}
}

// installNpmToolsBatch installs all npm-based CLI tools (claude, codex,
// copilot, playwright) in a single `pnpm add -g` invocation. This is
// significantly faster than per-tool installs because pnpm only resolves
// the dep graph and starts Node once. On batch failure we fall back to
// per-package installs so we can report exactly which tool broke.
//
// playwright is special: after the npm install we still need to provision
// browsers via `pnpx playwright install`. We do that after the batch.
func installNpmToolsBatch(pkgs []*CustomPackage) {
	if len(pkgs) == 0 {
		return
	}
	home, _ := os.UserHomeDir()
	if _, err := osStat(filepath.Join(home, ".nvm")); err != nil {
		errLog("NVM is not installed — cannot install npm-based tools")
		return
	}
	ensureNodeLTS()

	npmNames := map[string]string{
		"claude":     "@anthropic-ai/claude-code",
		"codex":      "@openai/codex",
		"copilot":    "@github/copilot",
		"playwright": "playwright",
		"mdts":       "mdts",
	}

	var npmPkgs []string
	var hasPlaywright bool
	for _, p := range pkgs {
		n := strings.ToLower(p.Name)
		if pkg, ok := npmNames[n]; ok {
			npmPkgs = append(npmPkgs, pkg)
			if n == "playwright" {
				hasPlaywright = true
			}
		}
	}
	if len(npmPkgs) == 0 {
		return
	}

	fmt.Printf("\n  Installing %d npm tool(s) via pnpm in one batch ...\n", len(npmPkgs))
	addCmd := fmt.Sprintf(`bash -c '%ssource ~/.nvm/nvm.sh && pnpm add -g %s'`,
		pnpmEnvPrefix(), strings.Join(npmPkgs, " "))
	if !runShell(addCmd, CmdOpts{}).OK() {
		warn("Batched pnpm add -g failed; retrying per-package to isolate failures ...")
		for _, p := range pkgs {
			n := strings.ToLower(p.Name)
			if pkg, ok := npmNames[n]; ok {
				installNpmPackage(pkg)
			}
		}
	}

	if hasPlaywright {
		installPlaywrightBrowsers()
	}
}

func installCustomPackages(toInstall []*CustomPackage) {
	fmt.Println("\n=== Custom Packages ===")
	if len(toInstall) == 0 {
		return
	}

	// Fetch latest versions for all to-install packages in parallel up
	// front — small HTTP calls but they add up serially on slow links.
	resolveLatestAll(toInstall)

	// Split into independent (Wave A) vs node-dependent (Wave B).
	nodeDeps := nodeDependentPkgs()
	var waveA, waveB []*CustomPackage
	for _, p := range toInstall {
		if nodeDeps[strings.ToLower(p.Name)] {
			waveB = append(waveB, p)
		} else {
			waveA = append(waveA, p)
		}
	}

	// Wave A: parallel up to cpuWorkers(). Each package's output is buffered
	// to a per-task taskOutput and flushed on completion so that concurrent
	// installs don't interleave on stdout.
	parallelDo(waveA, cpuWorkers(), func(_ int, pkg *CustomPackage) {
		tOut := newCapturedOutput(pkg.Name)
		withTaskOutput(tOut, func() {
			runOneCustomInstall(pkg)
		})
		tOut.Flush(os.Stdout)
	})

	// Wave B (npm tools): batched into a single pnpm call. Requires Wave A
	// to have completed (specifically: nvm install + ensureNodeLTS), so we
	// run it after the parallel block returns.
	installNpmToolsBatch(waveB)
}
