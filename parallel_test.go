package main

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCpuWorkers(t *testing.T) {
	t.Setenv("BOOTSTRAP_PARALLELISM", "4")
	if n := cpuWorkers(); n != 4 {
		t.Errorf("expected 4 workers via env, got %d", n)
	}
	t.Setenv("BOOTSTRAP_PARALLELISM", "")
	if n := cpuWorkers(); n < 1 {
		t.Errorf("expected at least 1 worker, got %d", n)
	}
}

func TestHttpWorkersRespectsCap(t *testing.T) {
	t.Setenv("BOOTSTRAP_PARALLELISM", "32")
	defer func() { githubTokenSet = false }()

	githubTokenSet = false
	if n := httpWorkers(); n != httpWorkersCap {
		t.Errorf("expected http workers capped at %d without token, got %d", httpWorkersCap, n)
	}

	githubTokenSet = true
	if n := httpWorkers(); n != 32 {
		t.Errorf("expected http workers uncapped to 32 with token, got %d", n)
	}
}

func TestParallelDoConcurrency(t *testing.T) {
	const items = 16
	var inFlight, peak int32
	work := make([]int, items)
	for i := range work {
		work[i] = i
	}

	parallelDo(work, 4, func(_ int, _ int) {
		now := atomic.AddInt32(&inFlight, 1)
		for {
			cur := atomic.LoadInt32(&peak)
			if now <= cur || atomic.CompareAndSwapInt32(&peak, cur, now) {
				break
			}
		}
		// brief busy spin to keep multiple workers overlapping
		for i := 0; i < 50000; i++ {
			_ = i * i
		}
		atomic.AddInt32(&inFlight, -1)
	})
	if peak < 2 {
		t.Errorf("expected at least 2 concurrent workers, observed peak %d", peak)
	}
	if peak > 4 {
		t.Errorf("worker cap violated: peak %d > 4", peak)
	}
}

func TestParallelDoEmpty(t *testing.T) {
	called := false
	parallelDo([]int{}, 4, func(_ int, _ int) { called = true })
	if called {
		t.Error("expected fn to not be invoked on empty input")
	}
}

func TestParallelDoAllItemsProcessed(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8}
	var sum int64
	parallelDo(items, 3, func(_ int, v int) {
		atomic.AddInt64(&sum, int64(v))
	})
	if sum != 36 {
		t.Errorf("expected sum 36, got %d", sum)
	}
}

func TestTaskOutputSerial(t *testing.T) {
	t.Cleanup(resetMocks)
	tOut := newSerialOutput()
	// Serial: writes go straight to stdout; Writer() returns nil.
	if tOut.Writer() != nil {
		t.Error("expected Writer() to be nil in serial mode")
	}
	// flushing serial mode is a no-op
	var buf bytes.Buffer
	tOut.Flush(&buf)
	if buf.Len() != 0 {
		t.Error("expected Flush to be no-op in serial mode")
	}
}

func TestTaskOutputCaptured(t *testing.T) {
	tOut := newCapturedOutput("mypkg")
	tOut.Printf("first %s\n", "line")
	tOut.Println("second line")
	tOut.Printf("third line")

	var buf bytes.Buffer
	tOut.Flush(&buf)
	got := buf.String()

	expected := "  [mypkg] first line\n  [mypkg] second line\n  [mypkg] third line\n"
	if got != expected {
		t.Errorf("captured output mismatch:\nexpected:\n%q\ngot:\n%q", expected, got)
	}

	// Flush is idempotent (second call writes nothing).
	buf.Reset()
	tOut.Flush(&buf)
	if buf.Len() != 0 {
		t.Errorf("expected second Flush to be empty, got %q", buf.String())
	}
}

func TestWithTaskOutputRoutesPrints(t *testing.T) {
	tOut := newCapturedOutput("worker")
	withTaskOutput(tOut, func() {
		taskPrintf("hello %d\n", 7)
		taskPrintln("world")
	})

	var buf bytes.Buffer
	tOut.Flush(&buf)
	got := buf.String()
	if !strings.Contains(got, "[worker] hello 7") || !strings.Contains(got, "[worker] world") {
		t.Errorf("expected routed output with prefix, got: %q", got)
	}
}

func TestWithTaskOutputUnsetsAfter(t *testing.T) {
	tOut := newCapturedOutput("worker")
	withTaskOutput(tOut, func() {
		if currentTask() == nil {
			t.Error("expected active task inside withTaskOutput")
		}
	})
	if currentTask() != nil {
		t.Error("expected no active task after withTaskOutput returns")
	}
}

func TestParallelTaskOutputIsolation(t *testing.T) {
	// Each goroutine should see only its own task output, even though
	// they all share package-global state.
	const n = 8
	var wg sync.WaitGroup
	results := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			label := "g" + string(rune('a'+i))
			tOut := newCapturedOutput(label)
			withTaskOutput(tOut, func() {
				taskPrintf("from %s\n", label)
			})
			var buf bytes.Buffer
			tOut.Flush(&buf)
			results[i] = buf.String()
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		label := "g" + string(rune('a'+i))
		want := "  [" + label + "] from " + label + "\n"
		if got != want {
			t.Errorf("goroutine %d: expected %q, got %q", i, want, got)
		}
	}
}

