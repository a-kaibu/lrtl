package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type measuredHolyGrailControllerCase struct {
	Input, Expected, Smoothed     []float64
	Ratings, ISO                  []int
	Stretch, Rotate, Reduce       float64
	CameraShutter, CameraAperture []string
	ShutterAPEX, ApertureAPEX     [][2]int32
}

func measuredHolyGrailControllerCases(t *testing.T) []measuredHolyGrailControllerCase {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-controller.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []measuredHolyGrailControllerCase
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 66 {
		t.Fatal("incomplete controller fixture")
	}
	return fixture.Cases
}

func TestHolyGrailControllerAgainstInstalledLRT755(t *testing.T) {
	for n, c := range measuredHolyGrailControllerCases(t) {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			cameras := make([]CameraExposure, len(c.Input))
			for i := range cameras {
				cameras[i] = CameraExposure{ExposureTime: 1, ISO: float64(c.ISO[i]), FNumber: 8}
			}
			got, err := buildHolyGrailLRTCompensations(c.Input, c.Ratings, cameras, HolyGrailCurveOptions{LRTInput: true, Stretch: c.Stretch, Rotate: c.Rotate, ReduceDeflicker: c.Reduce}, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Input) == 20 && c.Reduce > 0 && c.Reduce < .5 {
				decimalOptions := HolyGrailCurveOptions{LRTInput: true, Stretch: math.Round(c.Stretch*100) / 100, Rotate: math.Round(c.Rotate*100) / 100, ReduceDeflicker: .35}
				decimalResult, err := buildHolyGrailLRTCompensations(c.Input, c.Ratings, cameras, decimalOptions, false)
				if err != nil {
					t.Fatal(err)
				}
				for i, want := range c.Expected {
					if math.Abs(decimalResult[i]-want) > 2e-8 {
						t.Fatalf("decimal controls at %d got %g want %g", i, decimalResult[i], want)
					}
				}
			}
			smoothed := lrtHolyGrailLowess(got, 1.5)
			for i, want := range c.Expected {
				if math.Abs(want) >= 1 {
					t.Fatal("fixture requires clipping-disabled equivalence")
				}
				if math.IsNaN(got[i]) || math.IsInf(got[i], 0) || math.Abs(got[i]-want) > 2e-8 {
					t.Fatalf("frame %d compensation got %.17g want %.17g", i, got[i], want)
				}
				if math.IsNaN(smoothed[i]) || math.IsInf(smoothed[i], 0) || math.Abs(smoothed[i]-c.Smoothed[i]) > 2e-8 {
					t.Fatalf("frame %d smoothed got %.17g want %.17g", i, smoothed[i], c.Smoothed[i])
				}
			}
		})
	}
}

func TestHolyGrailMeasuredControllerCLIThroughVideo(t *testing.T) {
	runHolyGrailCLIAndVideo(t, measuredHolyGrailControllerCases(t)[0], false)
}

func runHolyGrailCLIAndVideo(t *testing.T, c measuredHolyGrailControllerCase, auto bool) {
	runHolyGrailCLIAndVideoWithCachedMetadata(t, c, auto, false)
}

