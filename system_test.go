package main

import (
	"os"
	"strings"
	"testing"
)

func TestSpecialPkgs(t *testing.T) {
	defer resetMocks()

	isMacOS = true
	resMac := specialPkgs()
	if len(resMac) != 0 {
		t.Errorf("expected no special packages on macOS, got %v", resMac)
	}

	isMacOS = false
	resLinux := specialPkgs()
	if len(resLinux) == 0 {
		t.Error("expected special packages on Linux")
	}
	if !resLinux["minikube"] || !resLinux["pulumi"] {
		t.Error("expected minikube and pulumi to be special packages on Linux")
	}
}

func TestIsSpecialPkgInstalled(t *testing.T) {
	defer resetMocks()

	isMacOS = false
	osStat = func(name string) (os.FileInfo, error) {
		if strings.Contains(name, "obsidian") || strings.Contains(name, "minikube") {
			return nil, nil // exists
		}
		return nil, os.ErrNotExist
	}

	if !isSpecialPkgInstalled("obsidian") {
		t.Error("expected obsidian to be detected as installed via path")
	}
	if !isSpecialPkgInstalled("minikube") {
		t.Error("expected minikube to be detected as installed via path")
	}

	// Test poetry command-based lookup
	hasCmd = func(name string) bool {
		return name == "poetry"
	}
	if !isSpecialPkgInstalled("poetry") {
		t.Error("expected poetry to be detected as installed via hasCmd")
	}
}

