package lrtl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemoveSequenceRepeatedSharedCompanions(t *testing.T) {
	requireRenamePlatform(t)
	source, _, _ := moveSequenceFixture(t)
	destination := source + "_REMOVED"
	before := renameTreeSnapshot(t, source)
	var out, stderr bytes.Buffer
	args := []string{"remove-sequence", source, "--filename-contains", "a.nef"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("preview changed source")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created output")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	first := renameTreeSnapshot(t, destination)
	if len(first) != 13 { // Eight files and five directories.
		t.Fatal("wrong first bundle", first)
	}
	if Main([]string{"remove-sequence", source, "--filename-contains", "a.png", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := renameTreeSnapshot(t, destination)
	if len(after) != 14 {
		t.Fatal("wrong repeated bundle", after)
	}
	for name, value := range first {
		if after[name] != value {
			t.Fatal("existing output changed", name)
		}
	}
	for _, name := range []string{"a.nef", "a.jpg", "a.png", "a.xmp", "a.nef.xmp", "a.jpg.xmp", ".lrt/previews/a.lrtpreview", ".lrt/visual/a.lrtpreview", ".lrt/proxy/a.proxy"} {
		name = filepath.FromSlash(name)
		if after[name] != before[name] {
			t.Fatal("removed file changed", name)
		}
		if _, err := os.Lstat(filepath.Join(source, name)); !os.IsNotExist(err) {
			t.Fatal("moved file still in source", name, err)
		}
	}
	for _, name := range []string{"b.nef", "b.xmp", ".lrt/settings.json"} {
		name = filepath.FromSlash(name)
		if renameTreeSnapshot(t, source)[name] != before[name] {
			t.Fatal("unselected file changed", name)
		}
	}
}

func TestMoveSequenceMergeRefusals(t *testing.T) {
	requireRenamePlatform(t)
	for _, kind := range []string{"image", "different-xmp", "case-image", "case-directory", "parent-file", "late-file", "changed-reuse", "replaced-directory"} {
		t.Run(kind, func(t *testing.T) {
			source, all, selected := moveSequenceFixture(t)
			dest := filepath.Join(t.TempDir(), "existing")
			companionWrite(t, dest, "unrelated.txt", []byte("preserve"))
			switch kind {
			case "image":
				companionWrite(t, dest, "a.nef", []byte("collision"))
			case "different-xmp":
				companionWrite(t, dest, "a.xmp", []byte("different settings"))
			case "case-image":
				companionWrite(t, dest, "A.NEF", []byte("collision"))
			case "case-directory":
				companionWrite(t, dest, ".LRT/unrelated", []byte("preserve"))
			case "parent-file":
				companionWrite(t, dest, ".lrt", []byte("not directory"))
			case "changed-reuse":
				companionWrite(t, dest, "a.xmp", []byte("opaque a.xmp"))
			}
			beforeSource := renameTreeSnapshot(t, source)
			plan, err := planSequenceMove(source, dest, all, selected, true, true)
			if kind == "late-file" || kind == "changed-reuse" || kind == "replaced-directory" {
				if err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "late-file":
					companionWrite(t, dest, "a.nef", []byte("appeared"))
				case "changed-reuse":
					companionWrite(t, dest, "a.xmp", []byte("external edit"))
				case "replaced-directory":
					if err := os.Rename(dest, dest+"-old"); err != nil {
						t.Fatal(err)
					}
					companionWrite(t, dest, "replacement", []byte("preserve"))
				}
				beforeDest := renameTreeSnapshot(t, dest)
				err = executeSequenceMove(plan, sequenceMoveOps{})
				if !reflect.DeepEqual(beforeDest, renameTreeSnapshot(t, dest)) {
					t.Fatal("failed move changed destination")
				}
			}
			if err == nil {
				t.Fatal("accepted collision/change", kind)
			}
			if !reflect.DeepEqual(beforeSource, renameTreeSnapshot(t, source)) {
				t.Fatal("failed move changed source")
			}
		})
	}
}

func TestMoveSequenceMergePublicationRollback(t *testing.T) {
	requireRenamePlatform(t)
	for _, kind := range []string{"ordinary", "nested-directory", "undo-failure", "external-edit", "late-collision", "cleanup-failure"} {
		t.Run(kind, func(t *testing.T) {
			source, all, selected := moveSequenceFixture(t)
			dest := filepath.Join(t.TempDir(), "existing")
			companionWrite(t, dest, "unrelated.txt", []byte("preserve"))
			plan, err := planSequenceMove(source, dest, all, selected, true, true)
			if err != nil {
				t.Fatal(err)
			}
			beforeSource, beforeDest := renameTreeSnapshot(t, source), renameTreeSnapshot(t, dest)
			publications := 0
			var firstTarget string
			ops := sequenceMoveOps{move: func(from, to string) error {
				publishing := pathContains(dest, to)
				undo := pathContains(dest, from)
				if undo && kind == "undo-failure" {
					return fmt.Errorf("injected undo failure")
				}
				if publishing {
					publications++
					failAt := 2
					if kind == "nested-directory" {
						failAt = 4
					}
					if publications == failAt && kind != "cleanup-failure" {
						if kind == "external-edit" {
							companionWrite(t, dest, filepath.Base(firstTarget), []byte("external edit"))
						}
						if kind == "late-collision" {
							if err := os.WriteFile(to, []byte("external collision"), 0600); err != nil {
								return err
							}
							return renameDirectoryNoReplace(from, to)
						}
						return fmt.Errorf("injected merge publication failure")
					}
					if publications == 1 {
						firstTarget = to
					}
				}
				return renameDirectoryNoReplace(from, to)
			}}
			if kind == "cleanup-failure" {
				ops.remove = func(string) error { return fmt.Errorf("injected cleanup failure") }
			}
			err = executeSequenceMove(plan, ops)
			if err == nil {
				t.Fatal("missing failure")
			}
			if kind == "cleanup-failure" {
				if !strings.Contains(err.Error(), "move published at") {
					t.Fatal("missing publication status", err)
				}
				assertPreviewEVFileBytes(t, filepath.Join(dest, "a.nef"), []byte("opaque a.nef"))
			} else {
				// Incomplete merge undo leaves its journal in the source.
				after := renameTreeSnapshot(t, source)
				for key, value := range beforeSource {
					if after[key] != value {
						t.Fatal("original not restored", key, err)
					}
				}
				if (kind == "ordinary" || kind == "nested-directory") && !reflect.DeepEqual(beforeDest, renameTreeSnapshot(t, dest)) {
					t.Fatal("ordinary rollback changed existing output")
				}
				if kind == "external-edit" {
					assertPreviewEVFileBytes(t, firstTarget, []byte("external edit"))
				}
				if kind == "undo-failure" || kind == "external-edit" {
					if !strings.Contains(err.Error(), "partial merge rollback incomplete") {
						t.Fatal("missing recovery status", err)
					}
					journals, _ := filepath.Glob(filepath.Join(source, ".lrtl-move-*", "move-journal.json"))
					if len(journals) != 1 {
						t.Fatal("recovery journal missing", journals)
					}
				}
			}
			assertPreviewEVFileBytes(t, filepath.Join(dest, "unrelated.txt"), []byte("preserve"))
		})
	}
}

func TestMoveSequenceMergeCLI(t *testing.T) {
	requireRenamePlatform(t)
	source, _, _ := moveSequenceFixture(t)
	dest := t.TempDir()
	companionWrite(t, dest, "unrelated", []byte("preserve"))
	var out, stderr bytes.Buffer
	args := []string{"move-sequence", source, "--output", dest, "--filename-contains", "a.nef"}
	if Main(args, &out, &stderr) == 0 {
		t.Fatal("accepted existing directory without --merge")
	}
	beforeSource, beforeDest := renameTreeSnapshot(t, source), renameTreeSnapshot(t, dest)
	args = append(args, "--merge")
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(beforeSource, renameTreeSnapshot(t, source)) || !reflect.DeepEqual(beforeDest, renameTreeSnapshot(t, dest)) {
		t.Fatal("merge preview changed files")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	assertPreviewEVFileBytes(t, filepath.Join(dest, "a.nef"), []byte("opaque a.nef"))
	assertPreviewEVFileBytes(t, filepath.Join(dest, "unrelated"), []byte("preserve"))
}

func TestRemoveSequenceSelectionAVIPixels(t *testing.T) {
	requireRenamePlatform(t)
	source, _ := frameSelectionFixture(t)
	dest := source + "_REMOVED"
	var out, stderr bytes.Buffer
	selection := filepath.Join(t.TempDir(), "selection.json")
	if Main([]string{"select", source, "--ratings", "4,5", "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	render := func(dir string, selected bool) []byte {
		path := filepath.Join(t.TempDir(), "movie.avi")
		args := []string{"render", dir, "--output", path, "--apply-xmp-basic"}
		if selected {
			args = append(args, "--selection", selection)
		}
		if Main(args, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI frames")
		}
		return data[offset:]
	}
	before := render(source, true)
	for _, rating := range []string{"4", "5"} {
		if Main([]string{"remove-sequence", source, "--ratings", rating, "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	if !bytes.Equal(before, render(dest, false)) {
		t.Fatal("repeated removal changed AVI frames")
	}
	frames, err := DiscoverSequence(source)
	if err != nil || len(frames) != 4 {
		t.Fatal("wrong remaining source frames", frames, err)
	}
}

func TestMoveSequenceMergeSymlinkBoundary(t *testing.T) {
	requireRenamePlatform(t)
	source, all, selected := moveSequenceFixture(t)
	dest, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, ".lrt")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	if _, err := planSequenceMove(source, dest, all, selected, true, true); err == nil {
		t.Fatal("accepted linked cache directory")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("wrote outside destination", entries, err)
	}
}

func TestMoveSequenceMergeSharedEditDuringPublication(t *testing.T) {
	requireRenamePlatform(t)
	source, all, selected := moveSequenceFixture(t)
	dest := t.TempDir()
	companionWrite(t, dest, "a.xmp", []byte("opaque a.xmp"))
	plan, err := planSequenceMove(source, dest, all, selected, true, true)
	if err != nil {
		t.Fatal(err)
	}
	before := renameTreeSnapshot(t, source)
	changed := false
	err = executeSequenceMove(plan, sequenceMoveOps{move: func(from, to string) error {
		if err := renameDirectoryNoReplace(from, to); err != nil {
			return err
		}
		if !changed && pathContains(dest, to) {
			changed = true
			companionWrite(t, dest, "a.xmp", []byte("external settings"))
		}
		return nil
	}})
	if err == nil {
		t.Fatal("accepted changed shared destination metadata")
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("source not restored")
	}
	assertPreviewEVFileBytes(t, filepath.Join(dest, "a.xmp"), []byte("external settings"))
	if frames, err := DiscoverSequence(dest); err != nil || len(frames) != 0 {
		t.Fatal("partial images not rolled back", frames, err)
	}
}

func FuzzMoveSequenceMergeRollback(f *testing.F) {
	f.Add([]byte("image bytes"), byte(0), true)
	f.Add([]byte{}, byte(1), false)
	f.Add([]byte("original"), byte(2), true)
	f.Fuzz(func(t *testing.T, data []byte, failure byte, reused bool) {
		requireRenamePlatform(t)
		if len(data) > 4096 {
			return
		}
		source, dest := t.TempDir(), t.TempDir()
		companionWrite(t, source, "a.nef", data)
		companionWrite(t, source, "b.nef", []byte("second"))
		companionWrite(t, source, "a.xmp", []byte("metadata"))
		companionWrite(t, dest, "unrelated", []byte("preserve"))
		if reused {
			companionWrite(t, dest, "a.xmp", []byte("metadata"))
		}
		frames, err := DiscoverSequence(source)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := planSequenceMove(source, dest, frames, frames, false, true)
		if err != nil {
			t.Fatal(err)
		}
		beforeSource, beforeDest := renameTreeSnapshot(t, source), renameTreeSnapshot(t, dest)
		publications := byte(0)
		err = executeSequenceMove(plan, sequenceMoveOps{move: func(from, to string) error {
			if pathContains(dest, to) {
				publications++
				if publications == failure%5 {
					return fmt.Errorf("injected publication failure")
				}
			}
			return renameDirectoryNoReplace(from, to)
		}})
		if err != nil {
			if !reflect.DeepEqual(beforeSource, renameTreeSnapshot(t, source)) || !reflect.DeepEqual(beforeDest, renameTreeSnapshot(t, dest)) {
				t.Fatal("failed merge changed data", err)
			}
		} else {
			assertPreviewEVFileBytes(t, filepath.Join(dest, "a.nef"), data)
			assertPreviewEVFileBytes(t, filepath.Join(dest, "b.nef"), []byte("second"))
			assertPreviewEVFileBytes(t, filepath.Join(dest, "a.xmp"), []byte("metadata"))
			if _, err := os.Lstat(filepath.Join(source, "a.nef")); !os.IsNotExist(err) {
				t.Fatal("source not moved")
			}
		}
		assertPreviewEVFileBytes(t, filepath.Join(dest, "unrelated"), []byte("preserve"))
	})
}
