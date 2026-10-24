package lrtl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func renameSequenceFixture(t *testing.T) (string, string) {
	t.Helper()
	parent := t.TempDir()
	source := filepath.Join(parent, "Sequence")
	for _, name := range []string{"frame.NEF", "frame.xmp", "frame.NEF.xmp", ".lrt/cache.bin", ".lrt/settings.json", "nested/snapshot.xml"} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("opaque "+name+"\x00"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(1700000000, 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return parent, source
}

func renameTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			result[name] = "directory"
			return nil
		}
		if !info.Mode().IsRegular() {
			result[name] = info.Mode().String()
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[name] = fmt.Sprintf("%s|%d|%s", info.Mode(), info.ModTime().UnixNano(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func requireRenamePlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("directory rename implementation is Windows/Linux")
	}
}

func TestRenameSequenceCLIWholeDirectoryAndNoop(t *testing.T) {
	requireRenamePlatform(t)
	_, source := renameSequenceFixture(t)
	before := renameTreeSnapshot(t, source)
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	// Windows loads the file ID lazily; capture it before the old path disappears.
	if !os.SameFile(original, original) {
		t.Fatal("could not capture directory identity")
	}
	var out, stderr bytes.Buffer
	args := []string{"rename-sequence", source, "--name", "Day 01_日本語"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("preview changed contents")
	}
	destination := filepath.Join(filepath.Dir(source), "Day 01_日本語")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created destination", err)
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatal("old source still exists", err)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, destination)) {
		t.Fatal("rename changed contents, timestamps or permissions")
	}
	now, err := os.Stat(destination)
	if err != nil || !os.SameFile(original, now) {
		t.Fatal("directory identity changed", err)
	}
	if Main([]string{"rename-sequence", destination, "--name", filepath.Base(destination), "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, destination)) {
		t.Fatal("no-op changed contents")
	}
}

func TestRenameSequenceNamesAndArguments(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", `..\escape`, `C:\escape`, "/escape", `\\host\share`, "new/dir", `new\dir`, "a:b", "a?b", "a*b", "a|b", "a<b", "a>b", "a\"b", "name.", "name ", " name", "a\x00b", "a\nb", "CON", "nul.txt", "COM1", "LPT9.log", "con .txt", "COM¹", "LPT².txt", strings.Repeat("a", 256), string([]byte{255})} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			if validateSequenceFolderName(name) == nil {
				t.Fatal("accepted invalid name", name)
			}
		})
	}
	for _, name := range []string{"Day 01_日本語", ".hidden", "COM10", "LPT0", "conifer", "folder.2", "emoji_🌅"} {
		if err := validateSequenceFolderName(name); err != nil {
			t.Fatal(name, err)
		}
	}
	_, source := renameSequenceFixture(t)
	before := renameTreeSnapshot(t, source)
	for _, args := range [][]string{{"rename-sequence"}, {"rename-sequence", source}, {"rename-sequence", source, "--name", "../escape", "--write"}, {"rename-sequence", source, "--name", "okay", "--unknown"}} {
		var out, stderr bytes.Buffer
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("accepted invalid arguments", args)
		}
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("invalid arguments changed files")
	}
}

