package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestTransitionCloseIntervalsAreLinearAndEqualIntervalsFlat(t *testing.T) {
	for _, c := range []struct {
		name      string
		positions []int
		values    []float64
		property  string
		frame     int
		want      string
	}{
		{"close double", []int{0, 30, 32, 100}, []float64{0, 2, 4, 1}, "Exposure2012", 31, "3"},
		{"negative integer tie", []int{0, 30, 32, 100}, []float64{0, -5, -2, 1}, "Contrast2012", 31, "-4"},
		{"flat double precision", []int{0, 20, 70, 100}, []float64{0, 2.000001, 2.000002, 0}, "Exposure2012", 45, "2.000001"},
		{"flat integer", []int{0, 20, 70, 100}, []float64{0, 10, 10, 0}, "Sharpness", 45, "10"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 101)
			for i, pos := range c.positions {
				writeTestXMP(t, frames[pos].Image, map[string]string{"xmp:Rating": "4", "crs:" + c.property: formatNumber(c.values[i])})
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := buildPlanWithSmoothness(frames, 50)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, change := range plan.Changes {
				if change.Frame.Index == c.frame {
					found = true
					if got := change.Values[c.property]; got != c.want {
						t.Fatalf("frame %d %s=%s want %s", c.frame, c.property, got, c.want)
					}
				}
				for _, pos := range c.positions {
					if change.Frame.Index == pos {
						if _, changesAnchor := change.Values[c.property]; changesAnchor {
							t.Fatal("keyframe overwritten")
						}
					}
				}
			}
			if !found {
				t.Fatal("target frame missing")
			}
		})
	}
}

func TestLocalTransitionsUseSamePostprocessing(t *testing.T) {
	key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}
	for _, c := range []struct {
		positions []int
		values    []float64
		frame     int
		want      float64
	}{
		{[]int{0, 30, 32, 100}, []float64{0, .2, .4, .1}, 31, .3},
		{[]int{0, 20, 70, 100}, []float64{0, .200001, .200002, 0}, 45, .200001},
	} {
		indexes := make([]map[maskTransitionKey]maskTransitionNode, 101)
		for i, pos := range c.positions {
			indexes[pos] = map[maskTransitionKey]maskTransitionNode{key: {values: map[string]float64{"LocalExposure2012": c.values[i]}}}
		}
		series := maskTransitionSeries(c.positions, indexes, key, 101, 50)
		if got := series[c.frame]["LocalExposure2012"]; math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("local frame %d=%g want %g", c.frame, got, c.want)
		}
	}
}

func TestCloseTransitionThresholdIsStrict(t *testing.T) {
	values, filled, flags := make([]float64, 100), make([]bool, 100), make([]bool, 100)
	for i := range filled {
		filled[i] = true
		values[i] = 77
	}
	flags[0], flags[10], flags[14], flags[99] = true, true, true, true
	anchors := []transitionAnchor{{0, 0}, {10, 0}, {14, 4}, {99, 0}}
	postprocessLRTTransition(values, filled, flags, anchors, numericTransitionSpec{min: -100, max: 100})
	if values[12] != 77 {
		t.Fatal("gap equal to four-percent threshold was linearized")
	}
	if values[5] != 0 {
		t.Fatal("equal boundary interval not flattened")
	}
}

func TestSplineRejectsInvalidAnchors(t *testing.T) {
	for _, points := range [][]lrtSplinePoint{
		nil, {{0, 1}}, {{0, 1}, {0, 2}}, {{float32(math.NaN()), 1}, {2, 2}}, {{0, float32(math.Inf(1))}, {2, 2}},
		{{0, math.MaxFloat32}, {1, -math.MaxFloat32}, {2, -math.MaxFloat32}},
	} {
		if _, err := newLRTTransitionSpline(points, 50); err == nil {
			t.Fatalf("invalid anchors accepted: %v", points)
		}
	}
	if _, err := newLRTTransitionSpline([]lrtSplinePoint{{0, 1}, {1, 2}}, 101); err == nil {
		t.Fatal("invalid smoothness accepted")
	}
}

func TestSingleAnchorCopiesReferenceWithoutRegisteredBounds(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "99"})
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("changes=%d", len(plan.Changes))
	}
	for _, change := range plan.Changes {
		if change.Values["Exposure2012"] != "99" {
			t.Fatalf("single anchor did not copy its raw reference: %v", change.Values)
		}
	}
}

func writeEmbeddedTransitionJPEG(t *testing.T, path string, packet []byte) []byte {
	t.Helper()
	payload := append([]byte(jpegXMPHeader), packet...)
	data := []byte{255, 216, 255, 225, 0, 0}
	binary.BigEndian.PutUint16(data[4:], uint16(len(payload)+2))
	data = append(data, payload...)
	data = append(data, 255, 217)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInterpolateCLIReadsEmbeddedXMPAndPreservesImage(t *testing.T) {
	dir := t.TempDir()
	originals := make(map[string][]byte)
	for i := 0; i < 3; i++ {
		rating := ""
		if i != 1 {
			rating = `xmp:Rating="4"`
		}
		exposure := []int{1, 99, 3}[i]
		xmp := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:vendor="urn:lrtl:embedded-transition" %s crs:Exposure2012="%d"><vendor:Note>preserve embedded packet</vendor:Note></rdf:Description></rdf:RDF></x:xmpmeta>`, rating, exposure)
		path := filepath.Join(dir, fmt.Sprintf("frame%d.jpg", i))
		originals[path] = writeEmbeddedTransitionJPEG(t, path, []byte(xmp))
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir, "--smoothness", "100", "--write"}, &stdout, &stderr); code != 0 {
		t.Fatalf("CLI %d: %s", code, stderr.String())
	}
	path := filepath.Join(dir, "frame1.jpg.xmp")
	packet, err := PacketForSidecar(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := packet.GetScalar("crs:Exposure2012"); err != nil || !found || value != "2" {
		t.Fatalf("embedded transition=%s %v", value, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("preserve embedded packet")) {
		t.Fatal("embedded unknown metadata discarded")
	}
	for path, original := range originals {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("original JPEG modified")
		}
	}
}

func TestInterpolateRejectsFloat32OverflowWithoutWriting(t *testing.T) {
	// Equal or single references use a double-precision copy branch. Only
	// differing anchors that enter a numeric worker require float32 inputs.
	for _, anchors := range [][]int{{0, 4}, {0, 2, 4}} {
		t.Run(fmt.Sprint(anchors), func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 5)
			for _, index := range anchors {
				writeTestXMP(t, frames[index].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": fmt.Sprintf("%de300", index+1)})
			}
			var out, stderr bytes.Buffer
			if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code == 0 {
				t.Fatal("overflowing spline input accepted")
			}
			if _, err := os.Stat(frames[1].OutputSidecar()); !os.IsNotExist(err) {
				t.Fatal("output created after invalid input")
			}
		})
	}
	packet := exposureTestPacket(t, "modern")
	if err := setXMPExposureTargetValue(packet, xmpExposureTarget{"MaskGroupBasedCorrections", 1}, 1e300); err != nil {
		t.Fatal(err)
	}
	other := exposureTestPacket(t, "modern")
	if err := setXMPExposureTargetValue(other, xmpExposureTarget{"MaskGroupBasedCorrections", 1}, 2e300); err != nil {
		t.Fatal(err)
	}
	if _, err := planMaskTransitions([]*XMPPacket{packet, EmptyXMPPacket(), other}, []int{0, 2}, 50); err == nil {
		t.Fatal("mask float32 overflow ignored")
	}
}
