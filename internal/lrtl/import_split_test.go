package lrtl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func splitImportFixture(t *testing.T, stamps []string) []Frame {
	t.Helper()
	frames := cropFixture(t, len(stamps))
	for i, frame := range frames {
		properties := map[string]string{"crs:Exposure2012": "0", "crs:CameraProfile": "keep"}
		if stamps[i] != "" {
			properties["exif:DateTimeOriginal"] = stamps[i]
		}
		writeTestXMP(t, frame.Image, properties)
	}
	return frames
}

func TestImportSplitCaptureOrderBoundariesAndDuplicateTimes(t *testing.T) {
	frames := splitImportFixture(t, []string{"2026-10-16T00:00:32Z", "2026-10-16T00:00:00Z", "2026-10-16T00:00:10Z", "2026-10-16T00:00:21Z", "2026-10-16T00:00:32Z"})
	items, err := planImport(filepath.Dir(frames[0].Image), filepath.Join(t.TempDir(), "output"), ImportOptions{SplitGap: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []int{1, 2, 3, 0, 4}
	wantGroups := []string{"sequence_001_20261016_000000", "sequence_001_20261016_000000", "sequence_002_20261016_000021", "sequence_003_20261016_000032", "sequence_003_20261016_000032"}
	for i, item := range items {
		if item.source != frames[wantOrder[i]].Image || filepath.Dir(item.destination) != wantGroups[i] {
			t.Fatalf("item %d=%+v", i, item)
		}
	}
}

func TestImportSplitSubsecondOffsetsAndDateRenaming(t *testing.T) {
	frames := splitImportFixture(t, []string{"2026-10-16T09:00:00.002+09:00", "2026-10-16T00:00:00.001Z", "2026-10-16T00:00:00.0031Z", "2026-10-16T00:00:00.0031Z"})
	items, err := planImport(filepath.Dir(frames[0].Image), filepath.Join(t.TempDir(), "output"), ImportOptions{SplitGap: time.Millisecond, RenameByDate: true})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].source != frames[1].Image || items[1].source != frames[0].Image || filepath.Dir(items[0].destination) != filepath.Dir(items[1].destination) || filepath.Dir(items[1].destination) == filepath.Dir(items[2].destination) {
		t.Fatalf("wrong offset/subsecond grouping: %+v", items)
	}
	if items[2].destination == items[3].destination {
		t.Fatal("duplicate timestamp filename collision")
	}
}

func TestImportSplitCLIThroughSequencesAndVideo(t *testing.T) {
	frames := splitImportFixture(t, []string{"2026-10-16T00:00:00Z", "2026-10-16T00:00:10Z", "2026-10-16T00:01:00Z"})
	source := filepath.Dir(frames[0].Image)
	destination := filepath.Join(t.TempDir(), "output")
	var out, stderr bytes.Buffer
	args := []string{"import", source, "--output", destination, "--split-gap", "10s"}
	if code := Main(append(append([]string{}, args...), "--dry-run"), &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("dry-run created output")
	}
	if !strings.Contains(out.String(), "sequence_002_20261016_000100") {
		t.Fatalf("preview hid split: %s", &out)
	}
	items, err := planImport(source, destination, ImportOptions{SplitGap: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if code := Main(args, &out, &stderr); code != 0 {
		t.Fatalf("import: %s", &stderr)
	}
	for _, item := range items {
		for _, pair := range [][2]string{{item.source, filepath.Join(destination, item.destination)}, {item.sidecar, filepath.Join(destination, item.destination) + ".xmp"}} {
			original, err := os.ReadFile(pair[0])
			if err != nil {
				t.Fatal(err)
			}
			copied, err := os.ReadFile(pair[1])
			if err != nil || !bytes.Equal(copied, original) {
				t.Fatalf("copy differs: %v err=%v", pair, err)
			}
		}
	}
	for _, tc := range []struct {
		folder string
		count  int
	}{{"sequence_001_20261016_000000", 2}, {"sequence_002_20261016_000100", 1}} {
		dir := filepath.Join(destination, tc.folder)
		sequence, err := DiscoverSequence(dir)
		if err != nil || len(sequence) != tc.count {
			t.Fatalf("sequence=%d err=%v", len(sequence), err)
		}
		video := filepath.Join(t.TempDir(), tc.folder+".avi")
		if code := Main([]string{"render", dir, "--output", video}, &out, &stderr); code != 0 {
			t.Fatalf("render: %s", &stderr)
		}
		data, err := os.ReadFile(video)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 12 || string(data[:4]) != "RIFF" || bytes.Count(data, []byte{255, 216}) != tc.count {
			t.Fatal("wrong AVI frame count")
		}
	}
	if code := Main(args, &out, &stderr); code == 0 {
		t.Fatal("repeat overwrote existing sequence")
	}
}

func TestImportSplitPreflightNoPartialWrites(t *testing.T) {
	for _, scenario := range []string{"missing-time", "collision", "parent-file"} {
		t.Run(scenario, func(t *testing.T) {
			stamps := []string{"2026-10-16T00:00:00Z", "2026-10-16T00:00:20Z"}
			if scenario == "missing-time" {
				stamps[1] = ""
			}
			frames := splitImportFixture(t, stamps)
			dest := filepath.Join(t.TempDir(), "output")
			if scenario != "missing-time" {
				items, err := planImport(filepath.Dir(frames[0].Image), dest, ImportOptions{SplitGap: 10 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dest, items[1].destination)
				if scenario == "parent-file" {
					path = filepath.Dir(path)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := ImportSequence(filepath.Dir(frames[0].Image), dest, ImportOptions{SplitGap: 10 * time.Second}, &out); err == nil {
				t.Fatal("accepted invalid import")
			}
			first := filepath.Join(dest, "sequence_001_20261016_000000")
			if _, err := os.Stat(first); !os.IsNotExist(err) {
				t.Fatal("failed preflight created first sequence")
			}
		})
	}
}

type splitImportFaultWriter struct {
	once   sync.Once
	action func()
}

func (w *splitImportFaultWriter) Write(p []byte) (int, error) {
	w.once.Do(w.action)
	return len(p), nil
}

func TestImportSplitRollbackRemovesOnlyCreatedFilesAndFolders(t *testing.T) {
	frames := splitImportFixture(t, []string{"2026-10-16T00:00:00Z", "2026-10-16T00:00:20Z"})
	dest := t.TempDir()
	sentinel := filepath.Join(dest, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	writer := &splitImportFaultWriter{action: func() {
		if err := os.Remove(frames[1].Image); err != nil {
			t.Fatal(err)
		}
	}}
	if err := ImportSequence(filepath.Dir(frames[0].Image), dest, ImportOptions{SplitGap: 10 * time.Second}, writer); err == nil {
		t.Fatal("accepted vanished source")
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("rollback output=%v err=%v", entries, err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "keep" {
		t.Fatal("rollback changed existing data")
	}
}

func TestImportSplitValidationAndRecursiveDNGPlan(t *testing.T) {
	for _, args := range [][]string{{"--split-gap", "0s"}, {"--split-gap", "-1s"}, {"--split-gap", "invalid"}, {"--split-gap", "1s", "--sort", "name"}} {
		var out, stderr bytes.Buffer
		if code := Main(append([]string{"import", "missing", "--output", filepath.Join(t.TempDir(), "output")}, args...), &out, &stderr); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for i, name := range []string{"a/frame.nef", "b/frame.nef", "b/skip.jpg"} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("RAW"), 0600); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-10-16T00:00:%02dZ", i*20)})
		}
	}
	items, err := planImport(source, filepath.Join(root, "output"), ImportOptions{Recursive: true, SkipJPEG: true, ConvertToDNG: true, SplitGap: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range items {
		if !item.convertDNG || item.sidecar == "" {
			t.Fatal("lost DNG/XMP plan")
		}
		names = append(names, filepath.Base(item.destination))
	}
	if !reflect.DeepEqual(names, []string{"000001_a__frame.dng", "000002_b__frame.dng"}) {
		t.Fatalf("names=%v", names)
	}
}

func TestImportSplitDisabledKeepsFlatNamesWithoutCaptureMetadata(t *testing.T) {
	frames := splitImportFixture(t, []string{"", ""})
	items, err := planImport(filepath.Dir(frames[0].Image), filepath.Join(t.TempDir(), "output"), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if item.destination != filepath.Base(frames[i].Image) {
			t.Fatalf("default import changed: %+v", item)
		}
	}
}
