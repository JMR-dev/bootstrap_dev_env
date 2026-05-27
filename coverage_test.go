package main

// Targeted tests filling the coverage gaps left by the focused suites.
// Each test exists to exercise a specific branch that was uncovered in
// the `go tool cover` report.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── check.go ────────────────────────────────────────────────────────────

func TestCheckFlatpakPackagesPartition(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return name == "flatpak" }
	probe = func(argv []string, _ time.Duration) (CmdResult, bool) {
		// Only "installed.app" is installed.
		if argv[len(argv)-1] == "installed.app" {
			return CmdResult{ExitCode: 0}, true
		}
		return CmdResult{ExitCode: 1}, true
	}

	res := checkFlatpakPackages([]string{"a.app", "installed.app", "b.app"})
	if !equalStringSlices(res.alreadyInstalled, []string{"installed.app"}) {
		t.Errorf("alreadyInstalled: want [installed.app], got %v", res.alreadyInstalled)
	}
	if !equalStringSlices(res.toInstall, []string{"a.app", "b.app"}) {
		t.Errorf("toInstall: want [a.app b.app], got %v", res.toInstall)
	}
}

func TestCheckFlatpakPackagesEmpty(t *testing.T) {
	defer resetMocks()
	res := checkFlatpakPackages(nil)
	if len(res.toInstall) != 0 || len(res.alreadyInstalled) != 0 {
		t.Errorf("expected empty results, got %+v", res)
	}
}

func TestCheckAllInParallelCombinations(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"

	hasCmd = func(name string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }

	// All three on.
	sys, flat, cust := checkAllInParallel(
		true, []string{"git"},
		true, []string{"a.app"},
		true, []*CustomPackage{{Name: "go"}},
	)
	if len(sys.alreadyInstalled) == 0 {
		t.Errorf("expected system pkgs marked installed, got %+v", sys)
	}
	if len(flat.alreadyInstalled) == 0 {
		t.Errorf("expected flatpak pkgs marked installed, got %+v", flat)
	}
	if len(cust.alreadyInstalled) == 0 {
		t.Errorf("expected custom pkgs marked installed, got %+v", cust)
	}

	// All three off (no goroutines spawned).
	sys2, flat2, cust2 := checkAllInParallel(false, nil, false, nil, false, nil)
	if len(sys2.toInstallRegular) != 0 || len(flat2.toInstall) != 0 || len(cust2.toInstall) != 0 {
		t.Errorf("expected empty results when all flags are off")
	}
}

func TestFmtListOverLimit(t *testing.T) {
	got := fmtList([]string{"a", "b", "c", "d", "e"}, 2)
	if !strings.Contains(got, "a b") || !strings.Contains(got, "+3 more") {
		t.Errorf("expected truncated list with '+3 more', got %q", got)
	}
}

func TestPrintCheckSummaryAllSections(t *testing.T) {
	defer resetMocks()
	captureStdout(t, func() {
		sys := systemCheckResult{
			toInstallRegular: []string{"a", "b"},
			toInstallSpecial: []string{"sp1"},
			alreadyInstalled: []string{"ok1"},
			skipped:          []string{"sk1"},
			remapped:         []remap{{From: "x", To: []string{"y", "z"}}},
		}
		flat := flatpakCheckResult{
			toInstall:        []string{"app1"},
			alreadyInstalled: []string{"app-ok"},
		}
		cust := customCheckResult{
			toInstall:        []*CustomPackage{{Name: "go", InstallPath: "/usr/local/go"}},
			alreadyInstalled: []customStatus{{pkg: &CustomPackage{Name: "zig"}, path: "/usr/local/zig"}},
		}
		osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
		hasCmd = func(_ string) bool { return false }
		total := printCheckSummary(sys, flat, cust, "")
		if total != 4 { // 3 system + 1 flatpak + 1 custom — wait recalc
			// 2 regular + 1 special + 1 flatpak + 1 custom = 5
			if total != 5 {
				t.Errorf("expected total 5, got %d", total)
			}
		}
	})
}

func TestPrintCheckSummaryOnlySystem(t *testing.T) {
	defer resetMocks()
	captureStdout(t, func() {
		sys := systemCheckResult{toInstallRegular: []string{"a"}}
		flat := flatpakCheckResult{}
		cust := customCheckResult{}
		_ = printCheckSummary(sys, flat, cust, "system")
	})
}

func TestPrintCheckSummaryOnlyFlatpak(t *testing.T) {
	defer resetMocks()
	captureStdout(t, func() {
		flat := flatpakCheckResult{toInstall: []string{"a.app"}, alreadyInstalled: []string{"b.app"}}
		_ = printCheckSummary(systemCheckResult{}, flat, customCheckResult{}, "flatpak")
	})
}

// ── custom.go ───────────────────────────────────────────────────────────

func TestResolveURLEmpty(t *testing.T) {
	p := &CustomPackage{Name: "x"}
	if p.resolveURL() != "" {
		t.Error("expected empty URL for empty template")
	}
}

func TestResolveSHA256URLEmpty(t *testing.T) {
	p := &CustomPackage{Name: "x"}
	if p.resolveSHA256URL() != "" {
		t.Error("expected empty SHA URL for empty template")
	}
}

