package main

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPkgInstallManyBatchesDnf verifies a single batched dnf call rather
// than one per package.
func TestPkgInstallManyBatchesDnf(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"

	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls = append(calls, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}

	failed := pkgInstallMany([]string{"git", "curl", "vim"})
	if len(failed) != 0 {
		t.Errorf("expected no failures, got %v", failed)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 batched call, got %d: %v", len(calls), calls)
	}
	got := strings.Join(calls[0], " ")
	if !strings.HasPrefix(got, "dnf install -y") {
		t.Errorf("expected 'dnf install -y …' prefix, got: %q", got)
	}
	for _, pkg := range []string{"git", "curl", "vim"} {
		if !strings.Contains(got, pkg) {
			t.Errorf("expected %s in batched call, got: %q", pkg, got)
		}
	}
}

// TestPkgInstallManyBatchesApt verifies the same for apt-get.
func TestPkgInstallManyBatchesApt(t *testing.T) {
	defer resetMocks()
	pkgMgr = "apt-get"

	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls = append(calls, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}

	pkgInstallMany([]string{"a", "b", "c"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 batched call, got %d", len(calls))
	}
	if calls[0][0] != "apt-get" || calls[0][1] != "install" || calls[0][2] != "-y" {
		t.Errorf("expected 'apt-get install -y' prefix, got: %v", calls[0])
	}
}

// TestPkgInstallManyBatchesPacman verifies pacman flags.
func TestPkgInstallManyBatchesPacman(t *testing.T) {
	defer resetMocks()
	pkgMgr = "pacman"

	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls = append(calls, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}

	pkgInstallMany([]string{"a", "b"})
	if len(calls) != 1 || calls[0][0] != "pacman" {
		t.Fatalf("expected single pacman call, got %v", calls)
	}
	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "--noconfirm") || !strings.Contains(joined, "--needed") {
		t.Errorf("expected --noconfirm --needed in pacman call, got: %q", joined)
	}
}

// TestPkgInstallManyFallback verifies that a failed batch retries per-package
// and returns the failures it identifies on the per-package retry.
func TestPkgInstallManyFallback(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"

	calls := 0
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls++
		// Fail the first (batched) call, succeed individual retries except for "bad".
		if calls == 1 {
			return CmdResult{ExitCode: 1}
		}
		for _, a := range argv {
			if a == "bad" {
				return CmdResult{ExitCode: 1}
			}
		}
		return CmdResult{ExitCode: 0}
	}

	failed := pkgInstallMany([]string{"good1", "good2", "bad"})
	if len(failed) != 1 || failed[0] != "bad" {
		t.Errorf("expected only 'bad' to fail, got %v", failed)
	}
	// 1 batch + 3 per-package retries = 4 calls.
	if calls != 4 {
		t.Errorf("expected 4 total calls (1 batch + 3 retries), got %d", calls)
	}
}

// TestPkgInstallManyEmpty: no-op on empty input, no calls.
func TestPkgInstallManyEmpty(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"
	called := false
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		called = true
		return CmdResult{ExitCode: 0}
	}
	failed := pkgInstallMany(nil)
	if len(failed) != 0 {
		t.Errorf("expected no failures, got %v", failed)
	}
	if called {
		t.Error("expected no runCmd call for empty input")
	}
}

// TestPkgInstallManyBrew verifies that brew is batched into a single
// `brew install f1 f2 …` call (formulas only — no casks in this test).
// Parallel brew calls would deadlock on shared transitive-dep locks
// (cmake, ninja, libsodium, …), so we deliberately batch and serialize.
func TestPkgInstallManyBrew(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"

	var mu sync.Mutex
	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		mu.Lock()
		calls = append(calls, append([]string(nil), argv...))
		mu.Unlock()
		return CmdResult{ExitCode: 0}
	}

	if failed := pkgInstallMany([]string{"git", "vim", "curl"}); len(failed) != 0 {
		t.Errorf("expected no failures, got %v", failed)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 batched brew call, got %d: %v", len(calls), calls)
	}
	got := strings.Join(calls[0], " ")
	if got != "brew install git vim curl" {
		t.Errorf("expected 'brew install git vim curl', got %q", got)
	}
}

