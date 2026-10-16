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

func batchDeflickerFixture(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, frame := range feedbackFrames(t) {
		for _, path := range []string{frame.Image, frame.Sidecar} {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func copyBatchDeflickerTree(t *testing.T, source, target string) {
	t.Helper()
	for path, data := range batchFileSnapshot(t, source) {
		rel, err := filepath.Rel(source, path)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBatchDeflickerMatchesIndividualAndVideo(t *testing.T) {
	for _, smoother := range []string{"gaussian", "lowess"} {
		t.Run(smoother, func(t *testing.T) {
			root, reference := t.TempDir(), t.TempDir()
			names := []string{"a", "b", "nested/c"}
			for _, name := range names {
				batchDeflickerFixture(t, root, name)
			}
			var initOut, initErr bytes.Buffer
			if Main([]string{"init", filepath.Join(root, "b"), "--write", "--no-backup"}, &initOut, &initErr) != 0 {
				t.Fatal(&initErr)
			}
			before := batchFileSnapshot(t, root)
			copyBatchDeflickerTree(t, root, reference)
			options := []string{"--passes", "3", "--constant", "--apply-xmp-basic", "--smooth", "2", "--smoother", smoother, "--reference", "2,2,12,12"}
			var serial, parallel, stderr bytes.Buffer
			if Main(append([]string{"batch-deflicker", root, "--recursive", "--workers", "1"}, options...), &serial, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if Main(append([]string{"batch-deflicker", root, "--recursive", "--workers", "4"}, options...), &parallel, &stderr) != 0 || serial.String() != parallel.String() {
				t.Fatalf("parallel result differs: %s", &stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("dry run wrote files")
			}
			var stdout bytes.Buffer
			if Main(append(append([]string{"batch-deflicker", root, "--recursive", "--workers", "4"}, options...), "--write"), &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			changed := 0
			for _, name := range names {
				if Main(append(append([]string{"deflicker", filepath.Join(reference, name)}, options...), "--write"), &stdout, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, err := DiscoverSequence(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				for _, frame := range frames {
					path := frame.OutputSidecar()
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					want, err := os.ReadFile(filepath.Join(reference, name, filepath.Base(path)))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("batch differs from individual: %s %v", path, err)
					}
					data, err := os.ReadFile(frame.Image)
					if err != nil || !bytes.Equal(data, before[frame.Image]) {
						t.Fatal("source image changed")
					}
					backups, err := filepath.Glob(path + "*.bak")
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, before[path]) {
						changed++
						if len(backups) != 1 {
							t.Fatal("expected one backup of original XMP")
						}
						old, err := os.ReadFile(backups[0])
						if err != nil || !bytes.Equal(old, before[path]) {
							t.Fatal("backup differs from original")
						}
					}
				}
				video, individual := filepath.Join(t.TempDir(), "batch.avi"), filepath.Join(t.TempDir(), "individual.avi")
				for _, pair := range [][2]string{{filepath.Join(root, name), video}, {filepath.Join(reference, name), individual}} {
					if Main([]string{"render", pair[0], "--output", pair[1], "--apply-xmp-basic", "--quality", "100"}, &stdout, &stderr) != 0 {
						t.Fatal(&stderr)
					}
				}
				got, err := os.ReadFile(video)
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(individual)
				if err != nil {
					t.Fatal(err)
				}
				// The AVI library writes the current time in its header comment.
				// Compare the main header plus the complete frames and index.
				i, j := bytes.Index(got, []byte("movi")), bytes.Index(want, []byte("movi"))
				h, k := bytes.Index(got, []byte("avih")), bytes.Index(want, []byte("avih"))
				if i < 0 || j < 0 || h < 0 || k < 0 || h+64 > len(got) || k+64 > len(want) || !bytes.Equal(got[i:], want[j:]) || !bytes.Equal(got[h:h+64], want[k:k+64]) {
					t.Fatal("batch correction video differs from individual")
				}
			}
			if changed == 0 {
				t.Fatal("fixture produced no corrections")
			}
			// Reset through the same API, without image decoding or analysis flags.
			if Main([]string{"batch-deflicker", root, "--recursive", "--reset", "--write", "--no-backup"}, &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, name := range names {
				if Main([]string{"deflicker", filepath.Join(reference, name), "--reset", "--write", "--no-backup"}, &stdout, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, err := DiscoverSequence(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				for _, frame := range frames {
					got, err := os.ReadFile(frame.OutputSidecar())
					if err != nil {
						t.Fatal(err)
					}
					want, err := os.ReadFile(filepath.Join(reference, name, filepath.Base(frame.OutputSidecar())))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatal("batch reset differs from individual")
					}
				}
			}
		})
	}
}

func TestBatchDeflickerValidationPreventsAllWrites(t *testing.T) {
	for _, kind := range []string{"broken-image", "broken-xmp", "tracking", "reference"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			batchDeflickerFixture(t, root, "a")
			dir := batchDeflickerFixture(t, root, "z")
			var err error
			args := []string{"batch-deflicker", root, "--workers", "4", "--write", "--constant"}
			switch kind {
			case "broken-image":
				err = os.WriteFile(filepath.Join(dir, "frame1.png"), []byte("broken"), 0600)
			case "broken-xmp":
				err = os.WriteFile(filepath.Join(dir, "frame1.png.xmp"), []byte("<broken>"), 0600)
			case "tracking":
				writeTestXMP(t, filepath.Join(dir, "frame1.png"), map[string]string{deflickerCorrectionProperty: "invalid"})
			case "reference":
				args = append(args, "--reference", "0,0,100,100")
			}
			if err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, root)
			var out, stderr bytes.Buffer
			if Main(args, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "no sidecars written") {
				t.Fatalf("validation succeeded: %s", &stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("validation failure wrote sidecars or backups")
			}
		})
	}
}

func TestBatchDeflickerOptionsAndHelp(t *testing.T) {
	root := t.TempDir()
	batchDeflickerFixture(t, root, "a")
	before := batchFileSnapshot(t, root)
	for _, args := range [][]string{
		nil, {root, "extra"}, {root, "--workers", "0"}, {root, "--workers", "65"},
		{root, "--smooth", "0"}, {root, "--passes", "9"}, {root, "--smoother", "invalid"},
		{root, "--reset", "--passes", "1"}, {root, "--reset", "--apply-xmp-basic=false"},
		{root, "--reference", "bad"}, {root, "--raw-preview", "--raw-develop"},
		{root, "--apply-xmp-basic", "--apply-xmp-exposure"}, {root, "--unknown"},
	} {
		var out, stderr bytes.Buffer
		if Main(append([]string{"batch-deflicker"}, args...), &out, &stderr) == 0 {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("invalid flags modified files")
	}
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", "--help"}, &out, &stderr) != 0 || !strings.Contains(stderr.String(), "--") && !strings.Contains(stderr.String(), "workers") {
		t.Fatalf("help failed: %s", &stderr)
	}
	if Main([]string{"batch-deflicker", t.TempDir()}, &out, &stderr) == 0 {
		t.Fatal("empty root accepted")
	}
	// Without recursion only immediate sequence directories are selected.
	nested := batchDeflickerFixture(t, root, "nested/c")
	out.Reset()
	if Main([]string{"batch-deflicker", root}, &out, &stderr) != 0 || strings.Contains(out.String(), nested) {
		t.Fatalf("nonrecursive selection failed: %s", &stderr)
	}
}

func TestBatchDeflickerResetWithoutDecodingAndNoBackup(t *testing.T) {
	root := t.TempDir()
	dir := batchDeflickerFixture(t, root, "a")
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", root, "--constant", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		if err := os.WriteFile(frame.Image, []byte("not an image anymore"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, root)
	if Main([]string{"batch-deflicker", root, "--reset"}, &out, &stderr) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatalf("reset preview failed or wrote files: %s", &stderr)
	}
	if Main([]string{"batch-deflicker", root, "--reset", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, err := packet.GetScalar(deflickerCorrectionProperty); err != nil || found {
			t.Fatalf("reset retained tracking: %v", err)
		}
		assertTransitionScalar(t, packet, "Exposure2012", 0)
	}
	for path := range batchFileSnapshot(t, root) {
		if strings.HasSuffix(path, ".bak") {
			t.Fatal("--no-backup created a backup")
		}
	}
}

func TestBatchDeflickerRAWMatchesIndividual(t *testing.T) {
	executable, _, raw := fakeDNGConverter(t)
	root, reference := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for i, exposure := range []string{"-1", "0", "1"} {
			path := filepath.Join(dir, fmt.Sprintf("frame%d.nef", i))
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			writeTestXMP(t, path, map[string]string{"crs:Exposure2012": exposure})
		}
	}
	copyBatchDeflickerTree(t, root, reference)
	options := []string{"--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure", "--constant", "--passes", "2", "--write", "--no-backup"}
	var out, stderr bytes.Buffer
	if Main(append([]string{"batch-deflicker", root, "--workers", "2"}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, name := range []string{"a", "b"} {
		if Main(append([]string{"deflicker", filepath.Join(reference, name)}, options...), &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	for path, got := range batchFileSnapshot(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(reference, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("RAW batch differs from individual: %s %v", rel, err)
		}
	}
}
