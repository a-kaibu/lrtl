package lrtl

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameDirectoryNoReplace(source, destination string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}