// TestPkgInstallManyBrewSplitCasks verifies that casks and formulas are
// emitted in separate calls (because --cask is mutually exclusive with
// formula installs in one invocation).
func TestPkgInstallManyBrewSplitCasks(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"

	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls = append(calls, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}

	// "docker" is in brewCasks; the rest are formulas.
	pkgInstallMany([]string{"git", "docker", "vim"})

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (1 formula batch + 1 cask batch), got %d: %v", len(calls), calls)
	}
	formula := strings.Join(calls[0], " ")
	cask := strings.Join(calls[1], " ")
	if formula != "brew install git vim" {
		t.Errorf("expected 'brew install git vim', got %q", formula)
	}
	if cask != "brew install --cask docker" {
		t.Errorf("expected 'brew install --cask docker', got %q", cask)
	}
}

// TestPkgInstallManyBrewFallback: batched formula install fails; we retry
// per-package and identify the broken one.
func TestPkgInstallManyBrewFallback(t *testing.T) {
	defer resetMocks()
	pkgMgr = "brew"

	calls := 0
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls++
		// First call is the batch — fail it.
		if calls == 1 {
			return CmdResult{ExitCode: 1}
		}
		// Per-package retries: only "broken" fails.
		if argv[len(argv)-1] == "broken" {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}

	failed := pkgInstallMany([]string{"git", "broken", "curl"})
	if len(failed) != 1 || failed[0] != "broken" {
		t.Errorf("expected only 'broken' to fail, got %v", failed)
	}
	// 1 batch + 3 per-package retries = 4 calls.
	if calls != 4 {
		t.Errorf("expected 4 total calls, got %d", calls)
	}
}

// TestInstallFlatpakBatched: a single batched flatpak install for the
// happy path.
func TestInstallFlatpakBatched(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return name == "flatpak" }
	var calls [][]string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls = append(calls, append([]string(nil), argv...))
		return CmdResult{ExitCode: 0}
	}

	installFlatpakPackages([]string{"a.app", "b.app", "c.app"})

	// Expect: remote-add (1) + single batched install (1) = 2 calls.
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (remote-add + batched install), got %d: %v", len(calls), calls)
	}
	if calls[1][1] != "install" {
		t.Errorf("expected install as second call, got %v", calls[1])
	}
	for _, app := range []string{"a.app", "b.app", "c.app"} {
		found := false
		for _, a := range calls[1] {
			if a == app {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in batched call, got %v", app, calls[1])
		}
	}
}

// TestInstallFlatpakBatchFallback: failed batch retries per-package.
func TestInstallFlatpakBatchFallback(t *testing.T) {
	defer resetMocks()

	hasCmd = func(name string) bool { return name == "flatpak" }
	calls := 0
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		calls++
		// First call: remote-add (always OK)
		// Second call: batched install (fail)
		// Following calls: per-package retries (OK)
		if calls == 2 {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}

	installFlatpakPackages([]string{"a.app", "b.app"})

	// 1 (remote-add) + 1 (failed batch) + 2 (per-package retries) = 4 calls.
	if calls != 4 {
		t.Errorf("expected 4 calls (remote-add + batch + 2 retries), got %d", calls)
	}
}

