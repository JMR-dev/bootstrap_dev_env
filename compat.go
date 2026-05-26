package main

import (
	"io"
	"os"
)

var (
	// Exec redirects
	runCmd   = runCmdReal
	runShell = runShellReal
	hasCmd   = hasCmdReal
	probe    = probeReal

	// Net redirects
	download  = downloadReal
	fetchJSON = fetchJSONReal
	fetchText = fetchTextReal

	// OS redirects
	osStat      = os.Stat
	osReadFile  = os.ReadFile
	osWriteFile = os.WriteFile
	osMkdirAll  = os.MkdirAll
	osRemove    = os.Remove
	osRemoveAll = os.RemoveAll
	osRename    = os.Rename
	osExit      = os.Exit
	stdin       io.Reader = os.Stdin

	// Filesystem paths
	osReleasePath = "/etc/os-release"
	passwdPath    = "/etc/passwd"
)
