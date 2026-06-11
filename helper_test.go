package main

import (
	"errors"
	"os"
	"strings"
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
	osExit = os.Exit
	progressConfigPath = progressConfigPathReal
	// Default to an empty reader so un-mocked tests don't hang waiting for user input.
	stdin = strings.NewReader("")
	readPassword = func() ([]byte, error) {
		return nil, errors.New("terminal blocked in test")
	}

	isMacOS = false
	pkgMgr = "dnf"
	isRHELFamily = true
	isArchFamily = false
	osName = "linux"
	archName = "x86_64"

	disableProgressTracking = true

	osReleasePath = "/etc/os-release"
	passwdPath = "/etc/passwd"

	issuesMu.Lock()
	issues = nil
	notices = nil
	errorCount = 0
	issuesMu.Unlock()
}
