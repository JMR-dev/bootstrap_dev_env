package main

import (
	"fmt"
	"os"
	"strings"
)

// repoFileExists returns true if any of the given paths exists.
func repoFileExists(paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func writeDNFRepo(name, displayName, baseurl, gpgkey string) {
	content := fmt.Sprintf(
		"[%s]\nname=%s\nbaseurl=%s\nenabled=1\ngpgcheck=1\ngpgkey=%s\n",
		name, displayName, baseurl, gpgkey,
	)
	path := "/etc/yum.repos.d/" + name + ".repo"
	runCmd([]string{"tee", path}, CmdOpts{AsSudo: true, Input: []byte(content), Capture: true})
}

func setupDockerRepo() {
	switch pkgMgr {
	case "dnf":
		if repoFileExists("/etc/yum.repos.d/docker-ce.repo") {
			return
		}
		runCmd([]string{"dnf", "config-manager", "addrepo", "--from-repofile",
			"https://download.docker.com/linux/fedora/docker-ce.repo"}, CmdOpts{AsSudo: true})
	case "apt-get":
		if repoFileExists("/etc/apt/sources.list.d/docker.list") {
			return
		}
		runCmd([]string{"apt-get", "update"}, CmdOpts{AsSudo: true})
		runCmd([]string{"apt-get", "install", "-y", "ca-certificates", "curl", "gnupg"}, CmdOpts{AsSudo: true})
		distroID := osReleaseField("ID")
		dockerDistro := "ubuntu"
		if distroID == "debian" || distroID == "ubuntu" {
			dockerDistro = distroID
		}
		runShell(
			"install -m 0755 -d /etc/apt/keyrings && "+
				"curl -fsSL https://download.docker.com/linux/"+dockerDistro+"/gpg | "+
				"sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg && "+
				"sudo chmod a+r /etc/apt/keyrings/docker.gpg",
			CmdOpts{},
		)
		codenameRes := runShell(". /etc/os-release && echo $VERSION_CODENAME",
			CmdOpts{Capture: true})
		codename := strings.TrimSpace(string(codenameRes.Stdout))
		debArch := archDeb[archName]
		runCmd(
			[]string{"tee", "/etc/apt/sources.list.d/docker.list"},
			CmdOpts{
				AsSudo:  true,
				Input:   []byte(fmt.Sprintf("deb [arch=%s signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/%s %s stable\n", debArch, dockerDistro, codename)),
				Capture: true,
			},
		)
		runCmd([]string{"apt-get", "update"}, CmdOpts{AsSudo: true})
	}
	// pacman: docker is in official repos — no extra repo needed.
}

func setupGHRepo() {
	switch pkgMgr {
	case "dnf":
		if repoFileExists("/etc/yum.repos.d/gh-cli.repo") {
			return
		}
		runCmd([]string{"dnf", "config-manager", "addrepo", "--from-repofile",
			"https://cli.github.com/packages/rpm/gh-cli.repo"}, CmdOpts{AsSudo: true})
	case "apt-get":
		if repoFileExists("/etc/apt/sources.list.d/github-cli.list") {
			return
		}
		debArch := archDeb[archName]
		runShell(
			"curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | "+
				"sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && "+
				"sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && "+
				fmt.Sprintf("echo 'deb [arch=%s signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main' | ", debArch)+
				"sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null",
			CmdOpts{},
		)
		runCmd([]string{"apt-get", "update"}, CmdOpts{AsSudo: true})
	}
	// pacman: github-cli is in community repo.
}

func setupChromeRepo() {
	if archName != "x86_64" {
		warn("Google Chrome has no Linux build for this arch — skipping repo")
		return
	}
	switch pkgMgr {
	case "dnf":
		if repoFileExists("/etc/yum.repos.d/google-chrome.repo") {
			return
		}
		writeDNFRepo(
			"google-chrome", "Google Chrome",
			"https://dl.google.com/linux/chrome/rpm/stable/x86_64",
			"https://dl.google.com/linux/linux_signing_key.pub",
		)
	case "apt-get":
		if repoFileExists("/etc/apt/sources.list.d/google-chrome.list") {
			return
		}
		runShell(
			"curl -fsSL https://dl.google.com/linux/linux_signing_key.pub | "+
				"sudo gpg --dearmor -o /etc/apt/keyrings/google-chrome.gpg && "+
				"echo 'deb [arch=amd64 signed-by=/etc/apt/keyrings/google-chrome.gpg] "+
				"https://dl.google.com/linux/chrome/deb/ stable main' | "+
				"sudo tee /etc/apt/sources.list.d/google-chrome.list > /dev/null && "+
				"sudo apt-get update",
			CmdOpts{},
		)
	}
}

func setupVivaldiRepo() {
	if archName != "x86_64" {
		warn("Vivaldi repo on this arch is not supported by this script — skipping")
		return
	}
	switch pkgMgr {
	case "dnf":
		if repoFileExists("/etc/yum.repos.d/vivaldi.repo") {
			return
		}
		writeDNFRepo(
			"vivaldi", "Vivaldi",
			"https://repo.vivaldi.com/archive/rpm/x86_64",
			"https://repo.vivaldi.com/archive/linux_signing_key.pub",
		)
	case "apt-get":
		if repoFileExists("/etc/apt/sources.list.d/vivaldi.list") {
			return
		}
		runShell(
			"curl -fsSL https://repo.vivaldi.com/archive/linux_signing_key.pub | "+
				"sudo gpg --dearmor -o /etc/apt/keyrings/vivaldi.gpg && "+
				"echo 'deb [arch=amd64 signed-by=/etc/apt/keyrings/vivaldi.gpg] "+
				"https://repo.vivaldi.com/archive/deb/ stable main' | "+
				"sudo tee /etc/apt/sources.list.d/vivaldi.list > /dev/null && "+
				"sudo apt-get update",
			CmdOpts{},
		)
	}
}

func setupTemurinRepo() {
	switch pkgMgr {
	case "dnf":
		if repoFileExists("/etc/yum.repos.d/adoptium.repo") {
			return
		}
		writeDNFRepo(
			"Adoptium", "Adoptium",
			"https://packages.adoptium.net/artifactory/rpm/fedora/$releasever/$basearch",
			"https://packages.adoptium.net/artifactory/api/gpg/key/public",
		)
	case "apt-get":
		if repoFileExists("/etc/apt/sources.list.d/adoptium.list") {
			return
		}
		runShell(
			"wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | "+
				"sudo gpg --dearmor | sudo tee /etc/apt/keyrings/adoptium.gpg > /dev/null && "+
				`echo "deb [signed-by=/etc/apt/keyrings/adoptium.gpg] `+
				`https://packages.adoptium.net/artifactory/deb/ `+
				`$(awk -F= '/^VERSION_CODENAME/{print$2}' /etc/os-release) main" | `+
				"sudo tee /etc/apt/sources.list.d/adoptium.list > /dev/null && "+
				"sudo apt-get update",
			CmdOpts{},
		)
	}
}

func setupDotnetRepo() {
	// .NET is in Fedora repos directly — no extra repo needed.
	if pkgMgr != "apt-get" {
		return
	}
	if repoFileExists(
		"/etc/apt/sources.list.d/microsoft-prod.list",
		"/etc/apt/sources.list.d/dotnet.list",
	) {
		return
	}
	distroID := strings.Trim(osReleaseField("ID"), `"`)
	versionID := strings.Trim(osReleaseField("VERSION_ID"), `"`)
	debURL := fmt.Sprintf(
		"https://packages.microsoft.com/config/%s/%s/packages-microsoft-prod.deb",
		distroID, versionID,
	)
	runShell(
		fmt.Sprintf("curl -fsSL %s -o /tmp/packages-microsoft-prod.deb && "+
			"sudo dpkg -i /tmp/packages-microsoft-prod.deb && "+
			"sudo apt-get update", debURL),
		CmdOpts{},
	)
}

type repoGroup struct {
	members map[string]bool
	setup   func()
}

func repoGroups() []repoGroup {
	mk := func(names ...string) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	return []repoGroup{
		{mk("containerd.io", "docker-buildx-plugin", "docker-ce-cli",
			"docker-ce-rootless-extras", "docker-ce", "docker-compose-plugin"), setupDockerRepo},
		{mk("gh"), setupGHRepo},
		{mk("google-chrome-stable"), setupChromeRepo},
		{mk("vivaldi-stable"), setupVivaldiRepo},
		{mk("temurin-25-jdk"), setupTemurinRepo},
		{mk("dotnet-sdk-10.0"), setupDotnetRepo},
	}
}
