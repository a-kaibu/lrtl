package lrtl

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func batchHolyGrailFixture(t *testing.T, root, name string, cached bool) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	c := measuredHolyGrailControllerCases(t)[0]
	for i, value := range c.Input {
		ext := ".jpg"
		if cached {
			ext = ".png"
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d%s", i, ext))
		if cached {
			writeRenderFrame(t, path, "png", 16, 16)
		} else if err := os.WriteFile(path, holyGrailCameraJPEGWithISO(t, uint16(c.ISO[i])), 0600); err != nil {
			t.Fatal(err)
		}
		rating := c.Ratings[i]
		if !cached {
			rating = 0
			if i == 0 || i == len(c.Input)-1 {
				rating = 4
			}
		}
		properties := map[string]string{
			"lrt:ExternalExposureDefault": fmt.Sprint(4 * value), "xmp:Rating": fmt.Sprint(rating),
			"crs:Exposure2012": ".25", "crs:CameraProfile": "preserve",
			"lrt:ShutterSpeed": "1", "lrt:Iso": fmt.Sprint(c.ISO[i]), "lrt:Aperture": "8",
		}
		if !cached && rating == 0 {
			delete(properties, "xmp:Rating") // HG respects every explicitly stored rating.
		}
		writeTestXMP(t, path, properties)
	}
	return dir
}

func assertBatchHolyGrailVideos(t *testing.T, batch, individual string) {
	t.Helper()
	var videos [][]byte
	for _, dir := range []string{batch, individual} {
		path := filepath.Join(t.TempDir(), "video.avi")
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", path, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		videos = append(videos, data)
	}
	i, j := bytes.Index(videos[0], []byte("movi")), bytes.Index(videos[1], []byte("movi"))
	h, k := bytes.Index(videos[0], []byte("avih")), bytes.Index(videos[1], []byte("avih"))
	// The AVI library's timestamp comment is excluded; main header, complete
	// encoded frames and frame index must match.
	if i < 0 || j < 0 || h < 0 || k < 0 || h+64 > len(videos[0]) || k+64 > len(videos[1]) || !bytes.Equal(videos[0][i:], videos[1][j:]) || !bytes.Equal(videos[0][h:h+64], videos[1][k:k+64]) {
		t.Fatal("batch Holy Grail video differs from individual")
	}
}

