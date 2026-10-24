//go:build !windows

package lrtl

import "os"

func removeMovedOriginal(path string) error { return os.Remove(path) }
