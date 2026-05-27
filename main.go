// bootstrap_environment ports the Python bootstrap script to Go.
//
// Sections handled:
//
//	System Packages  — installed via dnf, apt-get, pacman, or brew (macOS)
//	Flatpak Packages — installed via flatpak from Flathub (Linux only;
//	                   skipped by default and skipped entirely on macOS; use --gui)
//	Custom Packages  — downloaded, verified, extracted
//	macOS firecracker VM — provisions a Fedora cloud image under a hypervisor
//	                   that supports nested virtualization. Suppress with --no-vm.
//
// OS detection is automatic. On macOS the first actions are to install the
// Xcode Command Line Tools and Homebrew, which is then used as the system
// package manager.
//
// Usage:
//
//	Linux:  sudo bootstrap_environment [--only system|flatpak|custom] [--gui]
//	macOS:       bootstrap_environment [--only system|custom] [--gui] [--no-vm]
//	             (do NOT use sudo on macOS — Homebrew refuses to run as root)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

func main() {
	runMain(os.Args)
}

func runMain(args []string) {
	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
	only := fs.String("only", "", "Install only the named section (system|flatpak|custom)")
	gui := fs.Bool("gui", false, "Include GUI applications (headed environments).")
	noVM := fs.Bool("no-vm", false, "macOS only: skip provisioning the Fedora-on-QEMU VM that backs the firecracker() zsh wrapper.")
	noAI := fs.Bool("no-ai", false, "Skip installation of LLM/AI CLI tools (agy, claude, codex, copilot).")
	_ = fs.Parse(args[1:])

	switch *only {
	case "", "system", "flatpak", "custom":
	default:
		fmt.Fprintf(os.Stderr, "invalid --only value %q (use system|flatpak|custom)\n", *only)
		osExit(2)
		return
	}

	initPkgMgr()

	systemPkgs := append([]string(nil), SystemPackages...)
	flatpakPkgs := append([]string(nil), FlatpakPackages...)
	custom := customPackages()
	customPtrs := make([]*CustomPackage, 0, len(custom))
	for i := range custom {
		// Drop firecracker on macOS — it's provisioned inside the Fedora VM
		// (see setupFirecrackerVM), not on the host.
		if isMacOS && strings.ToLower(custom[i].Name) == "firecracker" {
			continue
		}
		if *noAI {
			name := strings.ToLower(custom[i].Name)
			if name == "agy" || name == "claude" || name == "codex" || name == "copilot" {
				continue
			}
		}
		customPtrs = append(customPtrs, &custom[i])
	}

	fmt.Printf("OS:              %s\n", osName)
	fmt.Printf("Architecture:    %s\n", archName)
	fmt.Printf("Package manager: %s\n", pkgMgr)
	if !*gui {
		fmt.Println("Mode:            headless (default) — skipping GUI apps and Flatpak")
	}

	if isMacOS {
		// Refuse to run as root before doing anything (brew won't run as root).
		checkSudo()
		ensureXcodeCLT()
		ensureHomebrew()
	}

	fmt.Println("Checking installed packages ...")

	if !*gui {
		var skippedGUI, kept []string
		for _, p := range systemPkgs {
			if guiSystemPkgs[p] {
				skippedGUI = append(skippedGUI, p)
			} else {
				kept = append(kept, p)
			}
		}
		systemPkgs = kept
		if len(skippedGUI) > 0 {
			fmt.Printf("  [HEADLESS] Skipping GUI system packages: %s\n", fmtList(skippedGUI, 6))
		}
		flatpakPkgs = nil
	}

	doFlatpak := (*only == "" || *only == "flatpak") && *gui && !isMacOS

	sysCheck, flatCheck, custCheck := checkAllInParallel(
		*only == "" || *only == "system", systemPkgs,
		doFlatpak, flatpakPkgs,
		*only == "" || *only == "custom", customPtrs,
	)

	total := printCheckSummary(sysCheck, flatCheck, custCheck, *only)

	if total == 0 {
		fmt.Println("\nAll packages already installed.")
		writeRunLog()
		return
	}

	if !askYN(fmt.Sprintf("\n%d item(s) to install. Proceed? [y/N] ", total)) {
		fmt.Fprintln(os.Stderr, "Aborted.")
		osExit(1)
		return
	}

	checkSudo()
	promptGitHubToken()

	if *only == "" || *only == "system" {
		installSystemPackages(sysCheck.toInstallRegular, sysCheck.toInstallSpecial)
		ensureZshDefault()
	}

	if doFlatpak {
		installFlatpakPackages(flatCheck.toInstall)
	}

	var pyenvWG interface{ Wait() }
	if *only == "" || *only == "custom" {
		installCustomPackages(custCheck.toInstall)
		ensureNodeLTS()
		if wg := ensurePythonLatest(); wg != nil {
			pyenvWG = wg
		}
	}

	if *only == "" {
		checkAndSetupSSH()
		cloneNvimConfig()
		if isMacOS && !*noVM {
			setupFirecrackerVM()
		}
	}

	if pyenvWG != nil {
		fmt.Println("\n[pyenv] Waiting for background Python install to finish ...")
		pyenvWG.Wait()
	}

	writeRunLog()
	printNotices()
	fmt.Println("\nDone.")

	if hasErrors() {
		osExit(1)
		return
	}

	home, _ := os.UserHomeDir()
	zshrc := filepath.Join(home, ".zshrc")
	if hasCmd("zsh") {
		if _, err := osStat(zshrc); err == nil {
			fmt.Println("\nSourcing ~/.zshrc ...")
			runShell(fmt.Sprintf("zsh -c 'source %s'", zshrc), CmdOpts{})
		}
	}
}

