package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomPackageResolvers(t *testing.T) {
	defer resetMocks()

	osName = "linux"
	archName = "x86_64"

	p := &CustomPackage{
		Name:              "my-pkg",
		Version:           "1.2.3",
		URLTemplate:       "http://example.com/download/{version}/{os}/{arch}/my-pkg.tar.gz",
		SHA256URLTemplate: "http://example.com/download/{version}/{os}/{arch}/my-pkg.tar.gz.minisig",
		SHA256Map: map[string]string{
			"linux-x86_64": "aabbcc",
		},
	}

	if p.resolveURL() != "http://example.com/download/1.2.3/linux/x86_64/my-pkg.tar.gz" {
		t.Errorf("unexpected URL: %q", p.resolveURL())
	}
	if p.resolveSHA256URL() != "http://example.com/download/1.2.3/linux/x86_64/my-pkg.tar.gz.minisig" {
		t.Errorf("unexpected SHA256 URL: %q", p.resolveSHA256URL())
	}
	if p.resolvedSHA256() != "aabbcc" {
		t.Errorf("unexpected resolved SHA256: %q", p.resolvedSHA256())
	}
	if p.displayName() != "my-pkg-1.2.3" {
		t.Errorf("unexpected display name: %q", p.displayName())
	}

	pNoVer := &CustomPackage{Name: "simple"}
	if pNoVer.displayName() != "simple" {
		t.Errorf("unexpected display name: %q", pNoVer.displayName())
	}
}

func TestCmpSemver(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "10.0.0", -1},
		{"1.10.2", "1.2.3", 1},
	}

	for _, tt := range tests {
		res := cmpSemver(tt.a, tt.b)
		// Normalize to -1, 0, 1
		actual := 0
		if res < 0 {
			actual = -1
		} else if res > 0 {
			actual = 1
		}
		if actual != tt.expected {
			t.Errorf("cmpSemver(%q, %q) expected %d, got %d (raw %d)", tt.a, tt.b, tt.expected, actual, res)
		}
	}
}

func TestVerifyArchive(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "archive.tar.gz")
	os.WriteFile(archive, []byte("archive-bytes"), 0644)
	// Hash of "archive-bytes" is 0c982986710a026635603031674053ca851fc0e3ea760094a34f59b84f7f6da6
	p := &CustomPackage{
		Name:   "test",
		SHA256: "0c982986710a026635603031674053ca851fc0e3ea760094a34f59b84f7f6da6",
	}

	if !verifyArchive(archive, p) {
		t.Error("expected verification to pass")
	}

	p.SHA256 = "incorrect-hash"
	if verifyArchive(archive, p) {
		t.Error("expected verification to fail")
	}
}

func TestResolveLatestGo(t *testing.T) {
	defer resetMocks()

	osName = "linux"
	archName = "x86_64"

	fetchJSON = func(url string, v any) bool {
		// Mock Go releases API response
		// This encodes mock data into v (raw JSON Message decoding)
		data := `[
			{
				"version": "go1.21.3",
				"files": [
					{
						"filename": "go1.21.3.linux-amd64.tar.gz",
						"kind": "archive",
						"sha256": "go-sha-value"
					}
				]
			}
		]`
		json.Unmarshal([]byte(data), v)
		return true
	}

	version, sha, ok := resolveLatestGo(nil)
	if !ok || version != "1.21.3" || sha != "go-sha-value" {
		t.Errorf("unexpected resolve latest Go result: version=%q, sha=%q, ok=%v", version, sha, ok)
	}
}

func TestResolveLatestFirecracker(t *testing.T) {
	defer resetMocks()

	isMacOS = false
	archName = "x86_64"

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.TagName = "v1.5.0"
		rel.Assets = []ghAsset{
			{Name: "firecracker-v1.5.0-x86_64.tgz.sha256.txt", BrowserDownloadURL: "http://sha-url"},
		}
		return true
	}

	fetchText = func(url string) string {
		return "firecracker-sha-value  firecracker-v1.5.0-x86_64.tgz"
	}

	version, sha, ok := resolveLatestFirecracker(nil)
	if !ok || version != "1.5.0" || sha != "firecracker-sha-value" {
		t.Errorf("unexpected resolve latest firecracker result: version=%q, sha=%q, ok=%v", version, sha, ok)
	}
}

func TestResolveLatestZig(t *testing.T) {
	defer resetMocks()

	osName = "linux"
	archName = "x86_64"

	fetchJSON = func(url string, v any) bool {
		data := `{
			"0.11.0": {
				"x86_64-linux": {
					"shasum": "zig-sha-value"
				}
			}
		}`
		json.Unmarshal([]byte(data), v)
		return true
	}

	version, sha, ok := resolveLatestZig(nil)
	if !ok || version != "0.11.0" || sha != "zig-sha-value" {
		t.Errorf("unexpected resolve latest zig result: version=%q, sha=%q, ok=%v", version, sha, ok)
	}
}

func TestIsCustomPkgInstalled(t *testing.T) {
	defer resetMocks()

	// Pip installed check
	hasCmd = func(name string) bool {
		return name == "python3"
	}
	probe = func(argv []string, timeout time.Duration) (CmdResult, bool) {
		return CmdResult{ExitCode: 0}, true
	}
	pPip := &CustomPackage{Name: "pip"}
	installed, _ := isCustomPkgInstalled(pPip)
	if !installed {
		t.Error("expected pip to be installed")
	}

	// Go check (installed check via default install path)
	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/local/go" {
			return nil, nil // exists
		}
		return nil, os.ErrNotExist
	}
	pGo := &CustomPackage{Name: "go"}
	installedGo, _ := isCustomPkgInstalled(pGo)
	if !installedGo {
		t.Error("expected go to be installed")
	}
}

