//go:build !linux

package postpivot

import "os"

func doReboot(_ string) { os.Exit(1) }
