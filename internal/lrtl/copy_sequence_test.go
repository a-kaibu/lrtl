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

func TestCopySequenceSelectedBytesTimesAndVideo(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	before := batchFileSnapshot(t, dir)
	selection := filepath.Join(t.TempDir(), "selection.json")
	destination := filepath.Join(t.TempDir(), "copied")
	var out, stderr bytes.Buffer
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	args := []string{"copy-sequence", dir, "--output", destination, "--selection", selection}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("dry run created output")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	got, err := DiscoverSequence(destination)
	if err != nil || len(got) != 2 {
		t.Fatalf("copy result: %v %v", got, err)
	}
	for i, source := range frames[2:4] {
		for _, pair := range [][2]string{{source.Image, got[i].Image}, {source.Sidecar, got[i].Sidecar}} {
			if filepath.Base(pair[0]) != filepath.Base(pair[1]) {
				t.Fatal("copy changed active sidecar/image naming")
			}
			assertPreviewEVFileBytes(t, pair[1], before[pair[0]])
			a, errA := os.Stat(pair[0])
			b, errB := os.Stat(pair[1])
			if errA != nil || errB != nil || !a.ModTime().Equal(b.ModTime()) {
				t.Fatal("copy changed modification time")
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("copy changed source files/backups")
	}
	videos := []string{filepath.Join(t.TempDir(), "source.avi"), filepath.Join(t.TempDir(), "copy.avi")}
	if Main([]string{"render", dir, "--selection", selection, "--output", videos[0], "--apply-xmp-basic"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"render", destination, "--output", videos[1], "--apply-xmp-basic"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	a, errA := os.ReadFile(videos[0])
	b, errB := os.ReadFile(videos[1])
	i, j := bytes.Index(a, []byte("movi")), bytes.Index(b, []byte("movi"))
	if errA != nil || errB != nil || i < 0 || j < 0 || !bytes.Equal(a[i:], b[j:]) {
		t.Fatal("copied selection produced different AVI frames/index")
	}
	after := batchFileSnapshot(t, destination)
	if Main(append(args, "--write"), &out, &stderr) == 0 {
		t.Fatal("replaced existing copied directory")
	}
	if !reflect.DeepEqual(after, batchFileSnapshot(t, destination)) {
		t.Fatal("existing output changed")
	}
}

func TestCopySequenceSharedSidecarAndOpaqueBytes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"same.nef", "same.jpg", "same.xmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("opaque "+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copy")
	plan, err := planSequenceCopy(dir, destination, frames)
	if err != nil || len(plan.files) != 3 {
		t.Fatalf("shared sidecar: %v %v", plan, err)
	}
	if err := executeSequenceCopy(plan, nil); err != nil {
		t.Fatal(err)
	}
	for _, file := range plan.files {
		data, err := os.ReadFile(file.source)
		if err != nil {
			t.Fatal(err)
		}
		assertPreviewEVFileBytes(t, filepath.Join(destination, file.name), data)
	}
}

func TestCopySequenceFailuresLeaveNoPartialOutput(t *testing.T) {
	for _, mode := range []string{"copy-error", "source-change", "earlier-source-change", "publish-collision"} {
		t.Run(mode, func(t *testing.T) {
			dir, frames := frameSelectionFixture(t)
			parent := t.TempDir()
			destination := filepath.Join(parent, "copied")
			plan, err := planSequenceCopy(dir, destination, frames)
			if err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			calls := 0
			copyOne := func(source, dest string) error {
				calls++
				if mode == "copy-error" && calls == 3 {
					return fmt.Errorf("injected later copy failure")
				}
				if err := copyFile(source, dest); err != nil {
					return err
				}
				if mode == "source-change" && calls == 1 {
					if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
						return err
					}
				}
				if mode == "earlier-source-change" && calls == 3 {
					if err := os.WriteFile(plan.files[0].source, []byte("changed later"), 0600); err != nil {
						return err
					}
				}
				if mode == "publish-collision" && calls == 1 {
					if err := os.Mkdir(destination, 0700); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(destination, "keep"), []byte("existing"), 0600); err != nil {
						return err
					}
				}
				return nil
			}
			if err := executeSequenceCopy(plan, copyOne); err == nil {
				t.Fatal("failure injection succeeded")
			}
			if mode == "publish-collision" {
				assertPreviewEVFileBytes(t, filepath.Join(destination, "keep"), []byte("existing"))
			} else if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("partial output was published")
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".lrtl-copy-") {
					t.Fatal("staging files leaked")
				}
			}
			if mode != "source-change" && mode != "earlier-source-change" && !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("failed copy changed source")
			}
		})
	}
}

func TestCopySequenceCaseFoldedSharedXMP(t *testing.T) {
	if !samePath("A", "a") {
		t.Skip("case-insensitive path semantics required")
	}
	dir := t.TempDir()
	for _, name := range []string{"same.JPG", "SAME.NEF", "same.xmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), frames)
	if err != nil || len(plan.files) != 3 {
		t.Fatalf("case-folded shared XMP: %v %v", plan, err)
	}
	if err := executeSequenceCopy(plan, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCopySequencePathValidation(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	for _, output := range []string{"", dir, filepath.Join(dir, "child")} {
		if _, err := planSequenceCopy(dir, output, frames); err == nil {
			t.Fatalf("accepted output %q", output)
		}
	}
	if _, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), nil); err == nil {
		t.Fatal("accepted empty selection")
	}
	for _, frame := range frames[1:] {
		if err := os.WriteFile(frame.Sidecar, []byte("malformed XMP"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	destination := filepath.Join(t.TempDir(), "copy")
	if Main([]string{"copy-sequence", dir, "--output", destination, "--ratings", "4", "--write"}, &out, &stderr) == 0 {
		t.Fatal("rating filter ignored invalid metadata")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("failed metadata preflight created output")
	}
}

func TestCopySequenceReadOnlySource(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	source := frames[2].Image
	if err := os.Chmod(source, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0600) })
	destination := filepath.Join(t.TempDir(), "copy")
	plan, err := planSequenceCopy(dir, destination, frames[2:3])
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destination, filepath.Base(source))
	t.Cleanup(func() { _ = os.Chmod(copied, 0600) })
	if err := executeSequenceCopy(plan, nil); err != nil {
		t.Fatal(err)
	}
	a, errA := os.Stat(source)
	b, errB := os.Stat(copied)
	if errA != nil || errB != nil || a.Mode().Perm() != b.Mode().Perm() || !a.ModTime().Equal(b.ModTime()) {
		t.Fatal("read-only file copy did not preserve permissions/time")
	}
}

func TestCopySequenceSymlinkBoundary(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := planSequenceCopy(dir, filepath.Join(alias, "child"), frames); err == nil {
		t.Fatal("output alias bypassed source containment")
	}
	imageAlias := filepath.Join(dir, "linked.png")
	if err := os.Symlink(frames[0].Image, imageAlias); err != nil {
		t.Fatal(err)
	}
	if _, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), []Frame{{Image: imageAlias}}); err == nil {
		t.Fatal("copied a symlink as an ordinary image")
	}
}
