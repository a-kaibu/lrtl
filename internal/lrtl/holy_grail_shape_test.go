package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHolyGrailShapeAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-shape.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input, Expected []float64
			Ratings         []int
			Stretch, Rotate float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 112 {
		t.Fatal("incomplete shape component fixture")
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			got := lrtHolyGrailShapeCurve(c.Input, c.Ratings, c.Stretch, c.Rotate)
			if len(got) != len(c.Expected) {
				t.Fatal("wrong shape length")
			}
			for i, want := range c.Expected {
				if math.Abs(got[i]-want) > 1e-12 {
					t.Fatalf("frame %d got %.17g want %.17g", i, got[i], want)
				}
			}
		})
	}
}

func TestHolyGrailPairAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-pair.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Before, After [3]float64
			Expected      float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 27 {
		t.Fatal("incomplete camera-pair fixture")
	}
	for i, c := range fixture.Cases {
		before := CameraExposure{ExposureTime: c.Before[0], ISO: c.Before[1], FNumber: c.Before[2]}
		after := CameraExposure{ExposureTime: c.After[0], ISO: c.After[1], FNumber: c.After[2]}
		if got := holyGrailLRTPairDelta(before, after, false); math.Abs(got-c.Expected) > 1e-14 {
			t.Fatalf("case %d got %.17g want %.17g", i, got, c.Expected)
		}
		withoutAperture := before
		withoutAperture.FNumber = 0
		if holyGrailLRTPairDelta(before, after, true) != holyGrailLRTPairDelta(withoutAperture, after, false) {
			t.Fatal("ignore aperture changed other camera terms")
		}
	}
	camera := CameraExposure{ExposureTime: 1, FNumber: 8, ISO: 100}
	if got := holyGrailLRTPairDelta(camera, camera, false); got != 0 {
		t.Fatalf("unchanged settings add %g EV", got)
	}
}

// Minimal independently generated JPEG/EXIF fixture: 1 second, f/8, ISO 100.
func holyGrailCameraJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 64})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 84)
	copy(tiff, []byte{'I', 'I', 42, 0})
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], 1)
	entry := func(offset int, tag, kind uint16, value uint32) {
		binary.LittleEndian.PutUint16(tiff[offset:], tag)
		binary.LittleEndian.PutUint16(tiff[offset+2:], kind)
		binary.LittleEndian.PutUint32(tiff[offset+4:], 1)
		binary.LittleEndian.PutUint32(tiff[offset+8:], value)
	}
	entry(10, 0x8769, 4, 26)
	binary.LittleEndian.PutUint16(tiff[26:], 3)
	entry(28, 0x829a, 5, 68)
	entry(40, 0x829d, 5, 76)
	entry(52, 0x8827, 3, 100)
	binary.LittleEndian.PutUint32(tiff[68:], 1)
	binary.LittleEndian.PutUint32(tiff[72:], 1)
	binary.LittleEndian.PutUint32(tiff[76:], 8)
	binary.LittleEndian.PutUint32(tiff[80:], 1)
	payload := append([]byte{'E', 'x', 'i', 'f', 0, 0}, tiff...)
	length := len(payload) + 2
	result := append([]byte{255, 216, 255, 225, byte(length >> 8), byte(length)}, payload...)
	return append(result, encoded.Bytes()[2:]...)
}

