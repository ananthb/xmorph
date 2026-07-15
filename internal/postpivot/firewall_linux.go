//go:build linux

package postpivot

import "github.com/google/nftables"

// FlushFirewall clears the host's nftables ruleset so the pivoted rootfs
// starts from an empty packet filter. Done natively over netlink — no
// shelling out to iptables/nft. On modern systems iptables is an nftables
// front-end (iptables-nft), so flushing the nft ruleset also clears those
// rules; a legacy xt_tables setup (rare now) is not touched. Best-effort:
// any error is ignored — the goal is "the rootfs's view is empty," and
// there may simply be no ruleset or no netfilter support.
func FlushFirewall() {
	c, err := nftables.New()
	if err != nil {
		return
	}
	c.FlushRuleset()
	_ = c.Flush()
}
