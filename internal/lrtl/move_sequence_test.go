package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func moveSequenceFixture(t *testing.T) (string, []Frame, []Frame) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	for _, name := range []string{"a.nef", "a.jpg", "a.png", "b.nef", "a.xmp", "a.nef.xmp", "a.jpg.xmp", "b.xmp", ".lrt/previews/a.lrtpreview", ".lrt/visual/a.lrtpreview", ".lrt/proxy/a.proxy", ".lrt/settings.json"} {
		companionWrite(t, source, name, []byte("opaque "+name))
	}
	all, err := DiscoverSequence(source)
	if err != nil {
		t.Fatal(err)
	}
	var selected []Frame
	for _, frame := range all {
		if filepath.Base(frame.Image) == "a.nef" {
			selected = append(selected, frame)
		}
	}
	return source, all, selected
}

func TestMoveSequenceBundlesAndSharedMetadata(t *testing.T) {
	requireRenamePlatform(t)
	source, all, selected := moveSequenceFixture(t)
	destination := filepath.Join(t.TempDir(), "moved")
	plan, err := planSequenceMove(source, destination, all, selected, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.copy.frames != 2 || plan.copy.sidecars != 3 || plan.copy.caches != 3 {
		t.Fatal("incorrect move bundle", plan.copy)
	}
	before := renameTreeSnapshot(t, source)
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err != nil {
		t.Fatal(err)
	}
	for i, file := range plan.copy.files {
		key, err := filepath.Rel(source, file.source)
		if err != nil {
			t.Fatal(err)
		}
		value := renameTreeSnapshot(t, destination)[file.name]
		if value != before[key] {
			t.Fatal("moved data, mode or time changed", file.name)
		}
		_, err = os.Lstat(file.source)
		if plan.keep[i] {
			if err != nil {
				t.Fatal("shared metadata removed", file.name)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal("original was not removed", file.name, err)
		}
	}
	for _, name := range []string{"a.png", "b.nef", "b.xmp", "a.xmp", ".lrt/settings.json", ".lrt/proxy/a.proxy"} {
		if renameTreeSnapshot(t, source)[name] != before[name] {
			t.Fatal("unselected content changed", name)
		}
	}
	left, _ := filepath.Glob(filepath.Join(source, ".lrtl-move-*"))
	if len(left) != 0 {
		t.Fatal("holding directory leaked", left)
	}
}

func TestMoveSequenceRollbackAndCommitFailures(t *testing.T) {
	requireRenamePlatform(t)
	for _, kind := range []string{"copy-error", "corrupt-copy", "stage-error", "publish-error", "late-destination", "rollback-error", "cleanup-error", "published-change"} {
		t.Run(kind, func(t *testing.T) {
			source, all, selected := moveSequenceFixture(t)
			destination := filepath.Join(t.TempDir(), "moved")
			plan, err := planSequenceMove(source, destination, all, selected, true)
			if err != nil {
				t.Fatal(err)
			}
			before := renameTreeSnapshot(t, source)
			moves := 0
			ops := sequenceMoveOps{}
			ops.copy = func(from, to string) error {
				if kind == "copy-error" {
					return fmt.Errorf("injected copy failure")
				}
				if err := copyFile(from, to); err != nil {
					return err
				}
				if kind == "corrupt-copy" {
					data := denoiseRead(t, to)
					data[0] ^= 1
					return os.WriteFile(to, data, 0600)
				}
				return nil
			}
			ops.move = func(from, to string) error {
				moves++
				publishing := to == destination
				if kind == "stage-error" && moves == 2 {
					return fmt.Errorf("injected source staging failure")
				}
				if publishing && (kind == "publish-error" || kind == "rollback-error") {
					return fmt.Errorf("injected publication failure")
				}
				if kind == "rollback-error" && strings.Contains(from, ".lrtl-move-") && !publishing && !strings.Contains(to, ".lrtl-move-") {
					return fmt.Errorf("injected rollback failure")
				}
				if err := renameDirectoryNoReplace(from, to); err != nil {
					return err
				}
				if kind == "late-destination" && moves == 1 {
					return os.Mkdir(destination, 0755)
				}
				if kind == "published-change" && publishing {
					return os.WriteFile(filepath.Join(destination, plan.copy.files[0].name), []byte("external edit"), 0600)
				}
				return nil
			}
			if kind == "cleanup-error" {
				ops.remove = func(string) error { return fmt.Errorf("injected cleanup failure") }
			}
			err = executeSequenceMove(plan, ops)
			if err == nil {
				t.Fatal("missing injected failure")
			}
			published := kind == "cleanup-error" || kind == "published-change"
			if published {
				if !strings.Contains(err.Error(), "move published at") {
					t.Fatal("missing commit status", err)
				}
				if _, err := os.Stat(filepath.Join(destination, plan.copy.files[0].name)); err != nil {
					t.Fatal(err)
				}
			} else if kind != "late-destination" {
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatal("published incomplete output")
				}
			}
			if kind == "rollback-error" || published {
				journals, _ := filepath.Glob(filepath.Join(source, ".lrtl-move-*", "move-journal.json"))
				if len(journals) != 1 {
					t.Fatal("missing recovery journal", journals)
				}
				var journal sequenceMoveJournal
				if err := json.Unmarshal(denoiseRead(t, journals[0]), &journal); err != nil {
					t.Fatal(err)
				}
				for _, file := range journal.Files {
					if file.KeepOriginal {
						continue
					}
					held := denoiseRead(t, file.Held)
					if string(held) != "opaque "+filepath.ToSlash(file.Name) {
						t.Fatal("held original changed", file.Name)
					}
				}
				if kind == "rollback-error" {
					if _, err := os.Stat(journal.Staging); err != nil {
						t.Fatal("verified copy discarded during incomplete rollback", err)
					}
				}
			} else {
				if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
					t.Fatal("failed move did not restore source")
				}
				stages, _ := filepath.Glob(filepath.Join(filepath.Dir(destination), ".lrtl-move-copy-*"))
				if len(stages) != 0 {
					t.Fatal("destination staging leaked", stages)
				}
			}
		})
	}
}

