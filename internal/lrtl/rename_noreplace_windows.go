package lrtl

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func renameDirectoryNoReplace(source, destination string) error {
	widePath := func(path string) (*uint16, error) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(absolute, `\\?\`) {
			if strings.HasPrefix(absolute, `\\`) {
				absolute = `\\?\UNC\` + absolute[2:]
			} else {
				absolute = `\\?\` + absolute
			}
		}
		return windows.UTF16PtrFromString(absolute)
	}
	from, err := widePath(source)
	if err != nil {
		return err
	}
	to, err := widePath(destination)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}
