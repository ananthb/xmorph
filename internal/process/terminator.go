package process

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// TerminateOptions controls Terminate. Mirrors src/process/terminator.zig:9-22.
type TerminateOptions struct {
	GracefulTimeout time.Duration // SIGTERM grace period
	KillTimeout     time.Duration // wait after SIGKILL before giving up
	SkipEssential   bool          // honor IsEssential
	ExcludePIDs     []int         // explicit exclusion list
}

// TerminateResult reports what happened.
type TerminateResult struct {
	Terminated  int // count of processes that exited (gracefully or via SIGKILL)
	Killed      int // count that required SIGKILL
	StubbornPID []int
}

// Terminate sends SIGTERM to every non-essential process, waits for
// GracefulTimeout, then SIGKILLs anything still alive and waits
// KillTimeout. Mirrors src/process/terminator.zig:46-158.
func Terminate(opts TerminateOptions) (TerminateResult, error) {
	if opts.GracefulTimeout <= 0 {
		opts.GracefulTimeout = 5 * time.Second
	}
	if opts.KillTimeout <= 0 {
		opts.KillTimeout = 2 * time.Second
	}

	procs, err := Scan()
	if err != nil {
		return TerminateResult{}, err
	}

	self := os.Getpid()
	parent := os.Getppid()
	excluded := func(p Info) bool {
		if p.PID == self || p.PID == parent {
			return true
		}
		for _, pid := range opts.ExcludePIDs {
			if p.PID == pid {
				return true
			}
		}
		if opts.SkipEssential && IsEssential(&p) {
			return true
		}
		// Kernel threads have no cmdline; always preserve.
		if p.IsKernelThread() {
			return true
		}
		return false
	}

	var targets []int
	for _, p := range procs {
		if !excluded(p) {
			targets = append(targets, p.PID)
		}
	}

	// SIGTERM phase.
	for _, pid := range targets {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	deadline := time.Now().Add(opts.GracefulTimeout)
	gracefulPolling(targets, deadline)

	result := TerminateResult{}
	var stillRunning []int
	for _, pid := range targets {
		if IsRunning(pid) {
			stillRunning = append(stillRunning, pid)
		} else {
			result.Terminated++
		}
	}

	// SIGKILL phase.
	for _, pid := range stillRunning {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		result.Killed++
	}
	if len(stillRunning) > 0 {
		time.Sleep(opts.KillTimeout)
		for _, pid := range stillRunning {
			if IsRunning(pid) {
				result.StubbornPID = append(result.StubbornPID, pid)
			} else {
				result.Terminated++
			}
		}
	}

	if len(result.StubbornPID) > 0 {
		// Caller decides whether non-empty stubborn list is fatal.
		return result, errors.New("some processes did not terminate")
	}
	return result, nil
}

func gracefulPolling(pids []int, deadline time.Time) {
	for time.Now().Before(deadline) {
		alive := 0
		for _, pid := range pids {
			if IsRunning(pid) {
				alive++
			}
		}
		if alive == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