func runHolyGrailCLIAndVideoWithCachedMetadata(t *testing.T, c measuredHolyGrailControllerCase, auto, cached bool) {
	t.Helper()
	dir := t.TempDir()
	originals := map[string][]byte{}
	for i, value := range c.Input {
		extension := "jpg"
		if cached {
			extension = "png"
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.%s", i, extension))
		originals[path] = holyGrailCameraJPEGWithISO(t, uint16(c.ISO[i]))
		if len(c.CameraShutter) > 0 {
			denominator, err := strconv.ParseUint(strings.TrimPrefix(c.CameraShutter[i], "1/"), 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			fNumber, err := strconv.ParseFloat(c.CameraAperture[i], 64)
			if err != nil {
				t.Fatal(err)
			}
			originals[path] = holyGrailExifJPEG(t, measuredHolyGrailExifCase{TimeNumerator: 1, TimeDenominator: uint32(denominator), ApertureNumerator: uint32(math.Round(fNumber * 100)), ApertureDenominator: 100, ISO: uint16(c.ISO[i])})
		}
		if len(c.ShutterAPEX) > 0 {
			originals[path] = holyGrailAPEXJPEG(t, [][4]int32{{0x9201, 10, c.ShutterAPEX[i][0], c.ShutterAPEX[i][1]}, {0x9202, 5, c.ApertureAPEX[i][0], c.ApertureAPEX[i][1]}}, uint16(c.ISO[i]))
		}
		if cached {
			img, err := jpeg.Decode(bytes.NewReader(originals[path]))
			if err != nil {
				t.Fatal(err)
			}
			var data bytes.Buffer
			if err := png.Encode(&data, img); err != nil {
				t.Fatal(err)
			}
			originals[path] = data.Bytes()
		}
		if err := os.WriteFile(path, originals[path], 0600); err != nil {
			t.Fatal(err)
		}
		properties := map[string]string{"lrt:ExternalExposureDefault": strconv.FormatFloat(4*value, 'g', -1, 64), "xmp:Rating": fmt.Sprint(c.Ratings[i]), "crs:Exposure2012": "0", "crs:CameraProfile": "keep"}
		if cached {
			properties["lrt:ShutterSpeed"] = "1"
			properties["lrt:Iso"] = fmt.Sprint(c.ISO[i])
			properties["lrt:Aperture"] = "8"
			if len(c.CameraShutter) > 0 {
				properties["lrt:ShutterSpeed"] = c.CameraShutter[i]
				properties["lrt:Aperture"] = c.CameraAperture[i]
			}
		}
		writeTestXMP(t, path, properties)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		if _, err := readCameraExposure(frames[0], false); err == nil {
			t.Fatal("cached fixture unexpectedly has camera EXIF")
		}
	}
	before, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"holy-grail", dir, "--lrt-input", "--stretch", strconv.FormatFloat(c.Stretch, 'g', -1, 64), "--rotate", strconv.FormatFloat(c.Rotate, 'g', -1, 64), "--lrt-reduce-deflicker", strconv.FormatFloat(c.Reduce, 'g', -1, 64)}
	if auto {
		args = []string{"holy-grail", dir, "--lrt-input", "--auto-shape", "--lrt-reduce-deflicker", strconv.FormatFloat(c.Reduce, 'g', -1, 64)}
	}
	if code := Main(args, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview wrote XMP")
	}
	if code := Main(append(append([]string{}, args...), "--write"), &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		text, found, err := packet.GetScalar("crs:Exposure2012")
		if err != nil {
			t.Fatal(err)
		}
		value, valid := scalarNumber(text)
		if !found || !valid || math.Abs(value-c.Expected[i]) > 2e-8 {
			t.Fatalf("frame %d XMP=%s reference=%g", i, text, c.Expected[i])
		}
		rating, _ := packet.Rating()
		if rating != c.Ratings[i] {
			t.Fatal("changed source rating")
		}
		profile, _, _ := packet.GetScalar("crs:CameraProfile")
		if profile != "keep" {
			t.Fatal("changed unrelated metadata")
		}
		if cached {
			wanted := map[string]string{"lrt:ShutterSpeed": "1", "lrt:Iso": fmt.Sprint(c.ISO[i]), "lrt:Aperture": "8"}
			if len(c.CameraShutter) > 0 {
				wanted["lrt:ShutterSpeed"], wanted["lrt:Aperture"] = c.CameraShutter[i], c.CameraAperture[i]
			}
			for name, want := range wanted {
				text, found, err := packet.GetScalar(name)
				if err != nil || !found || text != want {
					t.Fatalf("cached %s changed: %q err=%v", name, text, err)
				}
			}
		}
	}
	backups, _ := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(before, backup) {
		t.Fatal("backup differs")
	}
	output := filepath.Join(t.TempDir(), "native-hg.avi")
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
			t.Fatal("truncated JPEG")
		}
		frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		if count >= len(c.Expected) {
			t.Fatal("extra frame")
		}
		want := 255 * (1.055*math.Pow(math.Exp2(c.Expected[count])*math.Pow((64.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
		r, _, _, _ := frame.At(32, 16).RGBA()
		if math.Abs(float64(r>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d reference-EV expected=%g", count, r>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != len(c.Expected) {
		t.Fatalf("frames=%d", count)
	}
	for path, before := range originals {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("source image modified")
		}
	}
}

func TestHolyGrailISOAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-iso.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input    float64
			Expected int
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 140 {
		t.Fatal("incomplete ISO parser fixture")
	}
	for _, c := range fixture.Cases {
		if got := holyGrailLRTISO(c.Input); got != float64(c.Expected) {
			t.Fatalf("ISO %g got %g want %d", c.Input, got, c.Expected)
		}
	}
	if holyGrailLRTISO(math.NaN()) != 0 || holyGrailLRTISO(math.Inf(1)) != 2147483647 || holyGrailLRTISO(math.Inf(-1)) != -2147483648 {
		t.Fatal("Java int boundary behavior differs")
	}
}
