package lrtl

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMoveSequenceCrossFilesystem(t *testing.T) {
	parent, err := os.MkdirTemp("/dev/shm", "lrtl-move-cross-*")
	if err != nil {
		t.Skip("independent writable tmpfs unavailable:", err)
	}
	defer os.RemoveAll(parent)
	source, all, selected := moveSequenceFixture(t)
	a, _ := os.Stat(source)
	b, _ := os.Stat(parent)
	if a.Sys().(*syscall.Stat_t).Dev == b.Sys().(*syscall.Stat_t).Dev {
		t.Skip("same filesystem")
	}
	plan, err := planSequenceMove(source, filepath.Join(parent, "moved"), all, selected, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err != nil {
		t.Fatal(err)
	}
	assertPreviewEVFileBytes(t, filepath.Join(parent, "moved", "a.nef"), []byte("opaque a.nef"))
	if _, err := os.Stat(filepath.Join(source, "a.nef")); !os.IsNotExist(err) {
		t.Fatal("source image left after cross-filesystem move", err)
	}
}