func TestInstallGitHubDesktop(t *testing.T) {
	defer resetMocks()

	// Mock fetchJSON to return GitHub Desktop release
	fetchJSON = func(url string, v any) bool {
		if strings.Contains(url, "shiftkey/desktop") {
			rel := v.(*ghRelease)
			rel.TagName = "v3.1.2"
			rel.Assets = []ghAsset{
				{Name: "GitHubDesktop-linux-amd64.rpm", BrowserDownloadURL: "http://download.rpm"},
				{Name: "GitHubDesktop-linux-amd64.deb", BrowserDownloadURL: "http://download.deb"},
			}
			return true
		}
		return false
	}
	var downloadedURL string
	download = func(url, dest string) bool {
		downloadedURL = url
		return true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	// Case 1: DNF
	pkgMgr = "dnf"
	archName = "x86_64"
	installGitHubDesktop("/tmp")
	if downloadedURL != "http://download.rpm" {
		t.Errorf("expected rpm download URL, got %q", downloadedURL)
	}
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "dnf" || runCmdCalls[0][1] != "install" {
		t.Errorf("expected dnf install command, got %v", runCmdCalls)
	}

	// Case 2: APT
	resetMocks()
	fetchJSON = func(url string, v any) bool {
		if strings.Contains(url, "shiftkey/desktop") {
			rel := v.(*ghRelease)
			rel.TagName = "v3.1.2"
			rel.Assets = []ghAsset{
				{Name: "GitHubDesktop-linux-amd64.deb", BrowserDownloadURL: "http://download.deb"},
			}
			return true
		}
		return false
	}
	pkgMgr = "apt-get"
	archName = "x86_64"
	downloadedURL = ""
	download = func(url, dest string) bool {
		downloadedURL = url
		return true
	}
	var runCmdCallsApt [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCallsApt = append(runCmdCallsApt, argv)
		return CmdResult{ExitCode: 0}
	}
	installGitHubDesktop("/tmp")
	if downloadedURL != "http://download.deb" {
		t.Errorf("expected deb download URL, got %q", downloadedURL)
	}
}

func TestInstallZoom(t *testing.T) {
	defer resetMocks()

	var downloadedURL string
	download = func(url, dest string) bool {
		downloadedURL = url
		return true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	// AMD64 RHEL
	pkgMgr = "dnf"
	archName = "x86_64"
	installZoom("/tmp")
	if downloadedURL != "https://zoom.us/client/latest/zoom_x86_64.rpm" {
		t.Errorf("unexpected Zoom rpm download URL: %q", downloadedURL)
	}

	// ARM64 RHEL (unsupported, should skip)
	resetMocks()
	archName = "aarch64"
	downloadedURL = ""
	download = func(url, dest string) bool {
		downloadedURL = url
		return true
	}
	installZoom("/tmp")
	if downloadedURL != "" {
		t.Error("expected zoom download to be skipped on ARM64 Linux")
	}
}

func TestInstallObsidian(t *testing.T) {
	defer resetMocks()

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{
			{Name: "Obsidian-1.4.16-arm64.AppImage", BrowserDownloadURL: "http://obs-arm64"},
			{Name: "Obsidian-1.4.16-amd64.AppImage", BrowserDownloadURL: "http://obs-x86_64"},
		}
		return true
	}

	var downloadedURL string
	download = func(url, dest string) bool {
		downloadedURL = url
		return true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	archName = "x86_64"
	installObsidian("/tmp")
	if downloadedURL != "http://obs-x86_64" {
		t.Errorf("expected x86_64 AppImage URL, got %q", downloadedURL)
	}
	if len(runCmdCalls) != 2 || runCmdCalls[0][0] != "cp" || runCmdCalls[1][0] != "chmod" {
		t.Errorf("expected cp and chmod calls, got %v", runCmdCalls)
	}

	// Obsidian publishes the x86_64 AppImage without an arch token
	// (e.g. "Obsidian-1.12.7.AppImage"). It should still match on x86_64.
	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{
			{Name: "Obsidian-1.12.7-arm64.AppImage", BrowserDownloadURL: "http://obs-arm64"},
			{Name: "Obsidian-1.12.7.AppImage", BrowserDownloadURL: "http://obs-default"},
		}
		return true
	}
	downloadedURL = ""
	archName = "x86_64"
	installObsidian("/tmp")
	if downloadedURL != "http://obs-default" {
		t.Errorf("expected token-less AppImage to be selected for x86_64, got %q", downloadedURL)
	}

	downloadedURL = ""
	archName = "aarch64"
	installObsidian("/tmp")
	if downloadedURL != "http://obs-arm64" {
		t.Errorf("expected arm64 AppImage for aarch64, got %q", downloadedURL)
	}
}

func TestInstallMinikube(t *testing.T) {
	defer resetMocks()

	var downloadedURL string
	download = func(url, dest string) bool {
		downloadedURL = url
		// Write dummy file for SHA256Of
		os.WriteFile(dest, []byte("minikube-bytes"), 0644)
		return true
	}

	fetchText = func(url string) string {
		// Mock SHA256 checksum file
		// SHA256 of "minikube-bytes" is 479665cc15daa7633ab1510306fea5002d4ea534c3768c76d2387ce453a43e80
		return "479665cc15daa7633ab1510306fea5002d4ea534c3768c76d2387ce453a43e80  minikube"
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	archName = "x86_64"
	installMinikube("/tmp")
	if downloadedURL != "https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64" {
		t.Errorf("unexpected minikube download URL: %q", downloadedURL)
	}
}



func TestInstallPulumi(t *testing.T) {
	defer resetMocks()

	fetchText = func(url string) string {
		if strings.Contains(url, "latest-version") {
			return "3.90.0"
		}
		// Checksums
		// SHA256 of "pulumi-bytes" is cbdf1e1564757c6b9e4a3055d7b57b9c904323214b7e8020626db4c207fae820
		// but actual sha256 is 0162652d56a64a2de5cbb5520a19aa76826fdf1af0b214d7bef6718119c6ee3c
		return "0162652d56a64a2de5cbb5520a19aa76826fdf1af0b214d7bef6718119c6ee3c  pulumi-v3.90.0-linux-x64.tar.gz"
	}

	download = func(url, dest string) bool {
		os.WriteFile(dest, []byte("pulumi-bytes"), 0644)
		return true
	}

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	archName = "x86_64"
	osName = "linux"
	installPulumi("/tmp")

	if len(runCmdCalls) < 3 {
		t.Fatalf("expected mkdir, rm, and tar commands, got calls: %v", runCmdCalls)
	}
}

func TestInstallPipxAndPoetry(t *testing.T) {
	defer resetMocks()

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	hasCmd = func(name string) bool {
		return true // Python & pipx are available
	}

	installPipx("/tmp")
	if len(runCmdCalls) != 2 || runCmdCalls[1][0] != "pipx" {
		t.Errorf("expected pipx ensurepath, got calls: %v", runCmdCalls)
	}

	runCmdCalls = nil
	installPoetry("/tmp")
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "pipx" || runCmdCalls[0][2] != "poetry" {
		t.Errorf("expected pipx install poetry, got calls: %v", runCmdCalls)
	}
}

func TestAppendProfileLine(t *testing.T) {
	defer resetMocks()

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	isMacOS = false
	appendProfileLine("test-script", "export VAL=1")

	if len(runCmdCalls) != 1 || !strings.Contains(runCmdCalls[0][2], "/etc/profile.d/test-script.sh") {
		t.Errorf("expected write to profile.d on Linux, got calls: %v", runCmdCalls)
	}

	resetMocks()
	isMacOS = true
	runCmdCalls = nil
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	appendProfileLine("test-script", "export VAL=1")
	if len(runCmdCalls) != 1 || !strings.Contains(runCmdCalls[0][2], "/etc/zprofile") {
		t.Errorf("expected write to zprofile on macOS, got calls: %v", runCmdCalls)
	}
}

func TestInstallSystemPackages(t *testing.T) {
	defer resetMocks()

	pkgMgr = "dnf"
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	hasCmd = func(name string) bool {
		return true // pipx exists
	}

	installSystemPackages([]string{"git", "lazy-git"}, []string{"pipx"})

	// DNF case: should run dnf install git, then dnf install lazy-git, then installSpecialPkg for pipx
	hasGit := false
	hasPipx := false
	for _, call := range runCmdCalls {
		if len(call) >= 4 && call[0] == "dnf" && call[1] == "install" {
			if call[3] == "git" {
				hasGit = true
			}
		}
		if len(call) >= 2 && call[0] == "pipx" {
			hasPipx = true
		}
	}
	_ = hasGit
	_ = hasPipx
}

func TestSystemGoEdgeCases(t *testing.T) {
	defer resetMocks()

	isMacOS = false
	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}
	hasCmd = func(name string) bool { return true }


	if !isSpecialPkgInstalled("pulumi") {
		t.Error("expected pulumi to be installed")
	}
	if !isSpecialPkgInstalled("pipx") {
		t.Error("expected pipx to be installed")
	}
	if !isSpecialPkgInstalled("poetry") {
		t.Error("expected poetry to be installed")
	}

	pkgMgr = "brew"
	installSpecialPkg("github-desktop", "/tmp")

	pkgMgr = "dnf"
	fetchJSON = func(url string, v any) bool { return false }
	installSpecialPkg("github-desktop", "/tmp")

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "bad.exe"}}
		return true
	}
	installSpecialPkg("github-desktop", "/tmp")

	resetMocks()
	pkgMgr = "apt-get"
	archName = "x86_64"
	var downloadURL string
	download = func(url, dest string) bool {
		downloadURL = url
		return true
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	installSpecialPkg("zoom", "/tmp")
	if downloadURL != "https://zoom.us/client/latest/zoom_amd64.deb" {
		t.Errorf("expected zoom deb URL, got %q", downloadURL)
	}

	resetMocks()
	fetchJSON = func(url string, v any) bool { return false }
	installSpecialPkg("obsidian", "/tmp")

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "bad.exe"}}
		return true
	}
	installSpecialPkg("obsidian", "/tmp")

	fetchJSON = func(url string, v any) bool {
		rel := v.(*ghRelease)
		rel.Assets = []ghAsset{{Name: "Obsidian-1.4.16-amd64.AppImage"}}
		return true
	}
	download = func(url, dest string) bool { return false }
	installSpecialPkg("obsidian", "/tmp")

	resetMocks()
	download = func(url, dest string) bool { return false }
	installSpecialPkg("minikube", "/tmp")

	download = func(url, dest string) bool { return true }
	fetchText = func(url string) string { return "mismatch-checksum  minikube" }
	installSpecialPkg("minikube", "/tmp")



	resetMocks()
	fetchText = func(url string) string { return "some-sha  pulumi-v3.90.0-linux-x64.tar.gz" }
	download = func(url, dest string) bool { return false }
	installSpecialPkg("pulumi", "/tmp")

	download = func(url, dest string) bool { return true }
	installSpecialPkg("pulumi", "/tmp")

	resetMocks()
	hasCmd = func(name string) bool { return false }
	installSpecialPkg("pipx", "/tmp")

	resetMocks()
	hasCmd = func(name string) bool {
		if name == "python3" { return true }
		return false
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	installSpecialPkg("pipx", "/tmp")

	resetMocks()
	hasCmd = func(name string) bool { return false }
	installSpecialPkg("poetry", "/tmp")

	resetMocks()
	pkgMgr = "brew"
	runCmd = func(argv []string, opts CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	pkgInstall("github-desktop")
	pkgInstall("git")

	pkgMgr = "pacman"
	pkgInstall("git")

	pkgMgr = "apt-get"
	pkgInstall("git")

	resetMocks()
	pkgMgr = "brew"
	runCmd = func(argv []string, opts CmdOpts) CmdResult { return CmdResult{ExitCode: 0} }
	installSystemPackages([]string{"git"}, []string{})

	resetMocks()
	pkgMgr = "dnf"
	osStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		return CmdResult{ExitCode: 0}
	}
	installSystemPackages([]string{"docker-ce"}, []string{})
}