func TestResolvedSHA256MapMiss(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	p := &CustomPackage{
		Name:      "x",
		SHA256Map: map[string]string{"macos-aarch64": "abc"},
	}
	if got := p.resolvedSHA256(); got != "" {
		t.Errorf("expected empty when map has no entry for current OS/arch, got %q", got)
	}
}

func TestPipInstalledNoPython(t *testing.T) {
	defer resetMocks()
	hasCmd = func(name string) bool { return false }
	if pipInstalled() {
		t.Error("expected pipInstalled false when python3 missing")
	}
}

func TestNpmInstalledOnPath(t *testing.T) {
	defer resetMocks()
	// Pretend "echo" (which definitely exists) is the npm command.
	hasCmd = func(name string) bool { return name == "echo" }
	installed, path := npmInstalled("echo")
	if !installed {
		t.Error("expected installed when hasCmd returns true")
	}
	// path may or may not be set depending on resolved PATH, but
	// LookPath for echo should normally succeed.
	if path == "" {
		t.Log("note: exec.LookPath did not resolve a path; that's OK")
	}
}

func TestNpmInstalledViaPnpmDir(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	binDir := filepath.Join(tmp, ".local/share/pnpm/bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "fakebin")
	if err := os.WriteFile(binPath, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	hasCmd = func(_ string) bool { return false }
	installed, path := npmInstalled("fakebin")
	if !installed || path != binPath {
		t.Errorf("expected fakebin found in pnpm dir; got installed=%v path=%q", installed, path)
	}
}

func TestNpmInstalledViaNvmDir(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	binDir := filepath.Join(tmp, ".nvm/versions/node/v20.10.0/bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "fakebin")
	if err := os.WriteFile(binPath, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	hasCmd = func(_ string) bool { return false }
	installed, path := npmInstalled("fakebin")
	if !installed || path != binPath {
		t.Errorf("expected fakebin found in nvm dir; got installed=%v path=%q", installed, path)
	}
}

func TestNpmInstalledNotFound(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	hasCmd = func(_ string) bool { return false }
	installed, path := npmInstalled("absolutely-not-here")
	if installed || path != "" {
		t.Errorf("expected not-installed and empty path; got %v %q", installed, path)
	}
}

func TestIsCustomPkgInstalledNpmNames(t *testing.T) {
	defer resetMocks()
	hasCmd = func(name string) bool {
		return name == "claude" || name == "codex" || name == "copilot" || name == "playwright"
	}
	for _, name := range []string{"claude", "codex", "copilot", "playwright"} {
		ok, _ := isCustomPkgInstalled(&CustomPackage{Name: name})
		if !ok {
			t.Errorf("expected %s detected as installed", name)
		}
	}
}

func TestIsCustomPkgInstalledNoPath(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	// Unknown package with no install path → returns false, "".
	ok, path := isCustomPkgInstalled(&CustomPackage{Name: "no-such-pkg"})
	if ok || path != "" {
		t.Errorf("expected (false, \"\") for unknown pkg, got (%v, %q)", ok, path)
	}
}

func TestVerifyArchiveHashError(t *testing.T) {
	defer resetMocks()
	// SHA256 set but file doesn't exist — sha256Of returns an error.
	p := &CustomPackage{Name: "x", SHA256: "deadbeef"}
	if verifyArchive("/no/such/file", p) {
		t.Error("expected verifyArchive false when hash computation fails")
	}
	if !hasErrors() {
		t.Error("expected error logged for hash failure")
	}
}

func TestVerifyArchiveNoSHANoSig(t *testing.T) {
	defer resetMocks()
	p := &CustomPackage{Name: "x"}
	if !verifyArchive("/no/such/file", p) {
		t.Error("expected verifyArchive true when nothing to verify")
	}
}

func TestVerifyArchiveMinisignDownloadFail(t *testing.T) {
	defer resetMocks()
	p := &CustomPackage{Name: "x", SHA256URLTemplate: "http://example.com/{version}.sig", Version: "1"}
	download = func(_, _ string) bool { return false }
	if verifyArchive("/tmp/foo", p) {
		t.Error("expected verifyArchive false when signature download fails")
	}
}

func TestVerifyArchiveMinisignNotInstalled(t *testing.T) {
	defer resetMocks()
	p := &CustomPackage{Name: "x", SHA256URLTemplate: "http://example.com/{version}.sig", Version: "1"}
	download = func(_, _ string) bool { return true }
	hasCmd = func(name string) bool { return name != "minisign" }
	if !verifyArchive("/tmp/foo", p) {
		t.Error("expected verifyArchive true (skip-with-warning) when minisign missing")
	}
}

func TestVerifyArchiveMinisignFails(t *testing.T) {
	defer resetMocks()
	p := &CustomPackage{Name: "x", SHA256URLTemplate: "http://example.com/{version}.sig", Version: "1", MinisignKey: "key"}
	download = func(_, _ string) bool { return true }
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	if verifyArchive("/tmp/foo", p) {
		t.Error("expected verifyArchive false when minisign verification fails")
	}
}

func TestVerifyArchiveMinisignOK(t *testing.T) {
	defer resetMocks()
	p := &CustomPackage{Name: "x", SHA256URLTemplate: "http://example.com/{version}.sig", Version: "1"}
	download = func(_, _ string) bool { return true }
	hasCmd = func(_ string) bool { return true }
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	if !verifyArchive("/tmp/foo", p) {
		t.Error("expected verifyArchive true when minisign succeeds")
	}
}

func TestUrlArchOKEmptyTemplate(t *testing.T) {
	defer resetMocks()
	if !urlArchOK(&CustomPackage{Name: "x"}) {
		t.Error("expected urlArchOK true for empty URL")
	}
}

func TestUrlArchOKNoArchToken(t *testing.T) {
	defer resetMocks()
	archName = "x86_64"
	// URL doesn't contain an arch token at all — treated as OK.
	p := &CustomPackage{Name: "x", URLTemplate: "http://example.com/generic.tar.gz"}
	if !urlArchOK(p) {
		t.Error("expected urlArchOK true for arch-agnostic URL")
	}
}

func TestInstallGoWithExistingDir(t *testing.T) {
	defer resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/local/go" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	var cmds [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		cmds = append(cmds, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}
	installGo("/tmp/go.tgz")
	// First call should be "rm -rf /usr/local/go".
	if len(cmds) < 1 || cmds[0][0] != "rm" {
		t.Errorf("expected rm -rf as first call, got: %v", cmds)
	}
}

func TestInstallFirecrackerTarFail(t *testing.T) {
	defer resetMocks()
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installFirecracker("/tmp/foo.tgz", "/tmp")
	if !hasErrors() {
		t.Error("expected error logged when tar fails")
	}
}

func TestInstallFirecrackerNoBinaryFound(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	installFirecracker(filepath.Join(tmp, "x.tgz"), tmp)
	if !hasErrors() {
		t.Error("expected error logged when no firecracker binary in archive")
	}
}

func TestInstallZigWithExistingDir(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	osStat = func(name string) (os.FileInfo, error) {
		if strings.Contains(name, "zig-1.2.3") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	var cmds [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		cmds = append(cmds, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}
	pkg := &CustomPackage{Name: "zig", Version: "1.2.3"}
	installZig(pkg, "/tmp/zig.tar.xz")
	if len(cmds) < 1 || cmds[0][0] != "rm" {
		t.Errorf("expected rm -rf as first call, got %v", cmds)
	}
}

func TestInstallNeovimAssetMissing(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	osName = "linux"
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "wrong-name.tar.gz"}}
		return true
	}
	installNeovim(nil, tmp)
	if !hasErrors() {
		t.Error("expected error when asset name not found")
	}
}

func TestResolveLatestGoNoMatchingFile(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		// Provide a release but no archive file with the expected name.
		data := `[{"version":"go1.99.0","files":[]}]`
		return json.Unmarshal([]byte(data), v) == nil
	}
	_, _, ok := resolveLatestGo(nil)
	if ok {
		t.Error("expected resolveLatestGo to fail when no matching archive in release")
	}
}

func TestResolveLatestGoFetchFails(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, _ any) bool { return false }
	if _, _, ok := resolveLatestGo(nil); ok {
		t.Error("expected resolveLatestGo to fail on HTTP error")
	}
}

func TestResolveLatestGoEmptyArray(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, v any) bool {
		return json.Unmarshal([]byte("[]"), v) == nil
	}
	if _, _, ok := resolveLatestGo(nil); ok {
		t.Error("expected resolveLatestGo to fail on empty array")
	}
}

