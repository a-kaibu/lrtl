package lrtl

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Do not clear the readonly attribute: other hard links may still need it.
// If this operation is unavailable, keep the held original and report cleanup.
func removeMovedOriginal(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(abs, `\\?\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + abs[2:]
		} else {
			abs = `\\?\` + abs
		}
	}
	wide, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(wide, windows.DELETE|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return &os.PathError{Op: "remove moved original", Path: path, Err: err}
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	if err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), 4); err != nil {
		// Older filesystems may lack extended disposition. DeleteFile itself
		// does not clear attributes, so it is safe for non-readonly files.
		if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
			if fallback := windows.DeleteFile(wide); fallback == nil {
				return nil
			}
		}
		return &os.PathError{Op: "remove moved original", Path: path, Err: err}
	}
	return nil
}
