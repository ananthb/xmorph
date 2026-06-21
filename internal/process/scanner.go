// Package process scans /proc for running processes and decides which
// are essential (must be preserved through the pivot) vs which can be
// terminated. Mirrors src/process/* in the Zig version.
package process

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Info is the per-process info we extract from /proc/<pid>.
// Cmdline is the NUL-separated argv from /proc/<pid>/cmdline.
type Info struct {
	PID     int
	PPID    int
	Comm    string // /proc/<pid>/comm (process name, possibly truncated to 15 chars)
	Cmdline string // first arg of /proc/<pid>/cmdline (or comm if empty)
}

// IsKernelThread is true for processes with no executable mapping
// (i.e. PPID 2 or kernel-thread descendants). The cheap heuristic
// — empty /proc/<pid>/cmdline — matches src/process/scanner.zig's
// detection and the runz library's isKernelThread.
func (i *Info) IsKernelThread() bool {
	return i.Cmdline == ""
}

// Scan returns all currently running processes. Mirrors
// src/process/scanner.zig:scanProcesses.
func Scan() ([]Info, error) {
	return scanRoot("/proc")
}

func scanRoot(procRoot string) ([]Info, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		info, err := readProc(procRoot, pid)
		if err != nil {
			// Process may have exited between ReadDir and readProc;
			// treat as transient.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// Get returns the Info for a single PID.
func Get(pid int) (Info, error) {
	return readProc("/proc", pid)
}

// IsRunning is a cheap presence check.
func IsRunning(pid int) bool {
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	return err == nil
}

func readProc(procRoot string, pid int) (Info, error) {
	base := filepath.Join(procRoot, strconv.Itoa(pid))
	commData, err := os.ReadFile(filepath.Join(base, "comm"))
	if err != nil {
		return Info{}, err
	}
	comm := strings.TrimRight(string(commData), "\n")

	cmdlineData, err := os.ReadFile(filepath.Join(base, "cmdline"))
	if err != nil {
		return Info{}, err
	}
	cmdline := strings.SplitN(string(cmdlineData), "\x00", 2)[0]

	ppid := readPPID(filepath.Join(base, "status"))

	return Info{
		PID:     pid,
		PPID:    ppid,
		Comm:    comm,
		Cmdline: cmdline,
	}, nil
}

// readPPID extracts PPid from /proc/<pid>/status. Returns 0 if not
// found — fine for the essential-process classifier which never
// reasons about PPID 0 specifically.
func readPPID(statusPath string) int {
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err == nil {
				return n
			}
		}
	}
	return 0
}
