package postpivot

import (
	"os/exec"
)

// FlushFirewall clears all packet-filter rules across the standard
// commands (iptables, ip6tables, nft). Missing binaries are tolerated
// — the goal is "the rootfs's view is empty," not "every backend was
// scrubbed." Mirrors src/xenomorph-init.zig:104-121.
func FlushFirewall() {
	cmds := [][]string{
		{"iptables", "-F"},
		{"iptables", "-X"},
		{"iptables", "-t", "nat", "-F"},
		{"iptables", "-t", "mangle", "-F"},
		{"ip6tables", "-F"},
		{"ip6tables", "-X"},
		{"nft", "flush", "ruleset"},
	}
	for _, c := range cmds {
		_ = exec.Command(c[0], c[1:]...).Run()
	}
}