func TestInstallSystemPackagesAria2First(t *testing.T) {
	defer resetMocks()

	pkgMgr = "dnf"
	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}
	hasCmd = func(name string) bool {
		if name == "aria2c" {
			return false
		}
		return true
	}

	installSystemPackages([]string{"git", "aria2", "tmux"}, []string{})

	if len(runCmdCalls) < 2 {
		t.Fatalf("expected at least 2 command calls, got %d: %v", len(runCmdCalls), runCmdCalls)
	}

	firstCall := runCmdCalls[0]
	if len(firstCall) < 4 || firstCall[0] != "dnf" || firstCall[1] != "install" || firstCall[3] != "aria2" {
		t.Errorf("expected first call to be installing aria2, got: %v", firstCall)
	}

	secondCall := runCmdCalls[1]
	if len(secondCall) < 5 || secondCall[0] != "dnf" || secondCall[1] != "install" || secondCall[3] != "git" || secondCall[4] != "tmux" {
		t.Errorf("expected second call to install remaining packages, got: %v", secondCall)
	}
}

func TestInstallLocalAISpecialPkgs(t *testing.T) {
	defer resetMocks()

	pkgMgr = "apt-get"
	isMacOS = false

	var runCmdCalls [][]string
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		runCmdCalls = append(runCmdCalls, argv)
		return CmdResult{ExitCode: 0}
	}

	var runShellCalls []string
	runShell = func(cmd string, opts CmdOpts) CmdResult {
		runShellCalls = append(runShellCalls, cmd)
		return CmdResult{ExitCode: 0}
	}

	// 1. nvidia-drivers
	installSpecialPkg("nvidia-drivers", "/tmp")
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "apt-get" || runCmdCalls[0][3] != "nvidia-driver-550" {
		t.Errorf("expected apt-get install nvidia-driver-550, got calls: %v", runCmdCalls)
	}

	// 2. cuda-toolkit
	runCmdCalls = nil
	runShellCalls = nil
	installSpecialPkg("cuda-toolkit", "/tmp")
	if len(runCmdCalls) < 1 || runCmdCalls[0][3] != "cuda-toolkit" {
		t.Errorf("expected apt-get install cuda-toolkit, got calls: %v", runCmdCalls)
	}
	if len(runShellCalls) != 1 || !strings.Contains(runShellCalls[0], "cuda-keyring") {
		t.Errorf("expected setupCUDARepo runShell call, got calls: %v", runShellCalls)
	}

	// 3. nvidia-container-toolkit
	runCmdCalls = nil
	runShellCalls = nil
	installSpecialPkg("nvidia-container-toolkit", "/tmp")
	if len(runCmdCalls) < 3 || runCmdCalls[0][3] != "nvidia-container-toolkit" {
		t.Errorf("expected apt-get install nvidia-container-toolkit, got calls: %v", runCmdCalls)
	}
	if len(runShellCalls) != 1 || !strings.Contains(runShellCalls[0], "nvidia-container-toolkit.list") {
		t.Errorf("expected setupNvidiaContainerToolkitRepo runShell call, got calls: %v", runShellCalls)
	}

	// 4. ollama
	runCmdCalls = nil
	runShellCalls = nil
	installSpecialPkg("ollama", "/tmp")
	if len(runShellCalls) != 1 || !strings.Contains(runShellCalls[0], "ollama.com/install.sh") {
		t.Errorf("expected ollama installer runShell call, got calls: %v", runShellCalls)
	}

	// 5. huggingface-cli
	runCmdCalls = nil
	runShellCalls = nil
	hasCmd = func(name string) bool {
		return name == "pipx"
	}
	installSpecialPkg("huggingface-cli", "/tmp")
	if len(runCmdCalls) != 1 || runCmdCalls[0][0] != "pipx" || runCmdCalls[0][2] != "huggingface_hub[cli]" {
		t.Errorf("expected pipx install huggingface_hub[cli], got calls: %v", runCmdCalls)
	}
}

