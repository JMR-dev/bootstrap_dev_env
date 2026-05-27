package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
)

// cpuWorkers returns the parallelism level for install/check work.
// Defaults to runtime.NumCPU(); overridable via BOOTSTRAP_PARALLELISM
// (e.g. for tests / constrained hosts) and clamped to >=1.
func cpuWorkers() int {
	if v := os.Getenv("BOOTSTRAP_PARALLELISM"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n >= 1 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}

// httpWorkersCap is the polite ceiling for HTTP-bound concurrency when no
// GitHub token has been provided (GitHub anon rate-limits at 60/hour).
// Authenticated requests get 5000/hour so we lift the cap when a token is
// available — see githubTokenSet.
const httpWorkersCap = 8

var githubTokenSet bool

// httpWorkers caps cpuWorkers() to httpWorkersCap unless a GitHub token has
// been supplied (in which case we use the full processor count).
func httpWorkers() int {
	n := cpuWorkers()
	if githubTokenSet {
		return n
	}
	if n > httpWorkersCap {
		return httpWorkersCap
	}
	return n
}

// parallelDo runs fn(i, items[i]) over items with at most maxWorkers
// goroutines in flight. Returns once every task has finished. Order of
// completion is not guaranteed; fn is responsible for its own synchronization
// when writing shared state.
func parallelDo[T any](items []T, maxWorkers int, fn func(i int, item T)) {
	if len(items) == 0 {
		return
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	if maxWorkers > len(items) {
		maxWorkers = len(items)
	}
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i, item)
		}(i, item)
	}
	wg.Wait()
}

// taskOutput is the per-call sink for status text and subprocess output.
//
// Two modes:
//
//	Sequential (label==""): Printf goes straight to os.Stdout, and Writer()
//	returns nil so runCmd falls back to its default streamed-to-stdout mode.
//	Behavior matches the pre-parallelism code exactly.
//
//	Captured  (label!=""):  Printf and runCmd output both land in an internal
//	buffer; Flush() prints the whole block at once with a "  [label] " prefix
//	on every line. Used by parallel install workers so concurrent output
//	doesn't interleave.
type taskOutput struct {
	label string
	buf   bytes.Buffer
	mu    sync.Mutex
}

func newSerialOutput() *taskOutput        { return &taskOutput{} }
func newCapturedOutput(label string) *taskOutput {
	return &taskOutput{label: label}
}

// Printf writes to the task's destination.
func (t *taskOutput) Printf(format string, args ...any) {
	if t.label == "" {
		fmt.Printf(format, args...)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(&t.buf, format, args...)
}

// Println writes a line to the task's destination.
func (t *taskOutput) Println(args ...any) {
	if t.label == "" {
		fmt.Println(args...)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintln(&t.buf, args...)
}

// Writer returns the io.Writer that runCmd/runShell should target via
// CmdOpts.Out. Returns nil in sequential mode (preserves streamed stdout).
func (t *taskOutput) Writer() io.Writer {
	if t.label == "" {
		return nil
	}
	return &lockingWriter{mu: &t.mu, w: &t.buf}
}

// Flush emits the captured buffer to w with the task label prefixed onto
// every line. Idempotent and a no-op in sequential mode.
func (t *taskOutput) Flush(w io.Writer) {
	if t.label == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.buf.Len() == 0 {
		return
	}
	if w == nil {
		w = os.Stdout
	}
	prefix := fmt.Sprintf("  [%s] ", t.label)
	lines := bytes.Split(t.buf.Bytes(), []byte{'\n'})
	for i, line := range lines {
		if i == len(lines)-1 && len(line) == 0 {
			break
		}
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
	t.buf.Reset()
}

// lockingWriter is a thin io.Writer that holds the taskOutput mutex while
// writing, so runCmd / runShell can stream into the buffer concurrently with
// status Printf calls on the same task without corrupting the buffer.
type lockingWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockingWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
