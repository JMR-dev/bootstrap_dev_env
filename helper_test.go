package main

import (
	"os"
)

func resetMocks() {
	runCmd = runCmdReal
	runShell = runShellReal
	hasCmd = hasCmdReal
	probe = probeReal

	download = downloadReal
	fetchJSON = fetchJSONReal
	fetchText = fetchTextReal

	osStat = os.Stat
	osReadFile = os.ReadFile
	osWriteFile = os.WriteFile
	osMkdirAll = os.MkdirAll
	osRemove = os.Remove
	osRemoveAll = os.RemoveAll
	osRename = os.Rename
	osExit = os.Exit
	stdin = os.Stdin

	// Reset global state variables to safe defaults
	isMacOS = false
	pkgMgr = "dnf"
	isRHELFamily = true
	isArchFamily = false
	osName = "linux"
	archName = "x86_64"

	osReleasePath = "/etc/os-release"
	passwdPath = "/etc/passwd"

	issuesMu.Lock()
	issues = nil
	notices = nil
	issuesMu.Unlock()
}
