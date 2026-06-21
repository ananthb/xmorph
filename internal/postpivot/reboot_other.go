//go:build !linux

package postpivot

import "os"

func doReboot() { os.Exit(1) }
