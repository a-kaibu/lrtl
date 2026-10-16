package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeDeflickerXMPMatchesWorkerOracles(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-native-deflicker-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input, Previous, Result []float64
		Smoothing               float64
		Constant                bool
		Accuracy, Pass          int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"modern", "legacy", "global"} {
		for c, tc := range cases {
			frames, packets := make([]Frame, len(tc.Input)), make([]*XMPPacket, len(tc.Input))
			for i := range frames {
				frames[i] = Frame{Image: fmt.Sprintf("frame%d.png", i), Index: i}
				packet := exposureTestPacket(t, kind)
				target := findLRTExposureTarget(packet, "Deflicker", 6)
				if kind == "global" {
					if err := packet.SetScalar("crs:Exposure2012", formatNumber(1.25+tc.Previous[i])); err != nil {
						t.Fatal(err)
					}
					if err := packet.SetScalar(deflickerCorrectionProperty, formatNumber(tc.Previous[i])); err != nil {
						t.Fatal(err)
					}
					if err := packet.SetScalar(deflickerTargetProperty, "global"); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := setXMPExposureTargetValue(packet, target, tc.Previous[i]); err != nil {
						t.Fatal(err)
					}
				}
				packets[i] = packet
			}
			changes, err := buildNativeDeflickerChanges(frames, tc.Input, packets, nativeDeflickerOptions{true, tc.Smoothing, tc.Accuracy}, tc.Constant, tc.Pass)
			if err != nil {
				t.Fatal(err)
			}
			if err := applyDeflickerChanges(changes); err != nil {
				t.Fatal(err)
			}
			for i, packet := range packets {
				target := findLRTExposureTarget(packet, "Deflicker", 6)
				got, _, err := getXMPExposureTargetValue(packet, target)
				want := tc.Result[i]
				if kind == "global" {
					want += 1.25
				}
				if err != nil || math.Abs(got-want) > 2e-8 {
					t.Fatalf("%s case %d frame %d: %g != native %g (%v)", kind, c, i, got, want, err)
				}
				serialized, err := packet.Serialize()
				if err != nil || !bytes.Contains(serialized, []byte("preserve me")) {
					t.Fatal("native plan lost unknown metadata")
				}
			}
		}
	}
}

