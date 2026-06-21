package pivot

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// CleanupOldRoot lazily unmounts every mount under oldRoot, deepest
// first (longest path = deepest), so the kernel can release them in
// reverse-creation order. Mirrors src/pivot/mounts.zig:117-159.
//
// Errors on individual unmounts are tolerated (logged via the returned
// slice); only enumeration failure is returned.
func CleanupOldRoot(oldRoot string) ([]string, error) {
	mounts, err := readMountTargets()
	if err != nil {
		return nil, fmt.Errorf("read /proc/self/mounts: %w", err)
	}

	var under []string
	for _, m := range mounts {
		if strings.HasPrefix(m, oldRoot) {
			under = append(under, m)
		}
	}

	// Deepest first: longer paths come before their parents so MNT_DETACH
	// at the leaf doesn't fight with submounts still attached.
	sort.Slice(under, func(i, j int) bool {
		return len(under[i]) > len(under[j])
	})

	var failed []string
	for _, m := range under {
		if err := unix.Unmount(m, unix.MNT_DETACH); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", m, err))
		}
	}
	return failed, nil
}

// readMountTargets reads /proc/self/mounts and returns the mount-point
// column. Format: source target fstype options dump pass.
func readMountTargets() ([]string, error) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		out = append(out, fields[1])
	}
	return out, sc.Err()
}
