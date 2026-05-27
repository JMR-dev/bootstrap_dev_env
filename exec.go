package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Default per-call cap for runCmd and runShell. Generous enough for heavy
// installs (apt, brew, large downloads) but bounded so a stuck command can't
// hang the bootstrap forever. Override per-call for genuinely longer
// operations (e.g. pyenv compiles).
const defaultSubprocessTimeout = 30 * time.Minute

// CmdOpts captures the optional knobs on runCmd / runShell.
//
// Out, when non-nil, switches the call into "captured-routed" mode: stdout
// and stderr are buffered, then the "$ cmd" echo, captured stdout, and
// captured stderr are written to Out in order. Capture is forced true.
// This is how parallel workers route output into per-task buffers without
// interleaving on os.Stdout. When Out is nil (default) the call streams to
// os.Stdout exactly as before, preserving the live-tail behavior used by
// the sequential code paths.
type CmdOpts struct {
	AsSudo  bool
	Check   bool // exit on failure (kept for parity but treated as advisory — we return the error instead)
	Input   []byte
	Capture bool
	Cwd     string
	Timeout time.Duration // zero = defaultSubprocessTimeout
	Out     io.Writer     // optional sink for echo + captured streams
}

// CmdResult holds the outcome of a subprocess invocation.
type CmdResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	Err      error
}

func (r CmdResult) OK() bool { return r.Err == nil && r.ExitCode == 0 }

// runCmdReal executes argv with the supplied options.
func runCmdReal(argv []string, opts CmdOpts) CmdResult {
	if opts.Timeout == 0 {
		opts.Timeout = defaultSubprocessTimeout
	}
	if opts.AsSudo && os.Geteuid() != 0 {
		argv = append([]string{"sudo"}, argv...)
	}
	if opts.Out != nil {
		fmt.Fprintf(opts.Out, "$ %s\n", strings.Join(argv, " "))
		opts.Capture = true
	} else {
		fmt.Printf("  $ %s\n", strings.Join(argv, " "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Input != nil {
		cmd.Stdin = bytes.NewReader(opts.Input)
	}

	var stdout, stderr bytes.Buffer
	if opts.Capture {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	err := cmd.Run()
	res := CmdResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if opts.Out != nil {
		writeToOut(opts.Out, res.Stdout)
		writeToOut(opts.Out, res.Stderr)
	}

	if ctx.Err() == context.DeadlineExceeded {
		warn(fmt.Sprintf("%q timed out after %s", argv[0], opts.Timeout))
		res.ExitCode = 124
		res.Err = ctx.Err()
		return res
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			res.Err = err
			return res
		}
		warn(fmt.Sprintf("error launching %q: %v", argv[0], err))
		res.ExitCode = 1
		res.Err = err
	}
	return res
}

// runShellReal executes a single shell string via /bin/sh -c (matching the Python
// version's subprocess.run(..., shell=True)).
func runShellReal(cmd string, opts CmdOpts) CmdResult {
	if opts.Timeout == 0 {
		opts.Timeout = defaultSubprocessTimeout
	}
	if opts.Out != nil {
		fmt.Fprintf(opts.Out, "$ %s\n", cmd)
		opts.Capture = true
	} else {
		fmt.Printf("  $ %s\n", cmd)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	if opts.Cwd != "" {
		c.Dir = opts.Cwd
	}
	if opts.Input != nil {
		c.Stdin = bytes.NewReader(opts.Input)
	}

	var stdout, stderr bytes.Buffer
	if opts.Capture {
		c.Stdout = &stdout
		c.Stderr = &stderr
	} else {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	}

	err := c.Run()
	res := CmdResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if opts.Out != nil {
		writeToOut(opts.Out, res.Stdout)
		writeToOut(opts.Out, res.Stderr)
	}

	if ctx.Err() == context.DeadlineExceeded {
		warn(fmt.Sprintf("shell command timed out after %s", opts.Timeout))
		res.ExitCode = 124
		res.Err = ctx.Err()
		return res
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			res.Err = err
			return res
		}
		warn(fmt.Sprintf("OSError in shell command: %v", err))
		res.ExitCode = 1
		res.Err = err
	}
	return res
}

// writeToOut writes data to w, appending a trailing newline if data is
// non-empty and doesn't already end with one. Used by runCmd / runShell to
// keep captured stdout/stderr neatly separated when routed to a task buffer.
func writeToOut(w io.Writer, data []byte) {
	if len(data) == 0 {
		return
	}
	_, _ = w.Write(data)
	if data[len(data)-1] != '\n' {
		_, _ = w.Write([]byte{'\n'})
	}
}

// hasCmdReal is shutil.which() — returns true if name resolves on PATH.
func hasCmdReal(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// probeReal is a short, read-only command invocation used for "is this installed"
// checks. Returns (result, true) on completion (including non-zero exit) and
// (zero, false) on timeout/launch failure.
func probeReal(argv []string, timeout time.Duration) (CmdResult, bool) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := CmdResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if ctx.Err() == context.DeadlineExceeded {
		warn(fmt.Sprintf("%q probe timed out", argv[0]))
		return res, false
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, true
		}
		warn(fmt.Sprintf("%q probe failed: %v", argv[0], err))
		return res, false
	}
	return res, true
}
