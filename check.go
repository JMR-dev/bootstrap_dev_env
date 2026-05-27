package main

import (
	"fmt"
	"strings"
	"sync"
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

// parallelPartition runs check(item) over items concurrently (using the
// configured cpuWorkers pool) and returns the items where check returned
// true first, then those where it returned false — both in input order.
// We preserve input order so the displayed package lists stay stable.
func parallelPartition[T any](items []T, check func(T) bool) (truthy, falsy []T) {
	if len(items) == 0 {
		return nil, nil
	}
	results := make([]bool, len(items))
	parallelDo(items, cpuWorkers(), func(i int, item T) {
		results[i] = check(item)
	})
	for i, item := range items {
		if results[i] {
			truthy = append(truthy, item)
		} else {
			falsy = append(falsy, item)
		}
	}
	return
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

	alreadyR, toR := parallelPartition(regular, isSystemPkgInstalled)
	alreadyS, toS := parallelPartition(special, isSpecialPkgInstalled)
	return systemCheckResult{
		toInstallRegular: toR,
		toInstallSpecial: toS,
		alreadyInstalled: append(alreadyR, alreadyS...),
		skipped:          skipped,
		remapped:         remapped,
	}
}

func checkFlatpakPackages(ids []string) flatpakCheckResult {
	already, to := parallelPartition(ids, isFlatpakInstalled)
	return flatpakCheckResult{toInstall: to, alreadyInstalled: already}
}

func checkCustomPackages(pkgs []*CustomPackage) customCheckResult {
	type result struct {
		installed bool
		path      string
	}
	results := make([]result, len(pkgs))
	parallelDo(pkgs, cpuWorkers(), func(i int, p *CustomPackage) {
		installed, path := isCustomPkgInstalled(p)
		results[i] = result{installed: installed, path: path}
	})
	var to []*CustomPackage
	var already []customStatus
	for i, p := range pkgs {
		if results[i].installed {
			already = append(already, customStatus{pkg: p, path: results[i].path})
		} else {
			to = append(to, p)
		}
	}
	return customCheckResult{toInstall: to, alreadyInstalled: already}
}

// checkAllInParallel runs the three check passes concurrently. The caller
// must still gate which checks to run via *only; we accept already-prepared
// inputs and skip when the corresponding slice/conditional indicates no work.
func checkAllInParallel(
	runSys bool, sysPkgs []string,
	runFlat bool, flatPkgs []string,
	runCust bool, customPkgs []*CustomPackage,
) (systemCheckResult, flatpakCheckResult, customCheckResult) {
	var sys systemCheckResult
	var flat flatpakCheckResult
	var cust customCheckResult
	var wg sync.WaitGroup
	if runSys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sys = checkSystemPackages(sysPkgs)
		}()
	}
	if runFlat {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flat = checkFlatpakPackages(flatPkgs)
		}()
	}
	if runCust {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cust = checkCustomPackages(customPkgs)
		}()
	}
	wg.Wait()
	return sys, flat, cust
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