// promptGitHubToken asks the user if they want to supply a GitHub token
// after they've authenticated sudo. With a token, our HTTP-bound worker
// pool uncaps from the conservative 8-worker default up to runtime.NumCPU(),
// because authenticated GitHub requests get 5000/hour instead of the
// unauthenticated 60/hour. A token in the environment is honored without
// prompting. Token input is read with echo off via golang.org/x/term so it
// doesn't leak into terminal scrollback or recorded sessions.
func promptGitHubToken() {
	if existing := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); existing != "" {
		githubTokenSet = true
		fmt.Printf("[GitHub] GITHUB_TOKEN found in environment — HTTP workers uncapped to %d.\n", cpuWorkers())
		return
	}
	if !askYN("\n[GitHub] Provide a GitHub token to uncap HTTP workers from 8 to your CPU count? [y/N] ") {
		return
	}
	fmt.Print("  Paste token (input hidden): ")
	tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		warn(fmt.Sprintf("could not read token: %v — continuing without uncap", err))
		return
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		fmt.Println("  No token provided — keeping the conservative HTTP worker cap.")
		return
	}
	os.Setenv("GITHUB_TOKEN", token)
	githubTokenSet = true
	fmt.Printf("  Token accepted — HTTP workers uncapped to %d.\n", cpuWorkers())
}

func checkSudo() {
	if os.Geteuid() == 0 {
		if isMacOS {
			fmt.Fprintln(os.Stderr, "Do not run this with sudo on macOS — Homebrew refuses to run as root. "+
				"Re-run as your regular user; the tool will request sudo for the operations that need it.")
			osExit(1)
			return
		}
		return
	}
	if !hasCmd("sudo") {
		fmt.Fprintln(os.Stderr, "sudo is required but not installed.")
		osExit(1)
		return
	}
	fmt.Println("Validating sudo access ...")
	r := runCmd([]string{"sudo", "-v"}, CmdOpts{Timeout: 2 * time.Minute})
	if r.ExitCode != 0 {
		fmt.Fprintln(os.Stderr, "sudo authentication failed.")
		osExit(1)
		return
	}
}