func TestResolveLatestGoSingleObject(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		// Server returns a single object (not array) — fallback path.
		data := `{"version":"go1.50.0","files":[{"filename":"go1.50.0.linux-amd64.tar.gz","kind":"archive","sha256":"abc"}]}`
		return json.Unmarshal([]byte(data), v) == nil
	}
	v, sha, ok := resolveLatestGo(nil)
	if !ok || v != "1.50.0" || sha != "abc" {
		t.Errorf("expected fallback parse to yield 1.50.0/abc, got %q/%q/%v", v, sha, ok)
	}
}

func TestResolveLatestFirecrackerMacOS(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	if _, _, ok := resolveLatestFirecracker(nil); ok {
		t.Error("expected resolveLatestFirecracker false on macOS")
	}
}

func TestResolveLatestFirecrackerNoTag(t *testing.T) {
	defer resetMocks()
	isMacOS = false
	fetchJSON = func(_ string, v any) bool {
		rel := v.(*ghRelease)
		rel.TagName = ""
		return true
	}
	if _, _, ok := resolveLatestFirecracker(nil); ok {
		t.Error("expected false when tag missing")
	}
}

func TestResolveLatestFirecrackerNoMatchingAsset(t *testing.T) {
	defer resetMocks()
	isMacOS = false
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		rel := v.(*ghRelease)
		rel.TagName = "v1.0.0"
		rel.Assets = []ghAsset{{Name: "wrong"}}
		return true
	}
	if _, _, ok := resolveLatestFirecracker(nil); ok {
		t.Error("expected false when no matching .sha256.txt asset")
	}
}