func TestRenameSequenceCollisionsAndChangedSource(t *testing.T) {
	for _, kind := range []string{"empty-directory", "nonempty-directory", "file", "case-collision", "changed-source", "changed-parent"} {
		t.Run(kind, func(t *testing.T) {
			parent, source := renameSequenceFixture(t)
			plan, err := planSequenceRename(source, "NewName")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty-directory", "nonempty-directory", "case-collision":
				path := plan.destination
				if kind == "case-collision" {
					path = filepath.Join(parent, "newname")
				}
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "nonempty-directory" {
					if err := os.WriteFile(filepath.Join(path, "keep"), []byte("existing"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "file":
				if err := os.WriteFile(plan.destination, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-source":
				if err := os.Rename(source, filepath.Join(parent, "saved")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(source, 0755); err != nil {
					t.Fatal(err)
				}
			case "changed-parent":
				outer := t.TempDir()
				moved := filepath.Join(outer, "saved-parent")
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(source, 0755); err != nil {
					t.Fatal(err)
				}
			}
			before := renameTreeSnapshot(t, parent)
			called := false
			if err := executeSequenceRename(plan, func(string, string) error { called = true; return nil }); err == nil {
				t.Fatal("accepted invalid plan")
			}
			if called || !reflect.DeepEqual(before, renameTreeSnapshot(t, parent)) {
				t.Fatal("failed validation performed a move")
			}
		})
	}
}

func TestRenameSequenceNoReplaceAtMove(t *testing.T) {
	requireRenamePlatform(t)
	parent, source := renameSequenceFixture(t)
	before := renameTreeSnapshot(t, source)
	plan, err := planSequenceRename(source, "Destination")
	if err != nil {
		t.Fatal(err)
	}
	move := func(from, to string) error {
		if err := os.Mkdir(to, 0755); err != nil {
			return err
		}
		return renameDirectoryNoReplace(from, to)
	}
	if err := executeSequenceRename(plan, move); err == nil {
		t.Fatal("OS move replaced a destination that appeared after validation")
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
		t.Fatal("source changed on collision")
	}
	entries, err := os.ReadDir(filepath.Join(parent, "Destination"))
	if err != nil || len(entries) != 0 {
		t.Fatal("existing empty destination was replaced", entries, err)
	}
}

func TestRenameSequenceCaseOnlyRollbackAndRecovery(t *testing.T) {
	requireRenamePlatform(t)
	for _, kind := range []string{"success", "first-failure", "second-failure", "rollback-failure"} {
		t.Run(kind, func(t *testing.T) {
			parent, source := renameSequenceFixture(t)
			before := renameTreeSnapshot(t, source)
			plan, err := planSequenceRename(source, "sequence")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			move := func(from, to string) error {
				calls++
				if kind == "first-failure" && calls == 1 {
					return fmt.Errorf("injected staging failure")
				}
				if kind != "success" && calls == 2 {
					return fmt.Errorf("injected publication failure")
				}
				if kind == "rollback-failure" && calls == 3 {
					return fmt.Errorf("injected rollback failure")
				}
				return renameDirectoryNoReplace(from, to)
			}
			err = executeSequenceRename(plan, move)
			if kind == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, renameTreeSnapshot(t, plan.destination)) {
					t.Fatal("case change modified contents")
				}
				entries, _ := os.ReadDir(parent)
				if len(entries) != 1 || entries[0].Name() != "sequence" {
					t.Fatal("case change not applied", entries)
				}
			} else if kind == "rollback-failure" {
				if err == nil || !strings.Contains(err.Error(), "sequence retained at") {
					t.Fatal("missing recovery path", err)
				}
				staging, _ := filepath.Glob(filepath.Join(parent, ".lrtl-rename-*", "sequence"))
				if len(staging) != 1 {
					t.Fatal("missing retained data", staging)
				}
				if !reflect.DeepEqual(before, renameTreeSnapshot(t, staging[0])) {
					t.Fatal("recovery data changed")
				}
			} else {
				if err == nil {
					t.Fatal("missing injected failure")
				}
				if !reflect.DeepEqual(before, renameTreeSnapshot(t, source)) {
					t.Fatal("rollback did not restore original")
				}
				staging, _ := filepath.Glob(filepath.Join(parent, ".lrtl-rename-*"))
				if len(staging) != 0 {
					t.Fatal("empty staging directory left behind", staging)
				}
			}
		})
	}
}

func TestRenameSequenceSymlinkAndRootRejection(t *testing.T) {
	parent, source := renameSequenceFixture(t)
	link := filepath.Join(parent, "alias")
	if err := os.Symlink(source, link); err == nil {
		if _, err := planSequenceRename(link, "renamed"); err == nil {
			t.Fatal("accepted symlink source")
		}
	} else {
		t.Log("symlink creation unavailable:", err)
	}
	root := filepath.VolumeName(source) + string(filepath.Separator)
	if _, err := planSequenceRename(root, "renamed"); err == nil {
		t.Fatal("accepted filesystem root")
	}
}

func TestRenameSequenceLongPathAndReadonlyFile(t *testing.T) {
	requireRenamePlatform(t)
	parent := t.TempDir()
	for i := 0; i < 4; i++ {
		parent = filepath.Join(parent, strings.Repeat("segment", 9))
	}
	source := filepath.Join(parent, "Sequence")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(source, "locked.xmp")
	if err := os.WriteFile(file, []byte("<opaque metadata>"), 0400); err != nil {
		t.Fatal(err)
	}
	plan, err := planSequenceRename(source, "New_日本語")
	if err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceRename(plan, nil); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(plan.destination, "locked.xmp")
	defer os.Chmod(newFile, 0600)
	assertPreviewEVFileBytes(t, newFile, []byte("<opaque metadata>"))
	info, err := os.Stat(newFile)
	if err != nil || info.Mode().Perm()&0200 != 0 {
		t.Fatal("readonly flag changed", info, err)
	}
}

func TestRenameSequenceWindowsInputCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive input path")
	}
	_, source := renameSequenceFixture(t)
	plan, err := planSequenceRename(strings.ToUpper(source), "Sequence")
	if err != nil || !plan.noop {
		t.Fatal("actual spelling should be a no-op", plan, err)
	}
	plan, err = planSequenceRename(strings.ToUpper(source), "sequence")
	if err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceRename(plan, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(source))
	if err != nil || len(entries) != 1 || entries[0].Name() != "sequence" {
		t.Fatal("case rename through alias", entries, err)
	}
}

func TestRenameSequenceAVIAndSelectionPaths(t *testing.T) {
	requireRenamePlatform(t)
	dir, _ := frameSelectionFixture(t)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Rename(dir, source); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	render := func(directory string) []byte {
		path := filepath.Join(t.TempDir(), "video.avi")
		if Main([]string{"render", directory, "--output", path, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI payload")
		}
		return data[offset:]
	}
	before := render(source)
	selection := filepath.Join(t.TempDir(), "selection.json")
	if Main([]string{"select", source, "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"rename-sequence", source, "--name", "renamed", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	destination := filepath.Join(filepath.Dir(source), "renamed")
	if !bytes.Equal(before, render(destination)) {
		t.Fatal("directory rename changed rendered frames")
	}
	if Main([]string{"select", destination, "--selection", selection}, &out, &stderr) == 0 {
		t.Fatal("old absolute selection path should require regeneration")
	}
}

func FuzzSequenceRenameName(f *testing.F) {
	for _, name := range []string{"Day 01", "日本語", "../escape", `C:\escape`, "CON.txt", "sequence.", "emoji_🌅", "LPT².txt", ""} {
		f.Add(name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 2048 {
			return
		}
		if validateSequenceFolderName(name) != nil {
			return
		}
		parent := filepath.Join("synthetic", "parent")
		path := filepath.Join(parent, name)
		if filepath.Dir(path) != parent || filepath.Base(path) != name || filepath.IsAbs(name) {
			t.Fatal("accepted name escaped its parent", name)
		}
	})
}
