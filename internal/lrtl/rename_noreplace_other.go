//go:build !linux && !windows

package lrtl

import (
	"fmt"
	"runtime"
)

func renameDirectoryNoReplace(source, destination string) error {
	return fmt.Errorf("directory rename without replacement is not supported on %s", runtime.GOOS)
}
