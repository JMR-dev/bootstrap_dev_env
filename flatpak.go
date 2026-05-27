package main

import "fmt"

func installFlatpakPackages(toInstall []string) {
	fmt.Println("\n=== Flatpak Packages ===")

	if !hasCmd("flatpak") {
		fmt.Println("  flatpak is not installed.")
		if !askYN("  Install flatpak now? [y/N] ") {
			warn("flatpak not installed — skipping Flatpak section")
			return
		}
		res := pkgInstall("flatpak")
		if !res.OK() || !hasCmd("flatpak") {
			errLog("flatpak installation failed — skipping Flatpak section")
			return
		}
	}

	runCmd([]string{
		"flatpak", "remote-add", "--if-not-exists", "flathub",
		"https://dl.flathub.org/repo/flathub.flatpakrepo",
	}, CmdOpts{AsSudo: true})

	if len(toInstall) == 0 {
		return
	}

	// Single batched install — flatpak supports multiple refs per invocation
	// and resolves them concurrently internally. Fall back to per-package
	// installs on failure so callers see exactly which IDs broke.
	argv := append([]string{"flatpak", "install", "--noninteractive", "flathub"}, toInstall...)
	fmt.Printf("\n  Installing %d Flatpak(s) in one batch ...\n", len(toInstall))
	if runCmd(argv, CmdOpts{}).OK() {
		return
	}
	warn("Batched flatpak install failed; retrying per-package to isolate failures ...")
	for _, pkgID := range toInstall {
		fmt.Printf("\n  Installing %s ...\n", pkgID)
		res := runCmd([]string{"flatpak", "install", "--noninteractive", "flathub", pkgID}, CmdOpts{})
		if !res.OK() {
			errLog(fmt.Sprintf("Flatpak failed to install: %s", pkgID))
		}
	}
}