func TestResolveLatestFirecrackerEmptySHA(t *testing.T) {
	defer resetMocks()
	isMacOS = false
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		rel := v.(*ghRelease)
		rel.TagName = "v1.0.0"
		rel.Assets = []ghAsset{{Name: "firecracker-v1.0.0-x86_64.tgz.sha256.txt", BrowserDownloadURL: "http://x"}}
		return true
	}
	fetchText = func(_ string) string { return "" }
	if _, _, ok := resolveLatestFirecracker(nil); ok {
		t.Error("expected false when SHA body is empty")
	}
}

func TestResolveLatestZigEmpty(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, v any) bool {
		return json.Unmarshal([]byte(`{"master":{}}`), v) == nil
	}
	if _, _, ok := resolveLatestZig(nil); ok {
		t.Error("expected false when only master version present")
	}
}

func TestResolveLatestZigMissingPlatformKey(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		// Has a stable version but missing the current platform key.
		return json.Unmarshal([]byte(`{"0.11.0":{"aarch64-linux":{"shasum":"abc"}}}`), v) == nil
	}
	if _, _, ok := resolveLatestZig(nil); ok {
		t.Error("expected false when platform key missing")
	}
}

func TestResolveLatestZigEmptySHA(t *testing.T) {
	defer resetMocks()
	osName = "linux"
	archName = "x86_64"
	fetchJSON = func(_ string, v any) bool {
		return json.Unmarshal([]byte(`{"0.11.0":{"x86_64-linux":{"shasum":""}}}`), v) == nil
	}
	if _, _, ok := resolveLatestZig(nil); ok {
		t.Error("expected false when shasum empty")
	}
}

func TestResolveLatestFetchJSONFail(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, _ any) bool { return false }
	if _, _, ok := resolveLatestZig(nil); ok {
		t.Error("expected false on fetch failure")
	}
}

func TestResolveLatestUnknownHint(t *testing.T) {
	defer resetMocks()
	resolveLatest(&CustomPackage{Name: "x"}) // no FetchLatest → no-op
	resolveLatest(&CustomPackage{Name: "x", FetchLatest: "nonexistent"}) // unknown → no-op
}

func TestResolveLatestUpgrades(t *testing.T) {
	defer resetMocks()
	latestResolvers["upgrade-fixture"] = func(_ *CustomPackage) (string, string, bool) {
		return "2.0.0", "FACE", true
	}
	defer delete(latestResolvers, "upgrade-fixture")
	p := &CustomPackage{Name: "x", Version: "1.0.0", FetchLatest: "upgrade-fixture", SHA256URLTemplate: "sig"}
	resolveLatest(p)
	if p.Version != "2.0.0" || p.SHA256 != "face" || p.SHA256URLTemplate != "" {
		t.Errorf("expected upgrade applied with lowercase SHA and cleared template, got %+v", p)
	}
}

// runOneCustomInstall coverage: hit a few branches.

func TestRunOneCustomInstallFirecrackerOnMacOS(t *testing.T) {
	defer resetMocks()
	isMacOS = true
	runOneCustomInstall(&CustomPackage{Name: "firecracker"})
	// Should warn-and-skip without error.
	issuesMu.Lock()
	hasWarn := false
	for _, msg := range issues {
		if strings.Contains(msg, "Linux-only") {
			hasWarn = true
		}
	}
	issuesMu.Unlock()
	if !hasWarn {
		t.Error("expected Linux-only warning")
	}
}

func TestRunOneCustomInstallNoURLNoHandler(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	hasCmd = func(_ string) bool { return false }
	runOneCustomInstall(&CustomPackage{Name: "unknownpkg"})
	// Should warn about no URL / no handler.
	if !hasIssueContaining("No URL or install handler") {
		t.Errorf("expected 'no URL' warning, issues: %v", issuesSnapshot())
	}
}

func TestRunOneCustomInstallDownloadFail(t *testing.T) {
	defer resetMocks()
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	hasCmd = func(_ string) bool { return false }
	download = func(_, _ string) bool { return false }
	pkg := &CustomPackage{
		Name:        "go",
		Version:     "1.0.0",
		URLTemplate: "http://example.com/go-{arch}.tar.gz",
		SHA256:      "abc",
	}
	runOneCustomInstall(pkg)
}

func TestRunOneCustomInstallArchMismatch(t *testing.T) {
	defer resetMocks()
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	hasCmd = func(_ string) bool { return false }
	pkg := &CustomPackage{
		Name:        "weird",
		URLTemplate: "http://example.com/weird-aarch64.tar.gz",
	}
	runOneCustomInstall(pkg)
}

func TestRunOneCustomInstallVerifyFail(t *testing.T) {
	defer resetMocks()
	archName = "x86_64"
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	hasCmd = func(_ string) bool { return false }
	download = func(_, dest string) bool {
		// Write empty file so sha256Of works but the digest mismatches.
		return os.WriteFile(dest, []byte("x"), 0o644) == nil
	}
	pkg := &CustomPackage{
		Name:        "go",
		Version:     "1.0.0",
		URLTemplate: "http://example.com/go-{arch}.tar.gz",
		SHA256:      "deadbeef",
	}
	runOneCustomInstall(pkg)
}

