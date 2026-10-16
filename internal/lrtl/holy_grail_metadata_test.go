package lrtl

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHolyGrailMetadataParsersAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		Input    string
		Expected float64
	}
	var fixture struct {
		Namespace        string
		Fields           []string
		Number, Aperture []item
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Namespace != "http://lrtimelapse.com/" || !reflect.DeepEqual(fixture.Fields, []string{"ShutterSpeed", "Iso", "Aperture"}) || len(fixture.Number) != 137 || len(fixture.Aperture) != 45 {
		t.Fatal("unexpected registered metadata/parser fixture")
	}
	for _, c := range fixture.Number {
		got, err := parseHolyGrailCameraNumber(c.Input)
		if err != nil || math.Float64bits(got) != math.Float64bits(c.Expected) {
			t.Fatalf("number %s got %.17g want %.17g err=%v", c.Input, got, c.Expected, err)
		}
	}
	for _, c := range fixture.Aperture {
		got, err := parseHolyGrailAperture(c.Input)
		if c.Input == "F8" || c.Input == "f/8" {
			if err == nil {
				t.Fatal("lrtl must reject malformed cached aperture rather than native zero fallback")
			}
			continue
		}
		if err != nil || math.Float64bits(got) != math.Float64bits(c.Expected) {
			t.Fatalf("aperture %s got %.17g want %.17g err=%v", c.Input, got, c.Expected, err)
		}
	}
}

func TestHolyGrailCachedFramePairAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-metadata-pair.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Before, After [3]string
			Expected      float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 36 {
		t.Fatal("incomplete frame-pair fixture")
	}
	frames := cropFixture(t, 2) // PNGs have no camera EXIF: all settings come from XMP.
	for n, c := range fixture.Cases {
		var cameras [2]CameraExposure
		for i, settings := range [][3]string{c.Before, c.After} {
			writeTestXMP(t, frames[i].Image, map[string]string{"lrt:ShutterSpeed": settings[0], "lrt:Iso": settings[1], "lrt:Aperture": settings[2]})
			cameras[i], err = readHolyGrailLRTCameraExposure(frames[i], false)
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := holyGrailLRTFramePairDelta(cameras[0], cameras[1], false); math.IsNaN(got) || math.Abs(got-c.Expected) > 1e-14 {
			t.Fatalf("case %d got %.17g want %.17g", n, got, c.Expected)
		}
	}
}

func TestHolyGrailCachedMetadataFallbackAndIgnoreAperture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(path, holyGrailCameraJPEG(t), 0600); err != nil {
		t.Fatal(err)
	}
	frame := Frame{Image: path}
	writeTestXMP(t, path, map[string]string{"lrt:ShutterSpeed": "1/125", "lrt:Aperture": "5.6"})
	got, err := readHolyGrailLRTCameraExposure(frame, false)
	if err != nil || got.ExposureTime != 1.0/128 || got.ISO != 100 || got.lrtISOParsed || got.FNumber != lrtHolyGrailApertureLabels["5.6"] {
		t.Fatalf("partial cache=%+v err=%v", got, err)
	}
	physical, err := readCameraExposure(frame, false)
	if err != nil || physical.ExposureTime != 1 || physical.ISO != 100 || physical.FNumber != 8 {
		t.Fatalf("EXIF reader=%+v err=%v", physical, err)
	}
	pngFrame := cropFixture(t, 1)[0]
	writeTestXMP(t, pngFrame.Image, map[string]string{"lrt:ShutterSpeed": "1", "lrt:Iso": "100.0", "lrt:Aperture": "bad"})
	got, err = readHolyGrailLRTCameraExposure(pngFrame, true)
	if err != nil || got.ISO != 100 || !got.lrtISOParsed {
		t.Fatalf("ignored aperture=%+v err=%v", got, err)
	}
	if _, err := readHolyGrailLRTCameraExposure(pngFrame, false); err == nil {
		t.Fatal("accepted invalid cached aperture")
	}
}

func TestHolyGrailCachedMetadataCLIThroughVideo(t *testing.T) {
	runHolyGrailCLIAndVideoWithCachedMetadata(t, measuredHolyGrailControllerCases(t)[0], false, true)
}

func TestHolyGrailCachedMetadataInvalidBeforeWrites(t *testing.T) {
	for _, tc := range []struct{ property, bad string }{{"lrt:ShutterSpeed", "0"}, {"lrt:ShutterSpeed", "1/0"}, {"lrt:ShutterSpeed", "NaN"}, {"lrt:ShutterSpeed", "invalid"}, {"lrt:Iso", "NaN"}, {"lrt:Iso", "invalid"}, {"lrt:Aperture", "bad"}, {"lrt:Aperture", "NaN"}, {"lrt:Aperture", "-1"}} {
		frames := cropFixture(t, 3)
		for i, frame := range frames {
			properties := map[string]string{"lrt:ShutterSpeed": "1", "lrt:Iso": "100", "lrt:Aperture": "8", "lrt:ExternalExposureDefault": "0", "xmp:Rating": "2"}
			if i == 2 {
				properties[tc.property] = tc.bad
			}
			writeTestXMP(t, frame.Image, properties)
		}
		original, err := os.ReadFile(frames[0].OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := Main([]string{"holy-grail", filepath.Dir(frames[0].Image), "--lrt-input", "--write"}, &out, &stderr); code == 0 {
			t.Fatalf("accepted %s=%q", tc.property, tc.bad)
		}
		after, err := os.ReadFile(frames[0].OutputSidecar())
		if err != nil || !bytes.Equal(after, original) {
			t.Fatal("invalid metadata wrote XMP")
		}
		backups, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
		if len(backups) != 0 {
			t.Fatal("invalid metadata created backups")
		}
	}
}
