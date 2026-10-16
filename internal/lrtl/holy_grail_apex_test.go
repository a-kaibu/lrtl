package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type measuredHolyGrailAPEXCase struct {
	Tags                           [][4]int32
	ShutterLabel, ApertureLabel    string
	ExpectedTime, ExpectedAperture float64
}

func measuredHolyGrailAPEXCases(t *testing.T) []measuredHolyGrailAPEXCase {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-apex.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []measuredHolyGrailAPEXCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 112 {
		t.Fatal("incomplete APEX fixture")
	}
	return fixture.Cases
}

func holyGrailAPEXJPEG(t *testing.T, tags [][4]int32, iso uint16) []byte {
	t.Helper()
	// Encode EXIF independently; reuse only the fixture's encoded gray pixels.
	source := holyGrailCameraJPEG(t)
	start := 26 + 2 + (len(tags)+1)*12 + 4
	tiff := make([]byte, start+len(tags)*8)
	copy(tiff, "II*")
	order := binary.LittleEndian
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	entry := func(offset int, id, kind uint16, value uint32) {
		order.PutUint16(tiff[offset:], id)
		order.PutUint16(tiff[offset+2:], kind)
		order.PutUint32(tiff[offset+4:], 1)
		order.PutUint32(tiff[offset+8:], value)
	}
	entry(10, 0x8769, 4, 26)
	order.PutUint16(tiff[26:], uint16(len(tags)+1))
	for i, tag := range tags {
		entry(28+i*12, uint16(tag[0]), uint16(tag[1]), uint32(start+i*8))
		order.PutUint32(tiff[start+i*8:], uint32(tag[2]))
		order.PutUint32(tiff[start+i*8+4:], uint32(tag[3]))
	}
	entry(28+len(tags)*12, 0x8827, 3, uint32(iso))
	payload := append([]byte{'E', 'x', 'i', 'f', 0, 0}, tiff...)
	data := []byte{255, 216, 255, 225, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	data = append(data, payload...)
	return append(data, source[96:]...)
}

func TestHolyGrailAPEXAgainstInstalledLRT755(t *testing.T) {
	for n, c := range measuredHolyGrailAPEXCases(t) {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			frame := Frame{Image: filepath.Join(t.TempDir(), "frame.jpg")}
			if err := os.WriteFile(frame.Image, holyGrailAPEXJPEG(t, c.Tags, 100), 0600); err != nil {
				t.Fatal(err)
			}
			metadata, err := readHolyGrailLRTExposureMetadata(frame)
			if err != nil {
				t.Fatal(err)
			}
			if holyGrailLRTShutterLabel(metadata.ExposureTime) != c.ShutterLabel || (c.ExpectedAperture > 0 && holyGrailLRTApertureLabel(metadata.FNumber) != c.ApertureLabel) {
				t.Fatalf("labels=%s f%s want %s f%s", holyGrailLRTShutterLabel(metadata.ExposureTime), holyGrailLRTApertureLabel(metadata.FNumber), c.ShutterLabel, c.ApertureLabel)
			}
			camera, err := readHolyGrailLRTCameraExposure(frame, false)
			if c.ExpectedTime <= 0 || c.ExpectedAperture <= 0 {
				if err == nil {
					t.Fatal("nonpositive shutter or missing aperture must be rejected")
				}
				return
			}
			if err != nil || math.Float64bits(camera.ExposureTime) != math.Float64bits(c.ExpectedTime) || math.Float64bits(camera.FNumber) != math.Float64bits(c.ExpectedAperture) {
				t.Fatalf("camera=%+v want time=%g f=%g err=%v", camera, c.ExpectedTime, c.ExpectedAperture, err)
			}
		})
	}
}

func TestHolyGrailAPEXControllerCLIThroughVideo(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-apex-controller.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []measuredHolyGrailControllerCase
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 {
		t.Fatal("incomplete APEX controller fixture")
	}
	for n, c := range fixture.Cases {
		if len(c.ShutterAPEX) != len(c.Input) || len(c.ApertureAPEX) != len(c.Input) {
			t.Fatal("missing APEX inputs")
		}
		t.Run(fmt.Sprint(n), func(t *testing.T) { runHolyGrailCLIAndVideo(t, c, false) })
	}
}

func TestHolyGrailInvalidAPEXBeforeWrites(t *testing.T) {
	for name, tags := range map[string][][4]int32{
		"shutter-limit":   {{0x9201, 10, 100, 1}, {0x9202, 5, 6, 1}},
		"aperture-absent": {{0x9201, 10, 7, 1}, {0x9202, 5, 0, 1}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			before := map[string][]byte{}
			for i := 0; i < 3; i++ {
				path := filepath.Join(dir, fmt.Sprintf("frame%d.jpg", i))
				fields := [][4]int32{{0x9201, 10, 7, 1}, {0x9202, 5, 6, 1}}
				if i == 2 {
					fields = tags
				}
				if err := os.WriteFile(path, holyGrailAPEXJPEG(t, fields, 100), 0600); err != nil {
					t.Fatal(err)
				}
				writeTestXMP(t, path, map[string]string{"lrt:ExternalExposureDefault": ".4", "xmp:Rating": "2", "crs:Exposure2012": "0"})
				for _, p := range []string{path, (Frame{Image: path}).OutputSidecar()} {
					data, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					before[p] = data
				}
			}
			var out, stderr bytes.Buffer
			if Main([]string{"holy-grail", dir, "--lrt-input", "--write"}, &out, &stderr) == 0 {
				t.Fatal("invalid late APEX frame accepted")
			}
			for path, original := range before {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatalf("changed %s: %v", path, err)
				}
			}
			backups, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
			if len(backups) > 0 {
				t.Fatal("preflight created backups")
			}
			if name == "aperture-absent" && Main([]string{"holy-grail", dir, "--lrt-input", "--ignore-aperture"}, &out, &stderr) != 0 {
				t.Fatalf("ignore aperture: %s", &stderr)
			}
		})
	}
}
