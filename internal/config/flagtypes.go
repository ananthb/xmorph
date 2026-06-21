package config

import (
	"fmt"
	"strconv"
)

// tristateBool implements pflag.Value for a *bool that can be nil
// (not set), true, or false. Setting the bare flag (e.g. --ssh.enable)
// is treated as true via IsBoolFlag(); --ssh.enable=false sets false.
type tristateBool struct {
	dst **bool
}

func (t *tristateBool) String() string {
	if t.dst == nil || *t.dst == nil {
		return ""
	}
	if **t.dst {
		return "true"
	}
	return "false"
}

func (t *tristateBool) Set(s string) error {
	var b bool
	switch s {
	case "", "true":
		b = true
	case "false":
		b = false
	default:
		return fmt.Errorf("invalid bool value %q (expected true or false)", s)
	}
	*t.dst = &b
	return nil
}

func (t *tristateBool) Type() string { return "bool" }

// IsBoolFlag tells pflag that the bare flag form (--ssh.enable with no
// value) means true. Without this pflag would require an explicit value.
func (t *tristateBool) IsBoolFlag() bool { return true }

// tristateUint16 implements pflag.Value for a *uint16 that can be nil.
// Used for --ssh.port so we can distinguish "unset" from "0".
type tristateUint16 struct {
	dst **uint16
}

func (t *tristateUint16) String() string {
	if t.dst == nil || *t.dst == nil {
		return ""
	}
	return strconv.FormatUint(uint64(**t.dst), 10)
}

func (t *tristateUint16) Set(s string) error {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port value %q: %w", s, err)
	}
	v := uint16(n)
	*t.dst = &v
	return nil
}

func (t *tristateUint16) Type() string { return "uint16" }

// layerVar implements pflag.Value for a single --image or --rootfs flag.
// All layerVar instances share the same backing *[]Layer so multiple
// --image / --rootfs flags interleave in argv order. pflag invokes Set()
// in command-line order regardless of flag identity, which preserves the
// layer ordering semantics from src/config.zig:213-217.
type layerVar struct {
	dst  *[]Layer
	kind LayerKind
}

func (l *layerVar) String() string { return "" }

func (l *layerVar) Set(s string) error {
	layer := Layer{Kind: l.kind}
	switch l.kind {
	case LayerImage:
		layer.Ref = s
	case LayerRootfs:
		layer.Path = s
	}
	*l.dst = append(*l.dst, layer)
	return nil
}

func (l *layerVar) Type() string {
	if l.kind == LayerImage {
		return "imageRef"
	}
	return "path"
}

// appendStringVar implements pflag.Value as an always-appending []string.
// Used for --command / --cmd: both flags share the same backing slice and
// every invocation appends. (StringArrayVar from pflag replaces on the
// first Set, which breaks the alias-sharing case.)
type appendStringVar struct {
	dst *[]string
}

func (a *appendStringVar) String() string { return "" }
func (a *appendStringVar) Type() string   { return "string" }
func (a *appendStringVar) Set(s string) error {
	*a.dst = append(*a.dst, s)
	return nil
}
