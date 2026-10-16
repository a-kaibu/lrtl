package lrtl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func batchInterpolationFixture(t *testing.T, root, name string, missing bool) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
		writeRenderFrame(t, path, "png", 64, 32)
		if missing && i == 2 {
			continue
		}
		rating, exposure := "1", "-1"
		if i == 0 {
			rating, exposure = "4", "0"
		}
		if i == 4 {
			rating, exposure = "4", "2"
		}
		writeTestXMP(t, path, map[string]string{"xmp:Rating": rating, "crs:Exposure2012": exposure, "crs:FutureProperty": "preserve"})
	}
	return dir
}

func batchFileSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestBatchInterpolationMatchesIndividualAndVideo(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b", "nested/c"} {
		batchInterpolationFixture(t, root, name, name == "b")
	}
	before := batchFileSnapshot(t, root)
	for path, data := range before {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(reference, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var serial, parallel, stderr bytes.Buffer
	if Main([]string{"batch-interpolate", root, "--recursive", "--workers", "1", "--smoothness", "73"}, &serial, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"batch-interpolate", root, "--recursive", "--workers", "4", "--smoothness", "73"}, &parallel, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if serial.String() != parallel.String() {
		t.Fatal("parallel analysis changed result order or totals")
	}
	if !strings.Contains(serial.String(), "3 sequence(s), 15 frame(s)") {
		t.Fatal(&serial)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("dry run changed files")
	}
	for _, name := range []string{"a", "b", "nested/c"} {
		var out bytes.Buffer
		if Main([]string{"interpolate", filepath.Join(reference, name), "--smoothness", "73", "--write", "--no-backup"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	var out bytes.Buffer
	if Main([]string{"batch-interpolate", root, "--recursive", "--workers", "4", "--smoothness", "73", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, name := range []string{"a", "b", "nested/c"} {
		frames, err := DiscoverSequence(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for i, frame := range frames {
			got, err := os.ReadFile(frame.OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(reference, name, filepath.Base(frame.OutputSidecar())))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("different individual result %s: %v", frame.OutputSidecar(), err)
			}
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			assertTransitionScalar(t, packet, "Exposure2012", float64(i)/2)
			image, err := os.ReadFile(frame.Image)
			if err != nil || !bytes.Equal(image, before[frame.Image]) {
				t.Fatal("source image changed")
			}
			backups, _ := filepath.Glob(frame.OutputSidecar() + "*.bak")
			old, exists := before[frame.OutputSidecar()]
			changed := !bytes.Equal(got, old)
			if exists && changed {
				if len(backups) != 1 {
					t.Fatalf("expected one backup: %s", frame.OutputSidecar())
				}
				data, err := os.ReadFile(backups[0])
				if err != nil || !bytes.Equal(data, old) {
					t.Fatal("backup differs")
				}
			} else if len(backups) != 0 {
				t.Fatal("backup created for new/unchanged sidecar")
			}
		}
	}
	after := batchFileSnapshot(t, root)
	out.Reset()
	if Main([]string{"batch-interpolate", root, "--recursive", "--smoothness", "73", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !strings.Contains(out.String(), "updated 0 sidecar(s)") || !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
		t.Fatal("repeat modified unchanged sidecars or backups")
	}
	actualVideo, referenceVideo := filepath.Join(t.TempDir(), "actual"), filepath.Join(t.TempDir(), "reference")
	for _, pair := range [][2]string{{root, actualVideo}, {reference, referenceVideo}} {
		out.Reset()
		if Main([]string{"batch-render", pair[0], "--output", pair[1], "--recursive", "--format", "avi", "--", "--apply-xmp-exposure", "--quality", "100"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	for _, name := range []string{"a.avi", "b.avi", "nested/c.avi"} {
		got, err := os.ReadFile(filepath.Join(actualVideo, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(referenceVideo, name))
		if err != nil {
			t.Fatal(err)
		}
		i, j := bytes.Index(got, []byte("movi")), bytes.Index(want, []byte("movi"))
		if i < 0 || j < 0 || !bytes.Equal(got[i:], want[j:]) {
			t.Fatalf("rendered frames/index differ for %s", name)
		}
	}
}

func TestBatchInterpolationPassesSplineSmoothness(t *testing.T) {
	results := map[int][]string{}
	for _, smoothness := range []int{0, 100} {
		root, reference := t.TempDir(), t.TempDir()
		for i := 0; i < 17; i++ {
			rating, value := "1", "0"
			switch i {
			case 0:
				rating, value = "4", "-1"
			case 8:
				rating, value = "4", "2"
			case 16:
				rating, value = "4", "-2"
			}
			for _, dir := range []string{root, reference} {
				path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
				writeRenderFrame(t, path, "png", 64, 32)
				writeTestXMP(t, path, map[string]string{"xmp:Rating": rating, "crs:Exposure2012": value})
			}
		}
		var out, stderr bytes.Buffer
		setting := fmt.Sprint(smoothness)
		if Main([]string{"batch-interpolate", root, "--smoothness", setting, "--write", "--no-backup"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if Main([]string{"interpolate", reference, "--smoothness", setting, "--write", "--no-backup"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		frames, err := DiscoverSequence(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			value, found, err := packet.GetScalar("crs:Exposure2012")
			if err != nil || !found {
				t.Fatal("missing exposure")
			}
			results[smoothness] = append(results[smoothness], value)
			expected, err := PacketForFrame(Frame{Image: filepath.Join(reference, filepath.Base(frame.Image))})
			if err != nil {
				t.Fatal(err)
			}
			want, found, err := expected.GetScalar("crs:Exposure2012")
			if err != nil || !found || value != want {
				t.Fatal("batch differs from individual smoothness result")
			}
		}
	}
	if reflect.DeepEqual(results[0], results[100]) {
		t.Fatal("smoothness controls did not change nonlinear transition")
	}
}

func TestBatchInterpolationPreflightsAllSequences(t *testing.T) {
	for _, invalid := range []string{"malformed-xmp", "invalid-number"} {
		t.Run(invalid, func(t *testing.T) {
			root := t.TempDir()
			batchInterpolationFixture(t, root, "a", true)
			bad := batchInterpolationFixture(t, root, "z", false)
			path := filepath.Join(bad, "frame4.png")
			if invalid == "malformed-xmp" {
				if err := os.WriteFile(path+".xmp", []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestXMP(t, path, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "NaN"})
			}
			before := batchFileSnapshot(t, root)
			var out, stderr bytes.Buffer
			if Main([]string{"batch-interpolate", root, "--workers", "4", "--write"}, &out, &stderr) == 0 {
				t.Fatal("invalid sequence accepted")
			}
			if !strings.Contains(stderr.String(), "no sidecars written") || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatalf("preflight modified files: %s", &stderr)
			}
		})
	}
}

func TestBatchInterpolationDiscoveryOptionsAndNoBackup(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".", "a", "group/b"} {
		batchInterpolationFixture(t, root, name, false)
	}
	for _, recursive := range []bool{false, true} {
		got, err := discoverBatchSequences(root, "", recursive)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{root, filepath.Join(root, "a")}
		if recursive {
			want = append(want, filepath.Join(root, "group/b"))
		}
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("recursive=%v got %v want %v", recursive, got, want)
		}
	}
	var out, stderr bytes.Buffer
	if Main([]string{"batch-interpolate", root, "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for path := range batchFileSnapshot(t, root) {
		if strings.HasSuffix(path, ".bak") {
			t.Fatal("no-backup created a backup")
		}
	}
	frames, err := DiscoverSequence(filepath.Join(root, "group/b"))
	if err != nil {
		t.Fatal(err)
	}
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	assertTransitionScalar(t, packet, "Exposure2012", -1)
	before := batchFileSnapshot(t, root)
	for _, args := range [][]string{{}, {root, "extra"}, {root, "--workers", "0"}, {root, "--workers", "65"}, {root, "--smoothness", "-1"}, {root, "--smoothness", "101"}, {root, "--unknown"}, {root, "--workers", "abc"}, {t.TempDir()}, {filepath.Join(root, "missing")}, {filepath.Join(root, "frame0.png")}} {
		out.Reset()
		stderr.Reset()
		if Main(append([]string{"batch-interpolate"}, args...), &out, &stderr) == 0 {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("invalid args modified files")
	}
	out.Reset()
	stderr.Reset()
	if Main([]string{"batch-interpolate", "--help"}, &out, &stderr) != 0 {
		t.Fatal("help failed")
	}
}