func TestMoveSequenceSourceChangesAndPaths(t *testing.T) {
	requireRenamePlatform(t)
	source, all, selected := moveSequenceFixture(t)
	for _, destination := range []string{source, filepath.Join(source, "child")} {
		if _, err := planSequenceMove(source, destination, all, selected, true); err == nil {
			t.Fatal("accepted output inside source")
		}
	}
	plan, err := planSequenceMove(source, filepath.Join(t.TempDir(), "output"), all, selected, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selected[0].Image, []byte("external edit after planning"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err == nil {
		t.Fatal("accepted changed original")
	}
	assertPreviewEVFileBytes(t, selected[0].Image, []byte("external edit after planning"))
}

func TestMoveSequenceReadonlyHardlink(t *testing.T) {
	requireRenamePlatform(t)
	source := filepath.Join(t.TempDir(), "source")
	companionWrite(t, source, "a.nef", []byte("original"))
	a, b := filepath.Join(source, "a.nef"), filepath.Join(source, "b.nef")
	if err := os.Link(a, b); err != nil {
		t.Skip("hard link unavailable:", err)
	}
	if err := os.Chmod(a, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(b, 0600)
	all, err := DiscoverSequence(source)
	if err != nil {
		t.Fatal(err)
	}
	var selected []Frame
	for _, frame := range all {
		if frame.Image == a {
			selected = append(selected, frame)
		}
	}
	destination := filepath.Join(t.TempDir(), "moved")
	plan, err := planSequenceMove(source, destination, all, selected, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(destination, "a.nef"), 0600)
	for _, path := range []string{b, filepath.Join(destination, "a.nef")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0200 != 0 {
			t.Fatal("readonly attribute changed", path, info, err)
		}
		assertPreviewEVFileBytes(t, path, []byte("original"))
	}
}

func TestMoveSequenceReplacedSourceDirectory(t *testing.T) {
	requireRenamePlatform(t)
	source, all, selected := moveSequenceFixture(t)
	plan, err := planSequenceMove(source, filepath.Join(t.TempDir(), "moved"), all, selected, true)
	if err != nil {
		t.Fatal(err)
	}
	old := source + "-original"
	if err := os.Rename(source, old); err != nil {
		t.Fatal(err)
	}
	companionWrite(t, source, "a.nef", []byte("replacement"))
	before := renameTreeSnapshot(t, old)
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err == nil {
		t.Fatal("accepted replaced source directory")
	}
	assertPreviewEVFileBytes(t, filepath.Join(source, "a.nef"), []byte("replacement"))
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, old)) {
		t.Fatal("original directory changed")
	}
}

func TestMoveSequenceCLISelectionAndAVIPixels(t *testing.T) {
	requireRenamePlatform(t)
	source, _ := frameSelectionFixture(t)
	destination := filepath.Join(t.TempDir(), "moved")
	var out, stderr bytes.Buffer
	selection := filepath.Join(t.TempDir(), "selection.json")
	if Main([]string{"select", source, "--ratings", "4,5", "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	render := func(dir string, filter bool) []byte {
		path := filepath.Join(t.TempDir(), "video.avi")
		args := []string{"render", dir, "--output", path, "--apply-xmp-basic"}
		if filter {
			args = append(args, "--selection", selection)
		}
		if Main(args, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI payload")
		}
		return data[offset:]
	}
	video := render(source, true)
	before := renameTreeSnapshot(t, source)
	args := []string{"move-sequence", source, "--output", destination, "--ratings", "4,5"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("preview changed source")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created output")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !bytes.Equal(video, render(destination, false)) {
		t.Fatal("move changed selected AVI frames")
	}
	frames, err := DiscoverSequence(source)
	if err != nil || len(frames) != 4 {
		t.Fatal("wrong remaining source images", frames, err)
	}
	destBefore := renameTreeSnapshot(t, destination)
	if Main(append(args, "--write"), &out, &stderr) == 0 {
		t.Fatal("replaced existing output")
	}
	if !reflect.DeepEqual(destBefore, renameTreeSnapshot(t, destination)) {
		t.Fatal("repeat changed output")
	}
}

func FuzzMoveSequenceRollback(f *testing.F) {
	for _, failure := range []byte{0, 1, 2, 3, 4} {
		f.Add([]byte("original image"), failure, true)
	}
	f.Fuzz(func(t *testing.T, data []byte, failure byte, shared bool) {
		if len(data) > 4096 {
			return
		}
		if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
			return
		}
		source := filepath.Join(t.TempDir(), "source")
		companionWrite(t, source, "frame.nef", data)
		companionWrite(t, source, "frame.xmp", []byte("opaque metadata"))
		if shared {
			companionWrite(t, source, "frame.png", []byte("unselected"))
		}
		all, err := DiscoverSequence(source)
		if err != nil {
			t.Fatal(err)
		}
		var selected []Frame
		for _, frame := range all {
			if strings.HasSuffix(frame.Image, ".nef") {
				selected = append(selected, frame)
			}
		}
		plan, err := planSequenceMove(source, filepath.Join(t.TempDir(), "moved"), all, selected, false)
		if err != nil {
			t.Fatal(err)
		}
		calls := byte(0)
		err = executeSequenceMove(plan, sequenceMoveOps{move: func(from, to string) error {
			calls++
			if calls == failure%5 {
				return fmt.Errorf("injected move failure")
			}
			return renameDirectoryNoReplace(from, to)
		}})
		if err == nil {
			assertPreviewEVFileBytes(t, filepath.Join(plan.copy.destination, "frame.nef"), data)
		} else {
			assertPreviewEVFileBytes(t, filepath.Join(source, "frame.nef"), data)
		}
		if shared {
			assertPreviewEVFileBytes(t, filepath.Join(source, "frame.xmp"), []byte("opaque metadata"))
		}
	})
}