func TestExpandHomeAndDefaultInstallPath(t *testing.T) {
	defer resetMocks()

	// Test expandHome
	t.Setenv("HOME", "/my/home")
	expanded := expandHome("~/test")
	if expanded != "/my/home/test" {
		t.Errorf("expected /my/home/test, got %q", expanded)
	}
	notExpanded := expandHome("/other/path")
	if notExpanded != "/other/path" {
		t.Errorf("expected /other/path, got %q", notExpanded)
	}

	// Test defaultInstallPath
	p := &CustomPackage{Name: "go"}
	if defaultInstallPath(p) != "/usr/local/go" {
		t.Errorf("expected /usr/local/go, got %q", defaultInstallPath(p))
	}
	pUnknown := &CustomPackage{Name: "unknown"}
	if defaultInstallPath(pUnknown) != "" {
		t.Errorf("expected empty path, got %q", defaultInstallPath(pUnknown))
	}
}

func TestUrlArchOK(t *testing.T) {
	defer resetMocks()

	archName = "x86_64"
	p := &CustomPackage{Name: "test", URLTemplate: "http://example.com/test-x86_64.tar.gz"}
	if !urlArchOK(p) {
		t.Error("expected urlArchOK to return true for matching arch")
	}

	pBad := &CustomPackage{Name: "test", URLTemplate: "http://example.com/test-aarch64.tar.gz"}
	if urlArchOK(pBad) {
		t.Error("expected urlArchOK to return false for mismatching arch")
	}
}

func TestInstallCustomPackages(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return true }
	download = func(url, dest string) bool {
		// Write valid checksum file so it passes verification
		// SHA256 of "content" is 751a073f248535132b178652553f1f317b3f1f90be68c078021481e33d443224
		os.WriteFile(dest, []byte("content"), 0644)
		return true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	pkgs := []*CustomPackage{
		{
			Name:        "go",
			Version:     "1.21.0",
			URLTemplate: "http://example.com/go.tar.gz",
			SHA256:      "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
		},
		{
			Name:        "zig",
			Version:     "0.11.0",
			URLTemplate: "http://example.com/zig.tar.gz",
			SHA256:      "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
		},
	}

	installCustomPackages(pkgs)

	// Verify that we executed tar/mv/ln etc commands via runCmd
	hasTar := false
	for _, call := range runCmdCalls {
		if call[0] == "tar" {
			hasTar = true
		}
	}
	if !hasTar {
		t.Errorf("expected tar command to be executed, got: %v", runCmdCalls)
	}
}

func TestInstallFirecracker(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "firecracker.tgz")
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "tar" {
			dummyBin := filepath.Join(tmp, "firecracker-v1.5.0")
			os.WriteFile(dummyBin, []byte("binary-content"), 0755)
		}
		return CmdResult{ExitCode: 0}
	}
	installFirecracker(archive, tmp)

	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 1}
	}
	installFirecracker(archive, tmp)

	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	installFirecracker(archive, tmp)
}

func TestInstallNeovim(t *testing.T) {
	defer resetMocks()
	tmp := t.TempDir()
	osName = "linux"
	archName = "x86_64"

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{
			{
				Name: "nvim-linux-x86_64.tar.gz",
				BrowserDownloadURL: "http://example.com/nvim.tar.gz",
				Digest: "sha256:0c982986710a026635603031674053ca851fc0e3ea760094a34f59b84f7f6da6",
			},
		}
		return true
	}

	download = func(url, dest string) bool {
		os.WriteFile(dest, []byte("archive-bytes"), 0644)
		return true
	}

	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}

	installNeovim(nil, tmp)

	fetchJSON = func(url string, v any) bool { return false }
	installNeovim(nil, tmp)

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "other-name"}}
		return true
	}
	installNeovim(nil, tmp)

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "nvim-linux-x86_64.tar.gz", Digest: "bad-digest"}}
		return true
	}
	installNeovim(nil, tmp)

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "nvim-linux-x86_64.tar.gz", Digest: "sha256:0c982986710a026635603031674053ca851fc0e3ea760094a34f59b84f7f6da6"}}
		return true
	}
	download = func(url, dest string) bool { return false }
	installNeovim(nil, tmp)

	download = func(url, dest string) bool {
		os.WriteFile(dest, []byte("different-bytes"), 0644)
		return true
	}
	installNeovim(nil, tmp)
}

func TestResolveLatestEdgeCases(t *testing.T) {
	defer resetMocks()

	pkg := &CustomPackage{Name: "test", FetchLatest: "nonexistent"}
	resolveLatest(pkg)

	latestResolvers["panic-resolver"] = func(pkg *CustomPackage) (string, string, bool) {
		panic("simulated panic")
	}
	pkgPanic := &CustomPackage{Name: "test", FetchLatest: "panic-resolver", Version: "1.0.0"}
	resolveLatest(pkgPanic)

	latestResolvers["fail-resolver"] = func(pkg *CustomPackage) (string, string, bool) {
		return "", "", false
	}
	pkgFail := &CustomPackage{Name: "test", FetchLatest: "fail-resolver", Version: "1.0.0"}
	resolveLatest(pkgFail)

	latestResolvers["same-resolver"] = func(pkg *CustomPackage) (string, string, bool) {
		return "1.0.0", "hash", true
	}
	pkgSame := &CustomPackage{Name: "test", FetchLatest: "same-resolver", Version: "1.0.0"}
	resolveLatest(pkgSame)
}


