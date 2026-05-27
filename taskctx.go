package main

import (
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Goroutine-local task output routing.
//
// Why: install handlers in custom.go and post.go are deeply nested calls
// that use fmt.Printf/Println directly and pass CmdOpts to runCmd. To route
// their output into a per-task buffer (so parallel workers don't interleave
// on os.Stdout), we'd otherwise need to thread an io.Writer through every
// signature — ~30 call sites of churn including tests.
//
// Instead we keep a sync.Map keyed by goroutine id. The parallel orchestrator
// associates a taskOutput with its worker goroutine before invoking the
// handler; helpers below check the map and route output to the active task
// when present, falling back to direct stdout otherwise. Sequential callers
// observe no behavior change.
//
// goid() uses runtime.Stack — a small hack, but stable and idiomatic for
// goroutine-local state where context.Context threading would dwarf the
// surrounding work.

var activeTaskByGoroutine sync.Map // map[uint64]*taskOutput

func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := string(buf[:n])
	s = strings.TrimPrefix(s, "goroutine ")
	end := strings.IndexByte(s, ' ')
	if end < 0 {
		return 0
	}
	id, _ := strconv.ParseUint(s[:end], 10, 64)
	return id
}

// withTaskOutput pins tOut to the current goroutine for the duration of fn,
// then unpins. Re-entrant calls overwrite the previous binding and restore
// it on return. A nil tOut is treated as "no binding" (sequential mode).
func withTaskOutput(tOut *taskOutput, fn func()) {
	if tOut == nil {
		fn()
		return
	}
	id := goid()
	prev, hadPrev := activeTaskByGoroutine.Load(id)
	activeTaskByGoroutine.Store(id, tOut)
	defer func() {
		if hadPrev {
			activeTaskByGoroutine.Store(id, prev)
		} else {
			activeTaskByGoroutine.Delete(id)
		}
	}()
	fn()
}

// currentTask returns the taskOutput pinned to the current goroutine, or
// nil if none. Cheap enough to call per print (~microseconds).
func currentTask() *taskOutput {
	v, ok := activeTaskByGoroutine.Load(goid())
	if !ok {
		return nil
	}
	return v.(*taskOutput)
}

// taskPrintf routes via the active task (if any) or directly to stdout.
func taskPrintf(format string, args ...any) {
	if t := currentTask(); t != nil {
		t.Printf(format, args...)
		return
	}
	fmt.Printf(format, args...)
}

// taskPrintln routes via the active task (if any) or directly to stdout.
func taskPrintln(args ...any) {
	if t := currentTask(); t != nil {
		t.Println(args...)
		return
	}
	fmt.Println(args...)
}

// taskOut returns the io.Writer that runCmd / runShell should target via
// CmdOpts.Out for the active task. Returns nil when there is no active task,
// which preserves runCmd's default streamed-to-stdout behavior.
func taskOut() io.Writer {
	if t := currentTask(); t != nil {
		return t.Writer()
	}
	return nil
}
