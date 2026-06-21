package postpivot

import "strings"

// hostnameFromArgs scans `tailscale up` args for --hostname=<host> and
// returns its value, or empty string when absent. Used by Run to pass
// the hostname through to tsnetauth.PostPivot so the tailnet listener
// reattaches under the same identity that PreAuth registered.
func hostnameFromArgs(args string) string {
	for _, tok := range strings.Fields(args) {
		if rest, ok := strings.CutPrefix(tok, "--hostname="); ok {
			return rest
		}
	}
	return ""
}