func TestHolyGrailLRTShapeCLIThroughVideoAndReset(t *testing.T) {
	dir := t.TempDir()
	source := holyGrailCameraJPEG(t)
	input := []float64{0, .1, .6, .7, .8}
	ratings := []int{0, 2, 3, 0, 0}
	for i, value := range input {
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.jpg", i))
		if err := os.WriteFile(path, source, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"lrt:ExternalExposureDefault": fmt.Sprint(4 * value), "xmp:Rating": fmt.Sprint(ratings[i]), "crs:Exposure2012": "0", "crs:CameraProfile": "keep"})
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	camera, err := readCameraExposure(frames[0], false)
	if err != nil || camera.ExposureTime != 1 || camera.FNumber != 8 || camera.ISO != 100 {
		t.Fatalf("fixture camera=%+v err=%v", camera, err)
	}
	original, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"holy-grail", dir, "--lrt-input", "--stretch", ".9", "--lrt-reduce-deflicker", "0"}
	if code := Main(args, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preview wrote sidecar")
	}
	if code := Main(append(append([]string{}, args...), "--write"), &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	// Analytical combined-stage result: corrected input 0,.1,.2,.3,.4,
	// no camera-setting delta, stretch .9, then final centering.
	expected := []float64{-.2, -.1, 0, .1, .2}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertTransitionScalar(t, packet, "Exposure2012", expected[i])
		rating, _ := packet.Rating()
		if rating != ratings[i] {
			t.Fatal("HG changed source ratings")
		}
	}
	backups, _ := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup differs")
	}
	beforeRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if code := Main(append(append([]string{}, args...), "--write"), &out, &stderr); code != 0 {
		t.Fatalf("repeat: %s", &stderr)
	}
	afterRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(afterRepeat) != len(beforeRepeat) {
		t.Fatal("repeat rewrote unchanged correction")
	}
	output := filepath.Join(t.TempDir(), "hg.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-exposure", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 {
			t.Fatal("truncated frame")
		}
		frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		if count >= len(expected) {
			t.Fatal("extra frame")
		}
		want := 255 * (1.055*math.Pow(math.Exp2(expected[count])*math.Pow((64.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
		r, _, _, _ := frame.At(32, 16).RGBA()
		if math.Abs(float64(r>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d want %g", count, r>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != 5 {
		t.Fatalf("frames=%d", count)
	}
	if code := Main([]string{"holy-grail", dir, "--reset", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("reset: %s", &stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertTransitionScalar(t, packet, "Exposure2012", 0)
		if _, found, _ := packet.GetScalar(holyGrailCorrectionProperty); found {
			t.Fatal("reset retained correction")
		}
		imageData, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(imageData, source) {
			t.Fatal("source changed")
		}
	}
}

func TestHolyGrailShapeAdjacentMarkersAndExcludedFirstRangePoint(t *testing.T) {
	input := []float64{0, .1, .6, .7, .8}
	actual := lrtHolyGrailShapeCurve(input, []int{0, 2, 3, 0, 0}, .9, 0)
	// Corrected inputs are 0,.1,.2,.3,.4. Native normalization takes
	// min/max from frames 1..end, so frame zero can lie outside [-.5,+.5].
	want := []float64{-.75, -.45, -.15, .15, .45}
	for i, value := range actual {
		if math.Abs(value-want[i]) > 1e-12 {
			t.Fatalf("frame %d got %g want %g", i, value, want[i])
		}
	}
	for _, ratings := range [][]int{{0, 4, 4, 0, 0}, {0, 2, 0, 3, 0}, {2, 0, 0, 0, 3}} {
		if got := lrtHolyGrailShapeCurve(input, ratings, 1.5, 4); !reflect.DeepEqual(got, []float64{0, 0, 0, 0, 0}) {
			t.Fatalf("non-HG pair activated shape: %v -> %v", ratings, got)
		}
	}
}

func TestHolyGrailShapeDegeneratePolicyIsFinite(t *testing.T) {
	for _, input := range [][]float64{nil, {1}, {1, 1, 1}, {1, 2, 3}} {
		ratings := make([]int, len(input))
		for i := range ratings {
			ratings[i] = 2
		}
		for _, value := range lrtHolyGrailShapeCurve(input, ratings, 1, 4) {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatal("non-finite defensive shape")
			}
		}
	}
	if got := lrtHolyGrailShapeCurve([]float64{1, 2}, nil, 1, 1); !reflect.DeepEqual(got, []float64{0, 0}) {
		t.Fatal("mismatched ratings shape")
	}
}

func TestHolyGrailLRTCalculationUsesShapeMarkerGate(t *testing.T) {
	input := []float64{0, .1, .6, .7, .8}
	cameras := make([]CameraExposure, len(input))
	for i := range cameras {
		cameras[i] = CameraExposure{ExposureTime: 1, FNumber: 8, ISO: 100}
	}
	for _, ratings := range [][]int{{0, 4, 4, 0, 0}, {0, 2, 0, 3, 0}} {
		base, err := buildHolyGrailLRTCompensations(input, ratings, cameras, HolyGrailCurveOptions{LRTInput: true}, false)
		if err != nil {
			t.Fatal(err)
		}
		controlled, err := buildHolyGrailLRTCompensations(input, ratings, cameras, HolyGrailCurveOptions{LRTInput: true, Stretch: 1.5, Rotate: 4}, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(base, controlled) {
			t.Fatalf("shape changed compensation without adjacent HG markers: %v -> %v", base, controlled)
		}
	}
	ratings := []int{0, 2, 3, 0, 0}
	base, err := buildHolyGrailLRTCompensations(input, ratings, cameras, HolyGrailCurveOptions{LRTInput: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	controlled, err := buildHolyGrailLRTCompensations(input, ratings, cameras, HolyGrailCurveOptions{LRTInput: true, Stretch: .9}, false)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(base, controlled) {
		t.Fatal("adjacent HG markers did not activate shape")
	}
}

func TestHolyGrailZeroShapeDoesNotSmoothBaseCurve(t *testing.T) {
	// Analytical controller regression: fixed .5 LOWESS applies to the shape
	// array only. With no adjacent pair, that array is zero, even on a long
	// fluctuating sequence whose base would change under that smoother.
	input := make([]float64, 31)
	ratings := make([]int, 31)
	ratings[1] = 2
	cameras := make([]CameraExposure, 31)
	for i := range input {
		input[i] = []float64{-.3, .2, .1, -.1}[i%4]
		cameras[i] = CameraExposure{ExposureTime: 1, FNumber: 8, ISO: 100}
	}
	got, err := buildHolyGrailLRTCompensations(input, ratings, cameras, HolyGrailCurveOptions{LRTInput: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range got {
		want := -2 * (input[i] + .05)
		if math.Abs(value-want) > 1e-12 {
			t.Fatalf("frame %d base was smoothed: got %g want %g", i, value, want)
		}
	}
}