// TestCheckSystemPackagesParallelOrdering: ordering preserved despite
// concurrent probes.
func TestCheckSystemPackagesParallelOrdering(t *testing.T) {
	defer resetMocks()
	pkgMgr = "dnf"

	// odd-indexed packages "installed", even-indexed "not installed"
	probe = func(argv []string, _ time.Duration) (CmdResult, bool) {
		pkg := argv[len(argv)-1]
		// pkg-0..pkg-7
		idx := pkg[len(pkg)-1] - '0'
		if idx%2 == 1 {
			return CmdResult{ExitCode: 0}, true // installed
		}
		return CmdResult{ExitCode: 1}, true // not installed
	}

	names := []string{"pkg-0", "pkg-1", "pkg-2", "pkg-3", "pkg-4", "pkg-5", "pkg-6", "pkg-7"}
	res := checkSystemPackages(names)

	wantToInstall := []string{"pkg-0", "pkg-2", "pkg-4", "pkg-6"}
	wantAlready := []string{"pkg-1", "pkg-3", "pkg-5", "pkg-7"}
	if !equalStringSlices(res.toInstallRegular, wantToInstall) {
		t.Errorf("toInstall: want %v, got %v", wantToInstall, res.toInstallRegular)
	}
	if !equalStringSlices(res.alreadyInstalled, wantAlready) {
		t.Errorf("alreadyInstalled: want %v, got %v", wantAlready, res.alreadyInstalled)
	}
}

func TestCheckCustomPackagesParallel(t *testing.T) {
	defer resetMocks()

	// pkg with InstallPath /tmp/foo-N; "installed" iff N is odd.
	osStat = func(name string) (os.FileInfo, error) {
		// Map: name like /tmp/foo-1 → installed; /tmp/foo-0 → not.
		idx := name[len(name)-1] - '0'
		if idx%2 == 1 {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	pkgs := []*CustomPackage{
		{Name: "p0", InstallPath: "/tmp/foo-0"},
		{Name: "p1", InstallPath: "/tmp/foo-1"},
		{Name: "p2", InstallPath: "/tmp/foo-2"},
		{Name: "p3", InstallPath: "/tmp/foo-3"},
	}
	res := checkCustomPackages(pkgs)

	if len(res.toInstall) != 2 || res.toInstall[0].Name != "p0" || res.toInstall[1].Name != "p2" {
		t.Errorf("toInstall: want p0,p2 in order, got %v", names(res.toInstall))
	}
	if len(res.alreadyInstalled) != 2 || res.alreadyInstalled[0].pkg.Name != "p1" || res.alreadyInstalled[1].pkg.Name != "p3" {
		t.Errorf("already: want p1,p3 in order, got %v", customNames(res.alreadyInstalled))
	}
}

func TestInstallNpmToolsBatchSinglePnpmCall(t *testing.T) {
	defer resetMocks()

	// Pretend ~/.nvm exists so NVM check passes.
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".nvm") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	var shellCalls []string
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		shellCalls = append(shellCalls, cmd)
		return CmdResult{ExitCode: 0}
	}

	pkgs := []*CustomPackage{
		{Name: "claude"},
		{Name: "codex"},
		{Name: "copilot"},
	}
	installNpmToolsBatch(pkgs)

	// Expect exactly one pnpm add -g call containing all three packages.
	addCalls := 0
	for _, c := range shellCalls {
		if strings.Contains(c, "pnpm add -g") {
			addCalls++
			if !strings.Contains(c, "@anthropic-ai/claude-code") ||
				!strings.Contains(c, "@openai/codex") ||
				!strings.Contains(c, "@github/copilot") {
				t.Errorf("expected all three npm names in batched call, got: %q", c)
			}
		}
	}
	if addCalls != 1 {
		t.Errorf("expected exactly 1 batched pnpm add call, got %d (all calls: %v)", addCalls, shellCalls)
	}
}

func TestInstallNpmToolsBatchFallback(t *testing.T) {
	defer resetMocks()
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".nvm") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	calls := 0
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		calls++
		// Fail the first (batched) pnpm add call; succeed thereafter.
		if calls == 1 && strings.Contains(cmd, "pnpm add -g") {
			return CmdResult{ExitCode: 1}
		}
		return CmdResult{ExitCode: 0}
	}

	pkgs := []*CustomPackage{
		{Name: "claude"},
		{Name: "codex"},
	}
	installNpmToolsBatch(pkgs)

	// 1 ensureNodeLTS + 1 batch + 2 per-package retries (each may emit
	// 2 shell calls: ensureNodeLTS again + add). Just sanity-check that
	// retries happened.
	if calls < 3 {
		t.Errorf("expected at least 3 shell calls after batch failure, got %d", calls)
	}
}