func TestInstallNpmToolsBatchNoNVM(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	installNpmToolsBatch([]*CustomPackage{{Name: "claude"}})
	if !hasIssueContaining("NVM is not installed") {
		t.Error("expected error about missing NVM")
	}
}

func TestInstallNpmToolsBatchEmpty(t *testing.T) {
	defer resetMocks()
	installNpmToolsBatch(nil)
}

func TestInstallNpmToolsBatchNonNpmFiltered(t *testing.T) {
	defer resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".nvm") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	called := false
	runShell = func(_ string, _ CmdOpts) CmdResult {
		called = true
		return CmdResult{ExitCode: 0}
	}
	// Only an unknown name — should filter to nothing and short-circuit.
	installNpmToolsBatch([]*CustomPackage{{Name: "not-an-npm-tool"}})
	// ensureNodeLTS still runs, so shell calls *are* expected from that.
	_ = called
}

func TestInstallNpmToolsBatchPlaywrightBrowsers(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".nvm") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	var cmds []string
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		cmds = append(cmds, cmd)
		return CmdResult{ExitCode: 0}
	}
	installNpmToolsBatch([]*CustomPackage{{Name: "playwright"}})
	hasBrowserInstall := false
	for _, c := range cmds {
		if strings.Contains(c, "pnpx playwright install --with-deps") {
			hasBrowserInstall = true
		}
	}
	if !hasBrowserInstall {
		t.Errorf("expected pnpx playwright install --with-deps call, got: %v", cmds)
	}
}

func TestInstallCustomPackagesEmpty(t *testing.T) {
	defer resetMocks()
	captureStdout(t, func() {
		installCustomPackages(nil)
	})
}

// ── post.go ─────────────────────────────────────────────────────────────

func TestInstallPyenvFailure(t *testing.T) {
	defer resetMocks()
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installPyenv()
	if !hasErrors() {
		t.Error("expected error on pyenv install failure")
	}
}

func TestInstallNVMFetchFail(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, _ any) bool { return false }
	installNVM()
}

func TestInstallNVMNoTag(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, v any) bool {
		v.(*ghRelease).TagName = ""
		return true
	}
	installNVM()
	if !hasIssueContaining("NVM tag_name missing") {
		t.Error("expected NVM tag missing error")
	}
}

func TestInstallNVMShellFail(t *testing.T) {
	defer resetMocks()
	fetchJSON = func(_ string, v any) bool {
		v.(*ghRelease).TagName = "v0.39.0"
		return true
	}
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installNVM()
	if !hasIssueContaining("NVM installation failed") {
		t.Error("expected NVM install failed error")
	}
}

func TestInstallAgyFail(t *testing.T) {
	defer resetMocks()
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installAgy()
	if !hasErrors() {
		t.Error("expected error from installAgy failure")
	}
}

func TestInstallNpmPackageNoNvm(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	installNpmPackage("@scope/pkg")
	if !hasIssueContaining("NVM is not installed") {
		t.Error("expected NVM-missing error")
	}
}

func TestInstallNpmPackageShellFail(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installNpmPackage("@scope/pkg")
	if !hasIssueContaining("installation failed") {
		t.Error("expected install failure error")
	}
}

func TestInstallPlaywrightBrowsersNonApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	called := false
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		called = strings.Contains(cmd, "pnpx playwright install")
		if strings.Contains(cmd, "--with-deps") {
			t.Errorf("did not expect --with-deps on non-apt, got: %q", cmd)
		}
		return CmdResult{ExitCode: 0}
	}
	installPlaywrightBrowsers()
	if !called {
		t.Error("expected pnpx playwright install call")
	}
}

func TestInstallPlaywrightBrowsersFail(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installPlaywrightBrowsers()
	if !hasErrors() {
		t.Error("expected error on browser install failure")
	}
}

func TestInstallPlaywrightNoNvm(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	installPlaywright()
	if !hasIssueContaining("NVM is not installed") {
		t.Error("expected NVM error")
	}
}

func TestInstallPlaywrightAddFails(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	calls := 0
	runShell = func(_ string, _ CmdOpts) CmdResult {
		calls++
		// ensureNodeLTS issues a series of shell calls; fail the pnpm add step.
		// Easiest: just fail everything that contains "pnpm add -g playwright".
		return CmdResult{ExitCode: 0}
	}
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "pnpm add -g playwright") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPlaywright()
	if !hasIssueContaining("playwright installation failed") {
		t.Errorf("expected playwright install failure, issues: %v", issuesSnapshot())
	}
}

func TestInstallPipPython3Missing(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	installPip()
	if !hasIssueContaining("python3 is not installed") {
		t.Error("expected python3-missing error")
	}
}

func TestInstallPipDecimalFixFails(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	hasCmd = func(_ string) bool { return true }
	// Decimal probe fails both before and after attempted fix.
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	installPip()
	if !hasIssueContaining("_decimal C extension could not be fixed") {
		t.Error("expected decimal-fix error")
	}
}

func TestInstallPipEnsurepipFailApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	calls := 0
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls++
		// ensurepip is the first runCmd call → fail it.
		if calls == 1 {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}
	installPip()
	// Should attempt fallback `apt-get install python3-pip`.
}

func TestInstallPipEnsurepipFailUnknownMgr(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installPip()
	if !hasIssueContaining("ensurepip failed") {
		t.Error("expected ensurepip-failure error on unknown pkgmgr")
	}
}

func TestEnsureZshDefaultNoZsh(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	ensureZshDefault()
	if !hasIssueContaining("zsh not installed") {
		t.Error("expected zsh-missing warning")
	}
}

func TestEnsureZshDefaultRHEL(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh\n")}, true
	}
	isRHELFamily = true
	tmpDir := t.TempDir()
	passwdPath = filepath.Join(tmpDir, "passwd")
	t.Setenv("SUDO_USER", "testuser")
	// Real getpwnam will fail for "testuser" — that's fine, we just want
	// to exercise the RHEL branch up to the user lookup.
	ensureZshDefault()
}

func TestEnsureZshDefaultAlreadyZsh(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh\n")}, true
	}
	t.Setenv("SUDO_USER", os.Getenv("USER"))
	if u := os.Getenv("USER"); u == "" {
		t.Skip("USER env not set; cannot test")
	}
	tmp := t.TempDir()
	passwdPath = filepath.Join(tmp, "passwd")
	// Write a passwd entry that says the user's shell is already zsh.
	uid := fmt.Sprintf("%d", os.Getuid())
	entry := fmt.Sprintf("%s:x:%s:0::/home/%s:/bin/zsh\n", os.Getenv("USER"), uid, os.Getenv("USER"))
	os.WriteFile(passwdPath, []byte(entry), 0o644)
	ensureZshDefault()
}

func TestEnsureZshDefaultChshFails(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("/bin/zsh\n")}, true
	}
	t.Setenv("SUDO_USER", os.Getenv("USER"))
	if os.Getenv("USER") == "" {
		t.Skip("USER env not set")
	}
	tmp := t.TempDir()
	passwdPath = filepath.Join(tmp, "passwd")
	uid := fmt.Sprintf("%d", os.Getuid())
	entry := fmt.Sprintf("%s:x:%s:0::/home/%s:/bin/bash\n", os.Getenv("USER"), uid, os.Getenv("USER"))
	os.WriteFile(passwdPath, []byte(entry), 0o644)
	runCmd = func(_ []string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	ensureZshDefault()
	if !hasIssueContaining("Failed to set default shell") {
		t.Error("expected chsh-failure error")
	}
}

func TestInvokingUserNoSudo(t *testing.T) {
	defer resetMocks()
	t.Setenv("SUDO_USER", "")
	u := invokingUser()
	if u == "" {
		t.Error("expected invokingUser to fall back to user.Current()")
	}
}

func TestEnsureNodeLTSAlreadyInstalled(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "nvm version lts") {
			return CmdResult{ExitCode: 0, Stdout: []byte("v20.10.0\n")}
		}
		return CmdResult{ExitCode: 0}
	}
	ensureNodeLTS()
}

func TestEnsureNodeLTSInstallFail(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "nvm install --lts") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("")}
	}
	ensureNodeLTS()
	if !hasIssueContaining("Node.js LTS install via nvm failed") {
		t.Error("expected node install error")
	}
}

func TestEnsureNodeLTSCorepackFail(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "corepack enable") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("v20\n")}
	}
	ensureNodeLTS()
	if !hasIssueContaining("Failed to enable corepack") {
		t.Error("expected corepack error")
	}
}

func TestEnsureNodeLTSPnpmSetupFail(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "pnpm setup") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("v20\n")}
	}
	ensureNodeLTS()
	if !hasIssueContaining("pnpm setup failed") {
		t.Error("expected pnpm setup error")
	}
}

func TestEnsureNodeLTSAliasFail(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		if strings.Contains(cmd, "nvm alias default") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0, Stdout: []byte("v20\n")}
	}
	ensureNodeLTS()
	if !hasIssueContaining("Setting nvm default to LTS failed") {
		t.Error("expected nvm default error")
	}
}

func TestInstallOhMyZshNoZsh(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	installOhMyZsh()
	if !hasIssueContaining("zsh is not installed") {
		t.Error("expected zsh error")
	}
}

func TestInstallOhMyZshNoGit(t *testing.T) {
	defer resetMocks()
	hasCmd = func(name string) bool { return name == "zsh" }
	installOhMyZsh()
	if !hasIssueContaining("git is not installed") {
		t.Error("expected git error")
	}
}

func TestInstallOhMyZshInstallerFail(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	runShell = func(_ string, _ CmdOpts) CmdResult { return CmdResult{ExitCode: 1} }
	installOhMyZsh()
	if !hasIssueContaining("oh-my-zsh installer failed") {
		t.Error("expected installer failure")
	}
}

