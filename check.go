package main

import (
	"fmt"
	"strings"
)

type systemCheckResult struct {
	toInstallRegular []string
	toInstallSpecial []string
	alreadyInstalled []string
	skipped          []string
	remapped         []remap // for display only
}

type remap struct {
	From string
	To   []string
}

type flatpakCheckResult struct {
	toInstall        []string
	alreadyInstalled []string
}

type customCheckResult struct {
	toInstall        []*CustomPackage
	alreadyInstalled []customStatus
}

type customStatus struct {
	pkg  *CustomPackage
	path string
}

func checkSystemPackages(names []string) systemCheckResult {
	overrides := packageOverrides[pkgMgr]
	resolved, skipped := resolveSystemPkgs(names)

	var remapped []remap
	for _, n := range names {
		if ov, ok := overrides[n]; ok && !ov.Skip {
			remapped = append(remapped, remap{From: n, To: ov.Replacement})
		}
	}

	specials := specialPkgs()
	var special, regular []string
	for _, p := range resolved {
		if specials[p] {
			special = append(special, p)
		} else {
			regular = append(regular, p)
		}
	}

	var toR, alreadyR []string
	for _, p := range regular {
		if isSystemPkgInstalled(p) {
			alreadyR = append(alreadyR, p)
		} else {
			toR = append(toR, p)
		}
	}
	var toS, alreadyS []string
	for _, p := range special {
		if isSpecialPkgInstalled(p) {
			alreadyS = append(alreadyS, p)
		} else {
			toS = append(toS, p)
		}
	}
	return systemCheckResult{
		toInstallRegular: toR,
		toInstallSpecial: toS,
		alreadyInstalled: append(alreadyR, alreadyS...),
		skipped:          skipped,
		remapped:         remapped,
	}
}

func checkFlatpakPackages(ids []string) flatpakCheckResult {
	var to, already []string
	for _, p := range ids {
		if isFlatpakInstalled(p) {
			already = append(already, p)
		} else {
			to = append(to, p)
		}
	}
	return flatpakCheckResult{toInstall: to, alreadyInstalled: already}
}

func checkCustomPackages(pkgs []*CustomPackage) customCheckResult {
	var to []*CustomPackage
	var already []customStatus
	for _, p := range pkgs {
		installed, path := isCustomPkgInstalled(p)
		if installed {
			already = append(already, customStatus{pkg: p, path: path})
		} else {
			to = append(to, p)
		}
	}
	return customCheckResult{toInstall: to, alreadyInstalled: already}
}

func fmtList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, " ")
	}
	return strings.Join(items[:limit], " ") + fmt.Sprintf("  … +%d more", len(items)-limit)
}

func printCheckSummary(sys systemCheckResult, flat flatpakCheckResult, cust customCheckResult, only string) int {
	total := 0

	if only == "" || only == "system" {
		toR := sys.toInstallRegular
		toS := sys.toInstallSpecial
		ok := sys.alreadyInstalled
		fmt.Println("\nSystem packages:")
		if len(ok) > 0 {
			fmt.Printf("  [OK]      %3d already installed\n", len(ok))
		}
		n := len(toR) + len(toS)
		if n > 0 {
			combined := append([]string{}, toR...)
			combined = append(combined, toS...)
			fmt.Printf("  [INSTALL] %3d to install:  %s\n", n, fmtList(combined, 6))
		}
		if len(sys.skipped) > 0 {
			fmt.Printf("  [SKIP]    %3d overridden (→ skip): %s\n", len(sys.skipped), fmtList(sys.skipped, 6))
		}
		if len(sys.remapped) > 0 {
			var parts []string
			for _, r := range sys.remapped {
				parts = append(parts, fmt.Sprintf("%s→%s", r.From, strings.Join(r.To, ",")))
			}
			fmt.Printf("  [REMAP]       remapped: %s\n", strings.Join(parts, "  "))
		}
		total += n
	}

	if (only == "" || only == "flatpak") && (len(flat.toInstall) > 0 || len(flat.alreadyInstalled) > 0) {
		fmt.Println("\nFlatpak packages:")
		if len(flat.alreadyInstalled) > 0 {
			fmt.Printf("  [OK]      %3d already installed\n", len(flat.alreadyInstalled))
		}
		if len(flat.toInstall) > 0 {
			fmt.Printf("  [INSTALL] %3d to install:  %s\n", len(flat.toInstall), fmtList(flat.toInstall, 6))
		}
		total += len(flat.toInstall)
	}

	if only == "" || only == "custom" {
		fmt.Println("\nCustom packages:")
		for _, s := range cust.alreadyInstalled {
			suffix := ""
			if s.path != "" {
				suffix = fmt.Sprintf("  (%s)", s.path)
			}
			fmt.Printf("  [OK]      %s%s\n", s.pkg.displayName(), suffix)
		}
		for _, p := range cust.toInstall {
			_, path := isCustomPkgInstalled(p)
			suffix := ""
			if path != "" {
				suffix = fmt.Sprintf("  → %s", path)
			}
			fmt.Printf("  [INSTALL] %s%s\n", p.displayName(), suffix)
		}
		total += len(cust.toInstall)
	}

	return total
}