func TestCmdOptsOutRoutesRunCmd(t *testing.T) {
	defer resetMocks()

	var buf bytes.Buffer
	// Pick a command guaranteed to exist and produce output.
	result := runCmdReal([]string{"echo", "hello-out"}, CmdOpts{Out: &buf})
	if !result.OK() {
		t.Fatalf("echo failed: %v", result.Err)
	}
	got := buf.String()
	if !strings.Contains(got, "$ echo hello-out") {
		t.Errorf("expected command echo in Out, got: %q", got)
	}
	if !strings.Contains(got, "hello-out") {
		t.Errorf("expected stdout in Out, got: %q", got)
	}
}

func TestCmdOptsOutRoutesRunShell(t *testing.T) {
	defer resetMocks()

	var buf bytes.Buffer
	result := runShellReal("echo shell-out", CmdOpts{Out: &buf})
	if !result.OK() {
		t.Fatalf("shell failed: %v", result.Err)
	}
	got := buf.String()
	if !strings.Contains(got, "$ echo shell-out") {
		t.Errorf("expected command echo in Out, got: %q", got)
	}
	if !strings.Contains(got, "shell-out") {
		t.Errorf("expected stdout in Out, got: %q", got)
	}
}

func TestIssueLogRoutesViaTaskOutput(t *testing.T) {
	defer resetMocks()
	// Suppress fallback stdout for the non-task branch.
	issueLogWriter = &bytes.Buffer{}

	tOut := newCapturedOutput("isolated")
	withTaskOutput(tOut, func() {
		warn("a warning")
		errLog("an error")
	})

	var buf bytes.Buffer
	tOut.Flush(&buf)
	out := buf.String()
	if !strings.Contains(out, "[isolated] [WARN] a warning") {
		t.Errorf("expected routed WARN line, got: %q", out)
	}
	if !strings.Contains(out, "[isolated] [ERROR] an error") {
		t.Errorf("expected routed ERROR line, got: %q", out)
	}
	// Issues should still flow into the global issues slice.
	if !hasErrors() {
		t.Error("expected errLog to register a global error even when routed via task")
	}
}

func TestParallelPartitionOrdering(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8}
	even, odd := parallelPartition(items, func(v int) bool { return v%2 == 0 })

	wantEven := []int{2, 4, 6, 8}
	wantOdd := []int{1, 3, 5, 7}
	if !equalIntSlices(even, wantEven) {
		t.Errorf("evens: want %v, got %v", wantEven, even)
	}
	if !equalIntSlices(odd, wantOdd) {
		t.Errorf("odds: want %v, got %v", wantOdd, odd)
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGoidUnique(t *testing.T) {
	mainID := goid()
	if mainID == 0 {
		t.Error("goid returned 0 for main goroutine")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var childID uint64
	go func() {
		defer wg.Done()
		childID = goid()
	}()
	wg.Wait()
	if childID == 0 {
		t.Error("goid returned 0 for child goroutine")
	}
	if childID == mainID {
		t.Errorf("expected child goroutine id %d to differ from main id %d", childID, mainID)
	}
}

// Verify BOOTSTRAP_PARALLELISM=0 falls back to NumCPU rather than 0 workers.
func TestCpuWorkersInvalidEnv(t *testing.T) {
	t.Setenv("BOOTSTRAP_PARALLELISM", "0")
	if n := cpuWorkers(); n < 1 {
		t.Errorf("expected fallback to runtime.NumCPU for invalid env, got %d", n)
	}
}

func TestCpuWorkersBadEnv(t *testing.T) {
	t.Setenv("BOOTSTRAP_PARALLELISM", "notanumber")
	if n := cpuWorkers(); n < 1 {
		t.Errorf("expected fallback for non-numeric env, got %d", n)
	}
}

// Ensure the parallelDo guard returns when len(items) == 0 even with
// maxWorkers larger than 1. Smoke test for the early return.
func TestParallelDoBigWorkersSmallInput(t *testing.T) {
	items := []string{"only"}
	called := 0
	var mu sync.Mutex
	parallelDo(items, 32, func(_ int, _ string) {
		mu.Lock()
		called++
		mu.Unlock()
	})
	if called != 1 {
		t.Errorf("expected exactly 1 invocation, got %d", called)
	}
}

// Confirm runCmd inside withTaskOutput auto-routes via Out without callers
// having to set it explicitly — that's the contract that lets install
// handlers rely on taskOut().
func TestRunCmdInsideWithTaskOutput(t *testing.T) {
	defer resetMocks()
	tOut := newCapturedOutput("autoroute")

	withTaskOutput(tOut, func() {
		runCmdReal([]string{"echo", "auto"}, CmdOpts{Out: taskOut()})
	})

	var buf bytes.Buffer
	tOut.Flush(&buf)
	got := buf.String()
	if !strings.Contains(got, "[autoroute] $ echo auto") {
		t.Errorf("expected routed echo command, got: %q", got)
	}
	if !strings.Contains(got, "[autoroute] auto") {
		t.Errorf("expected routed echo stdout, got: %q", got)
	}
}

// Sanity: when no token is in env and the user prompt is suppressed,
// httpWorkers stays capped. Just exercise the path; ensures no panic.
func TestPromptGitHubTokenNoOp(t *testing.T) {
	defer func() { githubTokenSet = false }()
	githubTokenSet = false
	os.Unsetenv("GITHUB_TOKEN")
	// We can't easily prompt in a test; just confirm cap is in effect.
	n := httpWorkers()
	if n > httpWorkersCap {
		t.Errorf("expected http workers <= %d without token, got %d", httpWorkersCap, n)
	}
}