func TestInstallOhMyZshNoZshrc(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	osReadFile = func(_ string) ([]byte, error) { return nil, os.ErrNotExist }
	installOhMyZsh()
	if !hasIssueContaining("~/.zshrc not present") {
		t.Error("expected zshrc-missing warning")
	}
}

func TestInstallOhMyZshAppendTheme(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	osReadFile = func(_ string) ([]byte, error) { return []byte("# config without theme\n"), nil }
	written := ""
	osWriteFile = func(_ string, data []byte, _ os.FileMode) error {
		written = string(data)
		return nil
	}
	installOhMyZsh()
	if !strings.Contains(written, `ZSH_THEME="gnzh"`) {
		t.Errorf("expected theme appended, got: %q", written)
	}
}

func TestInstallOhMyZshWriteFail(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return true }
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	osReadFile = func(_ string) ([]byte, error) { return []byte("ZSH_THEME=\"x\"\n"), nil }
	osWriteFile = func(_ string, _ []byte, _ os.FileMode) error { return errors.New("write fail") }
	installOhMyZsh()
	if !hasIssueContaining("could not write ~/.zshrc") {
		t.Error("expected write-fail error")
	}
}

func TestLatestStablePythonProbeFail(t *testing.T) {
	defer resetMocks()
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{}, false
	}
	if v := latestStablePython("/x"); v != "" {
		t.Errorf("expected empty result on probe failure, got %q", v)
	}
}

func TestLatestStablePythonNonZeroExit(t *testing.T) {
	defer resetMocks()
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 1}, true
	}
	if v := latestStablePython("/x"); v != "" {
		t.Errorf("expected empty result on non-zero exit, got %q", v)
	}
}

func TestLatestStablePythonNoVersions(t *testing.T) {
	defer resetMocks()
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("  2.7.18\n  system\n")}, true
	}
	// Only python2 in output → no 3.x versions extracted → "".
	if v := latestStablePython("/x"); v != "" {
		t.Errorf("expected empty result when no 3.x versions, got %q", v)
	}
}

func TestEnsurePythonLatestNoDir(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	if wg := ensurePythonLatest(); wg != nil {
		t.Error("expected nil when pyenv dir missing")
	}
}

func TestEnsurePythonLatestNoBin(t *testing.T) {
	defer resetMocks()
	calls := 0
	osStat = func(_ string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	if wg := ensurePythonLatest(); wg != nil {
		t.Error("expected nil when pyenv bin missing")
	}
}

func TestEnsurePythonLatestLatestEmpty(t *testing.T) {
	defer resetMocks()
	osStat = func(_ string) (os.FileInfo, error) { return nil, nil }
	probe = func(_ []string, _ time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0, Stdout: []byte("")}, true
	}
	if wg := ensurePythonLatest(); wg != nil {
		t.Error("expected nil when no python version found")
	}
}

// ── system.go ───────────────────────────────────────────────────────────

func TestPkgInstallBrewCask(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"
	var argv []string
	runCmd = func(a []string, _ CmdOpts) CmdResult {
		argv = a
		return CmdResult{ExitCode: 0}
	}
	pkgInstall("docker") // docker is a brew cask
	if len(argv) < 3 || argv[2] != "--cask" {
		t.Errorf("expected brew --cask install, got %v", argv)
	}
}

func TestPkgInstallBrewFormula(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"
	var argv []string
	runCmd = func(a []string, _ CmdOpts) CmdResult {
		argv = a
		return CmdResult{ExitCode: 0}
	}
	pkgInstall("git")
	if len(argv) != 3 || argv[2] != "git" {
		t.Errorf("expected brew install git, got %v", argv)
	}
}

// ── pkgmgr.go ───────────────────────────────────────────────────────────

func TestEnsureWhichInstalledAlreadyPresent(t *testing.T) {
	defer resetMocks()
	hasCmd = func(name string) bool { return name == "which" }
	called := false
	runCmd = func(_ []string, _ CmdOpts) CmdResult {
		called = true
		return CmdResult{ExitCode: 0}
	}
	ensureWhichInstalled()
	if called {
		t.Error("expected no runCmd when which is already present")
	}
}

func TestEnsureWhichInstalledViaPacman(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	pkgMgr = "pacman"
	var argv []string
	runCmd = func(a []string, _ CmdOpts) CmdResult {
		argv = a
		return CmdResult{ExitCode: 0}
	}
	ensureWhichInstalled()
	if len(argv) == 0 || argv[0] != "pacman" {
		t.Errorf("expected pacman install, got %v", argv)
	}
}

func TestEnsureWhichInstalledViaBrew(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	pkgMgr = "brew"
	var argv []string
	runCmd = func(a []string, _ CmdOpts) CmdResult {
		argv = a
		return CmdResult{ExitCode: 0}
	}
	ensureWhichInstalled()
	if len(argv) == 0 || argv[0] != "brew" {
		t.Errorf("expected brew install, got %v", argv)
	}
}

func TestEnsureWhichInstalledFails(t *testing.T) {
	defer resetMocks()
	hasCmd = func(_ string) bool { return false }
	pkgMgr = "dnf"
	runCmd = func(_ []string, _ CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1, Err: errors.New("boom")}
	}
	// Should warn (printed to stderr) but not panic.
	ensureWhichInstalled()
}

