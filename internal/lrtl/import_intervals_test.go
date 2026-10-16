package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestImportIntervalsAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-interval-split.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Intervals []int64
			Tolerance int64
			Minimum   int
			Groups    [][]int
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 240 {
		t.Fatal("incomplete native interval fixture")
	}
	for n, c := range fixture.Cases {
		groups := groupLRTImportIntervals(c.Intervals, c.Tolerance, c.Minimum)
		var got [][]int
		seen := make([]bool, len(c.Intervals))
		for _, group := range groups {
			got = append(got, group.indices)
			for _, i := range group.indices {
				if i < 0 || i >= len(seen) || seen[i] {
					t.Fatal("duplicated or invalid frame index")
				}
				seen[i] = true
			}
		}
		if !reflect.DeepEqual(got, c.Groups) {
			t.Fatalf("native case %d: %v want %v (intervals=%v tolerance=%d minimum=%d)", n, got, c.Groups, c.Intervals, c.Tolerance, c.Minimum)
		}
		for _, found := range seen {
			if !found {
				t.Fatal("frame disappeared")
			}
		}
	}
}

func TestImportIntervalsCLIThroughVideoAndRollback(t *testing.T) {
	// Outgoing intervals: 1,1,9,20,2,2,2,2,-1 seconds. The boundary
	// frame joins the first run; the isolated 20-second run is preserved.
	frames := splitImportFixture(t, []string{"2026-10-16T00:00:00Z", "2026-10-16T00:00:01Z", "2026-10-16T00:00:02Z", "2026-10-16T00:00:11Z", "2026-10-16T00:00:31Z", "2026-10-16T00:00:33Z", "2026-10-16T00:00:35Z", "2026-10-16T00:00:37Z", "2026-10-16T00:00:39Z"})
	source := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, source)
	output := filepath.Join(t.TempDir(), "imported")
	args := []string{"import", source, "--output", output, "--split-intervals", "--interval-tolerance", "0s", "--min-sequence-length", "3"}
	var out, stderr bytes.Buffer
	if Main(append(append([]string{}, args...), "--dry-run"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("dry run created output")
	}
	items, err := planImport(source, output, ImportOptions{SplitByInterval: true, MinimumSequenceLength: 3})
	if err != nil {
		t.Fatal(err)
	}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	groups := map[string]int{}
	for _, item := range items {
		groups[filepath.Dir(item.destination)]++
		for _, pair := range [][2]string{{item.source, filepath.Join(output, item.destination)}, {item.sidecar, filepath.Join(output, item.destination) + ".xmp"}} {
			got, err := os.ReadFile(pair[1])
			if err != nil || !bytes.Equal(got, before[pair[0]]) {
				t.Fatalf("copied data differs: %v %v", pair, err)
			}
		}
	}
	if !reflect.DeepEqual(groups, map[string]int{"sequence_001_20261016_000000": 3, "sequence_002_20261016_000031": 5, "single_images": 1}) {
		t.Fatalf("wrong interval folders: %v", groups)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, source)) {
		t.Fatal("import changed sources")
	}
	for folder := range groups {
		video := filepath.Join(t.TempDir(), "out.avi")
		if Main([]string{"render", filepath.Join(output, folder), "--output", video, "--apply-xmp-exposure"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	// A conflict is detected before any additional copying occurs.
	saved := batchFileSnapshot(t, output)
	if Main(args, &out, &stderr) == 0 || !reflect.DeepEqual(saved, batchFileSnapshot(t, output)) {
		t.Fatal("conflict changed destination")
	}
}

func TestImportIntervalsValidationAndCaptureOrder(t *testing.T) {
	frames := splitImportFixture(t, []string{"2026-10-16T00:00:01.001Z", "2026-10-16T09:00:00+09:00", "2026-10-16T00:00:00Z", "2026-10-16T00:00:02.002Z"})
	source := filepath.Dir(frames[0].Image)
	output := filepath.Join(t.TempDir(), "out")
	items, err := planImport(source, output, ImportOptions{SplitByInterval: true, IntervalTolerance: time.Millisecond, MinimumSequenceLength: 1, RenameByDate: true})
	if err != nil || len(items) != len(frames) {
		t.Fatalf("interval sort: %v", err)
	}
	if items[0].source != frames[1].Image || items[1].source != frames[2].Image || items[2].source != frames[0].Image {
		t.Fatal("capture order or stable duplicate ordering differs")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.destination] {
			t.Fatal("duplicate timestamp collision")
		}
		seen[item.destination] = true
	}
	before := batchFileSnapshot(t, source)
	for _, flags := range [][]string{{"--interval-tolerance", "1s"}, {"--min-sequence-length", "5"}, {"--split-intervals", "--split-gap", "1s"}, {"--split-intervals", "--sort", "name"}, {"--split-intervals", "--min-sequence-length", "0"}, {"--split-intervals", "--interval-tolerance", "-1ms"}, {"--split-intervals", "--interval-tolerance", "1ns"}, {"--split-intervals", "--interval-tolerance", "60000h"}} {
		var out, stderr bytes.Buffer
		if Main(append([]string{"import", source, "--output", output}, flags...), &out, &stderr) == 0 {
			t.Fatalf("invalid flags accepted: %v", flags)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid flags created output")
		}
	}
	writeTestXMP(t, frames[0].Image, map[string]string{"crs:Exposure2012": "0"})
	if _, err := planImport(source, output, ImportOptions{SplitByInterval: true}); err == nil || !strings.Contains(err.Error(), "capture time") {
		t.Fatal("missing capture time accepted")
	}
	// The earlier argument checks must not change the original data.
	for path, data := range before {
		if path == frames[0].OutputSidecar() {
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("validation changed source data")
		}
	}
}

func TestImportIntervalsRollback(t *testing.T) {
	for _, minimum := range []int{1, 5} {
		t.Run(fmt.Sprint(minimum), func(t *testing.T) {
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
			if err := ImportSequence(filepath.Dir(frames[0].Image), dest, ImportOptions{SplitByInterval: true, MinimumSequenceLength: minimum}, writer); err == nil {
				t.Fatal("vanished source was accepted")
			}
			files := batchFileSnapshot(t, dest)
			if len(files) != 1 || string(files[sentinel]) != "keep" {
				t.Fatal("rollback left files or changed existing content")
			}
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != 1 {
				t.Fatal("rollback left sequence directories")
			}
		})
	}
}

func FuzzImportIntervalGrouping(f *testing.F) {
	f.Add([]byte{1, 1, 9, 2, 2, 2, 2, 0}, uint16(0), uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, tolerance uint16, minimum uint8) {
		if len(data) > 2048 {
			return
		}
		intervals := make([]int64, len(data))
		for i, v := range data {
			intervals[i] = int64(v)*1000 - 1
		}
		groups := groupLRTImportIntervals(intervals, int64(tolerance), 1+int(minimum))
		seen := make([]bool, len(data))
		for _, g := range groups {
			for _, i := range g.indices {
				if i < 0 || i >= len(seen) || seen[i] {
					t.Fatal("invalid group index")
				}
				seen[i] = true
			}
		}
		for i, v := range seen {
			if !v {
				t.Fatal(fmt.Sprintf("lost frame %d", i))
			}
		}
	})
}