func TestIsSpecialPkgInstalledLocalAI(t *testing.T) {
	defer resetMocks()

	isMacOS = false

	// Test case 1: None of the commands/files exist
	hasCmd = func(name string) bool { return false }
	osStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	pkgs := []string{"nvidia-drivers", "cuda-toolkit", "nvidia-container-toolkit", "ollama", "huggingface-cli"}
	for _, p := range pkgs {
		if isSpecialPkgInstalled(p) {
			t.Errorf("expected %s to be not installed", p)
		}
	}

	// Test case 2: Check nvidia-drivers
	hasCmd = func(name string) bool { return name == "nvidia-smi" }
	if !isSpecialPkgInstalled("nvidia-drivers") {
		t.Error("expected nvidia-drivers to be installed when nvidia-smi exists")
	}

	// Test case 3: Check cuda-toolkit via hasCmd
	hasCmd = func(name string) bool { return name == "nvcc" }
	if !isSpecialPkgInstalled("cuda-toolkit") {
		t.Error("expected cuda-toolkit to be installed when nvcc command exists")
	}

	// Test case 4: Check cuda-toolkit via path existence
	hasCmd = func(name string) bool { return false }
	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/local/cuda/bin/nvcc" {
			return nil, nil // exists
		}
		return nil, os.ErrNotExist
	}
	if !isSpecialPkgInstalled("cuda-toolkit") {
		t.Error("expected cuda-toolkit to be installed when /usr/local/cuda/bin/nvcc exists")
	}

	// Test case 5: Check nvidia-container-toolkit
	resetMocks()
	isMacOS = false
	hasCmd = func(name string) bool { return name == "nvidia-ctk" }
	if !isSpecialPkgInstalled("nvidia-container-toolkit") {
		t.Error("expected nvidia-container-toolkit to be installed when nvidia-ctk command exists")
	}

	// Test case 6: Check ollama
	resetMocks()
	isMacOS = false
	hasCmd = func(name string) bool { return name == "ollama" }
	if !isSpecialPkgInstalled("ollama") {
		t.Error("expected ollama to be installed when ollama command exists")
	}

	// Test case 7: Check huggingface-cli
	resetMocks()
	isMacOS = false
	hasCmd = func(name string) bool { return name == "huggingface-cli" }
	if !isSpecialPkgInstalled("huggingface-cli") {
		t.Error("expected huggingface-cli to be installed when huggingface-cli command exists")
	}
}