func TestNativeDeflickerDateReference(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	frames, refs, order := dateReferenceFixture(t, root, reference)
	var out, stderr bytes.Buffer
	options := []string{"--calculation", "lrt-7.5.5", "--passes", "2", "--write"}
	if Main(append([]string{"deflicker", root, "--lrt-reference"}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main(append([]string{"deflicker", reference}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for rank, original := range order {
		want, err := os.ReadFile(refs[rank].Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		assertPreviewEVFileBytes(t, frames[original].Sidecar, want)
	}
}

func TestNativeDeflickerFailurePreflight(t *testing.T) {
	root := t.TempDir()
	nativeCLISequence(t, filepath.Join(root, "a"))
	bad := filepath.Join(root, "z")
	frames := nativeCLISequence(t, bad)
	if err := os.WriteFile(frames[2].Sidecar, []byte("<broken>"), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", root, "--calculation", "lrt-7.5.5", "--workers", "2", "--write"}, &out, &stderr) == 0 {
		t.Fatal("invalid native batch accepted")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("native batch failure saved partial XMP/settings/backups")
	}
	valid := filepath.Join(root, "a")
	for _, command := range []string{"deflicker", "batch-deflicker"} {
		dir := valid
		if command == "batch-deflicker" {
			dir = root
		}
		for _, options := range [][]string{
			{"--calculation", "unknown"}, {"--lrt-smoothing", ".5"}, {"--ignore-blacks", "10"},
			{"--calculation", "lrt-7.5.5", "--lrt-smoothing", "NaN"},
			{"--calculation", "lrt-7.5.5", "--lrt-accuracy", "2"},
			{"--calculation", "lrt-7.5.5", "--ignore-whites", "101"},
			{"--calculation", "lrt-7.5.5", "--smooth", "5"},
			{"--calculation", "lrt-7.5.5", "--smoother", "lowess"},
			{"--calculation", "lrt-7.5.5", "--reset"},
		} {
			if Main(append([]string{command, dir, "--write"}, options...), &out, &stderr) == 0 {
				t.Fatalf("accepted invalid native flags %v", options)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("invalid native arguments wrote files")
			}
		}
	}
}

func TestNativeDeflickerRAWDevelopment(t *testing.T) {
	executable, _, raw := fakeDNGConverter(t)
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		path := filepath.Join(root, fmt.Sprintf("frame%d.nef", i))
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"crs:Exposure2012": fmt.Sprint(i % 2)})
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"deflicker", root, "--calculation", "lrt-7.5.5", "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure", "--passes", "2"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !strings.Contains(out.String(), "passes: 2") || !strings.Contains(out.String(), "calculation: lrt-7.5.5") {
		t.Fatalf("RAW native mode/pass summary incorrect: %s", &out)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("native RAW dry run modified source")
	}
}

func TestNativeDeflickerMigrationAmountAndFeedbackBaseline(t *testing.T) {
	frames := []Frame{{Image: "a.png"}, {Image: "b.png"}}
	packets := []*XMPPacket{exposureTestPacket(t, "modern"), exposureTestPacket(t, "modern")}
	for _, packet := range packets {
		if err := packet.SetScalar("crs:Exposure2012", "-0.3"); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar(deflickerCorrectionProperty, "-0.7"); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := buildNativeDeflickerChanges(frames, []float64{.5, .5}, packets, nativeDeflickerOptions{true, .5, 0}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDeflickerChanges(changes); err != nil {
		t.Fatal(err)
	}
	for _, packet := range packets {
		assertExposureValue(t, packet, globalXMPExposureTarget(), .4)
		assertExposureValue(t, packet, findLRTExposureTarget(packet, "Deflicker", 6), 0)
	}
	for _, amount := range []float64{.5, 0} {
		packets = []*XMPPacket{exposureTestPacket(t, "modern"), exposureTestPacket(t, "modern")}
		for _, packet := range packets {
			element, err := xmpExposureTargetElement(packet, findLRTExposureTarget(packet, "Deflicker", 6))
			if err != nil {
				t.Fatal(err)
			}
			setCRSElementValue(element, "CorrectionAmount", formatNumber(amount))
		}
		changes, err := buildNativeDeflickerChanges(frames, []float64{.5, .5}, packets, nativeDeflickerOptions{true, .5, 0}, false, 0)
		if amount == 0 {
			if err == nil {
				t.Fatal("native plan accepted zero-amount target")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := applyDeflickerChanges(changes); err != nil {
			t.Fatal(err)
		}
		for _, packet := range packets {
			assertExposureValue(t, packet, findLRTExposureTarget(packet, "Deflicker", 6), 0)
		}
	}
	actual := feedbackFrames(t)
	for _, frame := range actual {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"crs:Exposure2012": "0.7", deflickerCorrectionProperty: "0.3", deflickerTargetProperty: "global"} {
			if err := packet.SetScalar(key, value); err != nil {
				t.Fatal(err)
			}
		}
		persistExposurePacket(t, frame, packet)
	}
	plan, err := planDeflickerSequence(actual, image.Rectangle{}, imageInputOptions{NativeDeflicker: nativeDeflickerOptions{true, .5, 0}}, 5, 2, false, "gaussian")
	if err != nil || len(plan.Measurements) != 2 {
		t.Fatalf("native feedback baseline: %v", err)
	}
	if plan.Measurements[0][0] != plan.Measurements[1][0] {
		t.Fatal("native feedback applied existing correction a second time")
	}
	if plan.Measurements[0][1] == plan.Measurements[1][1] {
		t.Fatal("native feedback failed to apply the new residual")
	}
}

func nativeCLISequence(t *testing.T, dir string) []Frame {
	t.Helper()
	frames := movingReferenceFixture(t, dir, true)
	for _, frame := range frames {
		persistExposurePacket(t, frame, exposureTestPacket(t, "modern"))
	}
	return frames
}

func TestNativeDeflickerCLIAndFeedback(t *testing.T) {
	root := t.TempDir()
	frames := nativeCLISequence(t, root)
	before := batchFileSnapshot(t, root)
	measurements, err := analyzePreviewEVSequence(frames, imageInputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current := make([]float64, len(frames))
	for i := range current {
		current[i] = .2
	}
	want, err := calculateNativeDeflickerPass(measurements, current, .5, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	renderPixels := func() []byte {
		t.Helper()
		path := filepath.Join(t.TempDir(), "native.avi")
		if Main([]string{"render", root, "--output", path, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var pixels bytes.Buffer
		count := 0
		for {
			start := bytes.Index(data, []byte{0xff, 0xd8})
			if start < 0 {
				break
			}
			data = data[start:]
			end := bytes.Index(data, []byte{0xff, 0xd9})
			if end < 0 {
				t.Fatal("truncated AVI JPEG")
			}
			img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
			if err != nil {
				t.Fatal(err)
			}
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					pixels.Write([]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8)})
				}
			}
			count++
			data = data[end+2:]
		}
		if count != len(frames) {
			t.Fatalf("rendered %d frames, want %d", count, len(frames))
		}
		return pixels.Bytes()
	}
	baselinePixels := renderPixels()
	args := []string{"deflicker", root, "--calculation", "lrt-7.5.5"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("native dry run wrote files")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		target := findLRTExposureTarget(packet, "Deflicker", 6)
		value, _, err := getXMPExposureTargetValue(packet, target)
		if err != nil || math.Abs(value-want.Correction[i]) > 1e-10 {
			t.Fatal("native CLI saved wrong absolute slot value")
		}
		assertExposureValue(t, packet, globalXMPExposureTarget(), .4)
		assertExposureValue(t, packet, findHolyGrailXMPTarget(packet), .2)
		assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
	}
	if bytes.Equal(baselinePixels, renderPixels()) {
		t.Fatal("native XMP correction did not change rendered AVI pixels")
	}
	if Main([]string{"deflicker", root, "--reset", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertExposureValue(t, packet, findLRTExposureTarget(packet, "Deflicker", 6), .2)
	}
	if !bytes.Equal(baselinePixels, renderPixels()) {
		t.Fatal("reset did not restore original AVI pixels")
	}
	clean := nativeCLISequence(t, t.TempDir())
	options := imageInputOptions{NativeDeflicker: nativeDeflickerOptions{true, .5, 1}}
	plan, err := planDeflickerSequence(clean, image.Rectangle{}, options, 5, 3, false, "gaussian")
	if err != nil || len(plan.Measurements) < 2 {
		t.Fatalf("native pixel feedback: %v", err)
	}
	if reflect.DeepEqual(plan.Measurements[0], plan.Measurements[1]) {
		t.Fatal("native subsequent pass reused uncorrected measurements")
	}
	if len(plan.Changes) == 0 {
		t.Fatal("native multipass produced no final corrections")
	}
}

func TestNativeDeflickerBatchMatchesIndividual(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b"} {
		nativeCLISequence(t, filepath.Join(root, name))
	}
	before := batchFileSnapshot(t, root)
	copyBatchDeflickerTree(t, root, reference)
	options := []string{"--calculation", "lrt-7.5.5", "--lrt-smoothing", "0.6", "--lrt-accuracy", "1", "--passes", "3", "--constant", "--ignore-blacks", "10", "--ignore-whites", "15", "--hard-cut", "--write"}
	var out, stderr bytes.Buffer
	if Main(append([]string{"batch-deflicker", root, "--workers", "2"}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, name := range []string{"a", "b"} {
		if Main(append([]string{"deflicker", filepath.Join(reference, name)}, options...), &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		frames, err := DiscoverSequence(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			want, err := os.ReadFile(filepath.Join(reference, name, filepath.Base(frame.Sidecar)))
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewEVFileBytes(t, frame.Sidecar, want)
			assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
			backups, err := filepath.Glob(frame.Sidecar + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal("native batch should save one original backup")
			}
			assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
		}
	}
}