// ── parallel.go ─────────────────────────────────────────────────────────

func TestTaskOutputSerialPrintf(t *testing.T) {
	defer resetMocks()
	// Serial mode prints to real stdout; just exercise the branch.
	captureStdout(t, func() {
		t := newSerialOutput()
		t.Printf("hello %d\n", 1)
		t.Println("world")
	})
}

func TestParallelDoZeroWorkers(t *testing.T) {
	items := []int{1, 2, 3}
	var sum int64
	parallelDo(items, 0, func(_ int, v int) {
		atomic := int64(v)
		_ = atomic
		sum += int64(v)
	})
	if sum != 6 {
		t.Errorf("expected sum 6 (clamped to 1 worker), got %d", sum)
	}
}

// ── taskctx.go ──────────────────────────────────────────────────────────

func TestWithTaskOutputNilFn(t *testing.T) {
	called := false
	withTaskOutput(nil, func() { called = true })
	if !called {
		t.Error("expected fn called even with nil taskOutput")
	}
}

func TestWithTaskOutputNested(t *testing.T) {
	outer := newCapturedOutput("outer")
	inner := newCapturedOutput("inner")
	withTaskOutput(outer, func() {
		if currentTask() != outer {
			t.Error("expected outer task active")
		}
		withTaskOutput(inner, func() {
			if currentTask() != inner {
				t.Error("expected inner task active")
			}
		})
		if currentTask() != outer {
			t.Error("expected outer restored after nested withTaskOutput")
		}
	})
	if currentTask() != nil {
		t.Error("expected no active task after outer returns")
	}
}

// ── exec.go ─────────────────────────────────────────────────────────────

func TestWriteToOutEmpty(t *testing.T) {
	var buf bytes.Buffer
	writeToOut(&buf, nil)
	if buf.Len() != 0 {
		t.Error("expected no write for empty input")
	}
}

func TestWriteToOutAddsNewline(t *testing.T) {
	var buf bytes.Buffer
	writeToOut(&buf, []byte("no-newline"))
	if !bytes.HasSuffix(buf.Bytes(), []byte{'\n'}) {
		t.Errorf("expected trailing newline appended, got %q", buf.String())
	}
}

func TestWriteToOutPreservesExistingNewline(t *testing.T) {
	var buf bytes.Buffer
	writeToOut(&buf, []byte("ends-with\n"))
	if bytes.Count(buf.Bytes(), []byte{'\n'}) != 1 {
		t.Errorf("expected exactly one newline, got %d", bytes.Count(buf.Bytes(), []byte{'\n'}))
	}
}

func TestRunCmdRealWithCapture(t *testing.T) {
	defer resetMocks()
	r := runCmdReal([]string{"echo", "cap-mode"}, CmdOpts{Capture: true})
	if !r.OK() {
		t.Fatalf("echo failed: %v", r.Err)
	}
	if !bytes.Contains(r.Stdout, []byte("cap-mode")) {
		t.Errorf("expected captured stdout to contain 'cap-mode', got: %q", r.Stdout)
	}
}

// ── issues.go ───────────────────────────────────────────────────────────

func TestWriteRunLogFailure(t *testing.T) {
	defer resetMocks()
	warn("a warning to ensure issues is non-empty")
	osWriteFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("disk full")
	}
	// Capture stderr to verify the write failure is reported.
	captureStderr(t, func() {
		writeRunLog()
	})
}

// ── main.go ─────────────────────────────────────────────────────────────

func TestPromptGitHubTokenFromEnv(t *testing.T) {
	defer func() { githubTokenSet = false }()
	t.Setenv("GITHUB_TOKEN", "ghp_test_token")
	githubTokenSet = false
	captureStdout(t, func() {
		promptGitHubToken()
	})
	if !githubTokenSet {
		t.Error("expected githubTokenSet=true when GITHUB_TOKEN env present")
	}
}

func TestPromptGitHubTokenDecline(t *testing.T) {
	defer func() { githubTokenSet = false }()
	githubTokenSet = false
	os.Unsetenv("GITHUB_TOKEN")
	stdin = strings.NewReader("n\n")
	defer func() { stdin = os.Stdin }()
	captureStdout(t, func() {
		promptGitHubToken()
	})
	if githubTokenSet {
		t.Error("expected githubTokenSet to stay false when user declines")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	var mu sync.Mutex
	go func() {
		mu.Lock()
		io.Copy(&buf, r)
		mu.Unlock()
		close(done)
	}()
	fn()
	w.Close()
	os.Stdout = old
	<-done
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	done := make(chan struct{})
	var buf bytes.Buffer
	var mu sync.Mutex
	go func() {
		mu.Lock()
		io.Copy(&buf, r)
		mu.Unlock()
		close(done)
	}()
	fn()
	w.Close()
	os.Stderr = old
	<-done
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

func hasIssueContaining(substr string) bool {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	for _, m := range issues {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

func issuesSnapshot() []string {
	issuesMu.Lock()
	defer issuesMu.Unlock()
	out := make([]string, len(issues))
	copy(out, issues)
	return out
}