func TestBatchHolyGrailMatchesIndividualAndMeasuredLRT(t *testing.T) {
	c := measuredHolyGrailControllerCases(t)[0]
	for _, mode := range []string{"exif", "exif-auto", "lrt", "auto"} {
		t.Run(mode, func(t *testing.T) {
			root, reference := t.TempDir(), t.TempDir()
			names := []string{"a", "b", "nested/c"}
			var out, stderr bytes.Buffer
			for _, name := range names {
				dir := batchHolyGrailFixture(t, root, name, !strings.HasPrefix(mode, "exif"))
				if name == "b" && Main([]string{"init", dir, "--write", "--no-backup"}, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			before := batchFileSnapshot(t, root)
			copyBatchDeflickerTree(t, root, reference)
			options := []string{"--mark-jumps"}
			if mode == "exif-auto" {
				options = []string{"--auto-shape", "--no-mark-jumps", "--ignore-aperture"}
			} else if mode == "lrt" {
				options = []string{"--lrt-input", "--stretch", fmt.Sprint(c.Stretch), "--rotate", fmt.Sprint(c.Rotate), "--lrt-reduce-deflicker", fmt.Sprint(c.Reduce)}
			} else if mode == "auto" {
				options = []string{"--lrt-input", "--auto-shape", "--lrt-reduce-deflicker", ".35"}
			}
			var serial, parallel bytes.Buffer
			if Main(append([]string{"batch-holy-grail", root, "--recursive", "--workers", "1"}, options...), &serial, &stderr) != 0 || Main(append([]string{"batch-holy-grail", root, "--recursive", "--workers", "4"}, options...), &parallel, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if serial.String() != parallel.String() || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("parallel preview differs or wrote files")
			}
			if Main(append(append([]string{"batch-holy-grail", root, "--recursive", "--workers", "4"}, options...), "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			changed := 0
			for _, name := range names {
				if Main(append(append([]string{"holy-grail", filepath.Join(reference, name)}, options...), "--write"), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, err := DiscoverSequence(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				for i, frame := range frames {
					path := frame.OutputSidecar()
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					want, err := os.ReadFile(filepath.Join(reference, name, filepath.Base(path)))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("batch/individual XMP differs: %s %v", path, err)
					}
					image, err := os.ReadFile(frame.Image)
					if err != nil || !bytes.Equal(image, before[frame.Image]) {
						t.Fatal("source image changed")
					}
					if !bytes.Equal(got, before[path]) {
						changed++
						backups, err := filepath.Glob(path + "*.bak")
						if err != nil || len(backups) != 1 {
							t.Fatal("missing original backup")
						}
						old, err := os.ReadFile(backups[0])
						if err != nil || !bytes.Equal(old, before[path]) {
							t.Fatal("backup differs from original XMP")
						}
					}
					if mode == "lrt" {
						packet, err := PacketForFrame(frame)
						if err != nil {
							t.Fatal(err)
						}
						text, found, err := packet.GetScalar(holyGrailCorrectionProperty)
						value, valid := scalarNumber(text)
						if err != nil || !found || !valid || math.Abs(value-c.Expected[i]) > 2e-8 {
							t.Fatalf("batch native LRT fixture frame %d correction %g want %g (%v)", i, value, c.Expected[i], err)
						}
					}
					if mode == "exif" && (i == 1 || i == 2) {
						packet, err := PacketForFrame(frame)
						if err != nil {
							t.Fatal(err)
						}
						rating, found := packet.Rating()
						if !found || rating != i+1 {
							t.Fatalf("jump marker %d: rating=%d found=%t", i, rating, found)
						}
					}
				}
				assertBatchHolyGrailVideos(t, filepath.Join(root, name), filepath.Join(reference, name))
			}
			if changed == 0 {
				t.Fatal("fixture had no HG changes")
			}
			// Repeating the calculation must not accumulate the compensation.
			saved := batchFileSnapshot(t, root)
			if Main(append(append([]string{"batch-holy-grail", root, "--recursive"}, options...), "--write", "--no-backup"), &out, &stderr) != 0 || !reflect.DeepEqual(saved, batchFileSnapshot(t, root)) {
				t.Fatalf("HG repeat accumulated changes: %s", &stderr)
			}
			if Main([]string{"batch-holy-grail", root, "--recursive", "--reset", "--write", "--no-backup"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, name := range names {
				if Main([]string{"holy-grail", filepath.Join(reference, name), "--reset", "--write", "--no-backup"}, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, err := DiscoverSequence(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				for _, frame := range frames {
					packet, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					original, err := parseXMP(before[frame.OutputSidecar()])
					if err != nil {
						t.Fatal(err)
					}
					oldRating, oldFound := original.Rating()
					rating, found := packet.Rating()
					if found != oldFound || rating != oldRating {
						t.Fatalf("reset did not restore original rating: %d vs %d", rating, oldRating)
					}
					assertTransitionScalar(t, packet, "Exposure2012", .25)
				}
			}
			for path, got := range batchFileSnapshot(t, root) {
				if strings.HasSuffix(path, ".bak") {
					if !bytes.Equal(got, saved[path]) {
						t.Fatal("reset changed an existing backup")
					}
					continue // Individual backups have different creation timestamps.
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(filepath.Join(reference, rel))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("reset differs: %s %v", rel, err)
				}
			}
		})
	}
}

func TestBatchHolyGrailValidationPreventsAllWrites(t *testing.T) {
	for _, kind := range []string{"camera", "xmp", "tracking", "lrt-input", "rating-conflict"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			batchHolyGrailFixture(t, root, "a", false)
			dir := batchHolyGrailFixture(t, root, "z", false)
			path := filepath.Join(dir, "frame02.jpg")
			var err error
			args := []string{"batch-holy-grail", root, "--workers", "4", "--write", "--mark-jumps"}
			switch kind {
			case "camera":
				err = os.WriteFile(path, []byte("invalid JPEG"), 0600)
			case "xmp":
				err = os.WriteFile(path+".xmp", []byte("<broken>"), 0600)
			case "tracking":
				writeTestXMP(t, path, map[string]string{holyGrailCorrectionProperty: "invalid"})
			case "rating-conflict":
				writeTestXMP(t, path, map[string]string{"xmp:Rating": "5"})
			case "lrt-input":
				writeTestXMP(t, path, map[string]string{"lrt:ExternalExposureDefault": "invalid"})
				args = []string{"batch-holy-grail", root, "--workers", "4", "--write", "--lrt-input"}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, root)
			var out, stderr bytes.Buffer
			if Main(args, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "no sidecars written") || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatalf("failed validation modified files: %s", &stderr)
			}
		})
	}
}

func TestBatchHolyGrailOptionsAndResetWithoutEXIF(t *testing.T) {
	root := t.TempDir()
	dir := batchHolyGrailFixture(t, root, "a", false)
	before := batchFileSnapshot(t, root)
	for _, args := range [][]string{nil, {root, "extra"}, {root, "--workers", "0"}, {root, "--workers", "65"}, {root, "--mark-jumps", "--no-mark-jumps"}, {root, "--lrt-input", "--mark-jumps"}, {root, "--lrt-reduce-deflicker", "0"}, {root, "--lrt-input", "--lrt-reduce-deflicker", "NaN"}, {root, "--stretch", "2"}, {root, "--rotate", "Inf"}, {root, "--auto-shape", "--stretch", "0"}, {root, "--reset", "--lrt-input"}, {root, "--unknown"}} {
		var out, stderr bytes.Buffer
		if Main(append([]string{"batch-holy-grail"}, args...), &out, &stderr) == 0 {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("invalid flags wrote files")
	}
	var out, stderr bytes.Buffer
	if Main([]string{"batch-holy-grail", "--help"}, &out, &stderr) != 0 || !strings.Contains(stderr.String(), "workers") {
		t.Fatal("missing help")
	}
	if Main([]string{"batch-holy-grail", t.TempDir()}, &out, &stderr) == 0 {
		t.Fatal("empty root accepted")
	}
	nested := batchHolyGrailFixture(t, root, "nested/c", false)
	out.Reset()
	if Main([]string{"batch-holy-grail", root}, &out, &stderr) != 0 || strings.Contains(out.String(), nested) {
		t.Fatal("nonrecursive discovery failed")
	}
	if Main([]string{"batch-holy-grail", dir, "--mark-jumps", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		if err := os.WriteFile(frame.Image, []byte("no EXIF"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	saved := batchFileSnapshot(t, dir)
	if Main([]string{"batch-holy-grail", dir, "--reset"}, &out, &stderr) != 0 || !reflect.DeepEqual(saved, batchFileSnapshot(t, dir)) {
		t.Fatal("reset preview failed or changed files")
	}
	if Main([]string{"batch-holy-grail", dir, "--reset", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, err := packet.GetScalar(holyGrailCorrectionProperty); err != nil || found {
			t.Fatal("reset retained correction")
		}
		assertTransitionScalar(t, packet, "Exposure2012", .25)
	}
	for path := range batchFileSnapshot(t, dir) {
		if strings.HasSuffix(path, ".bak") {
			t.Fatal("--no-backup created backups")
		}
	}
}