func TestInstallCustomPackagesWavesIndependentFirst(t *testing.T) {
	defer resetMocks()

	// Make hasCmd / osStat permissive. ~/.nvm must "exist" so the
	// npm-batch path doesn't bail out at its precondition check.
	hasCmd = func(name string) bool { return true }
	osStat = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".nvm") {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	osReadFile = func(name string) ([]byte, error) {
		return []byte{}, os.ErrNotExist
	}

	var orderMu sync.Mutex
	var order []string
	runCmd = func(argv []string, _ CmdOpts) CmdResult {
		orderMu.Lock()
		order = append(order, strings.Join(argv, " "))
		orderMu.Unlock()
		return CmdResult{ExitCode: 0}
	}
	runShell = func(cmd string, _ CmdOpts) CmdResult {
		orderMu.Lock()
		order = append(order, cmd)
		orderMu.Unlock()
		return CmdResult{ExitCode: 0}
	}
	download = func(_, _ string) bool { return true }
	fetchJSON = func(_ string, _ any) bool { return false }
	fetchText = func(_ string) string { return "" }

	// Mix of independent + node-dependent. We just verify dispatch order:
	// the npm-batched call must appear after some Wave A activity.
	pkgs := []*CustomPackage{
		{Name: "agy"},
		{Name: "oh-my-zsh"},
		{Name: "claude"},
		{Name: "codex"},
	}
	installCustomPackages(pkgs)

	// The pnpm add -g call must exist and appear after agy/oh-my-zsh
	// install attempts.
	var firstBatchIdx, firstWaveAIdx int = -1, -1
	for i, c := range order {
		if strings.Contains(c, "pnpm add -g @anthropic-ai/claude-code") {
			firstBatchIdx = i
		}
		if (strings.Contains(c, "antigravity.google") || strings.Contains(c, "ohmyzsh")) && firstWaveAIdx == -1 {
			firstWaveAIdx = i
		}
	}
	if firstWaveAIdx == -1 {
		t.Errorf("expected to see Wave A activity (agy/oh-my-zsh), got order: %v", order)
	}
	if firstBatchIdx == -1 {
		t.Errorf("expected to see batched pnpm add call, got order: %v", order)
	}
	if firstWaveAIdx > firstBatchIdx {
		t.Errorf("expected Wave A activity to begin before Wave B batch, got waveA@%d batch@%d", firstWaveAIdx, firstBatchIdx)
	}
}

func TestResolveLatestAllRunsInParallel(t *testing.T) {
	defer resetMocks()

	// Register a custom resolver that records start order.
	var mu sync.Mutex
	var starts []string

	latestResolvers["test-fast"] = func(p *CustomPackage) (string, string, bool) {
		mu.Lock()
		starts = append(starts, p.Name)
		mu.Unlock()
		return p.Version, p.SHA256, true
	}
	defer delete(latestResolvers, "test-fast")

	pkgs := []*CustomPackage{
		{Name: "x", Version: "1", SHA256: "a", FetchLatest: "test-fast"},
		{Name: "y", Version: "2", SHA256: "b", FetchLatest: "test-fast"},
		{Name: "z", Version: "3", SHA256: "c", FetchLatest: "test-fast"},
	}
	resolveLatestAll(pkgs)

	if len(starts) != 3 {
		t.Errorf("expected all 3 resolvers invoked, got %d: %v", len(starts), starts)
	}
}

// ── small helpers/fakes ────────────────────────────────────────────────

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func names(pkgs []*CustomPackage) []string {
	out := make([]string, len(pkgs))
	for i, p := range pkgs {
		out[i] = p.Name
	}
	return out
}

func customNames(s []customStatus) []string {
	out := make([]string, len(s))
	for i, st := range s {
		out[i] = st.pkg.Name
	}
	return out
}
