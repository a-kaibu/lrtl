package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNumericMissingAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-numeric-missing.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Excluded []string
		Cases    []struct {
			Input, Expected []*float64
			Changed         bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 9 {
		t.Fatalf("cases=%d", len(fixture.Cases))
	}
	numericExcluded := map[string]bool{}
	for _, name := range fixture.Excluded {
		if _, registered := globalTransitionSpecs[name]; registered {
			numericExcluded[name] = true
		}
	}
	if !reflect.DeepEqual(numericExcluded, numericPreflightExclusions) {
		t.Fatalf("excluded numeric fields=%v", numericExcluded)
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			packets := make([]*XMPPacket, len(c.Input))
			originals := make([][]byte, len(packets))
			for i, value := range c.Input {
				packets[i] = EmptyXMPPacket()
				if value != nil {
					if err := packets[i].SetScalar("crs:Exposure2012", fmt.Sprint(*value)); err != nil {
						t.Fatal(err)
					}
				}
				originals[i], err = packets[i].Serialize()
				if err != nil {
					t.Fatal(err)
				}
			}
			plan, err := prepareNumericTransitionInputs(packets)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			for i, want := range c.Expected {
				got, found := plan.Values[i]["Exposure2012"]
				if (want != nil) != found || (want != nil && got != *want) {
					t.Fatalf("frame %d got %.15g found=%t want %v", i, got, found, want)
				}
				_, missing := plan.Missing[i]["Exposure2012"]
				if missing != (c.Input[i] == nil && want != nil) {
					t.Fatalf("incorrect missing flag at %d", i)
				}
				changed = changed || missing
				after, err := packets[i].Serialize()
				if err != nil || !bytes.Equal(after, originals[i]) {
					t.Fatal("preflight mutated its packet")
				}
			}
			if changed != c.Changed {
				t.Fatalf("changed=%t want %t", changed, c.Changed)
			}
		})
	}
}

func TestNumericMissingPreflightExcludesWBAndUnregisteredProperties(t *testing.T) {
	packets := []*XMPPacket{EmptyXMPPacket(), EmptyXMPPacket(), EmptyXMPPacket()}
	for _, name := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint", "Exposure", "ChromaticAberrationB", "ChromaticAberrationR", "Defringe"} {
		if err := packets[1].SetScalar("crs:"+name, "1"); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := prepareNumericTransitionInputs(packets)
	if err != nil {
		t.Fatal(err)
	}
	for i := range packets {
		if len(plan.Missing[i]) != 0 {
			t.Fatalf("copied excluded values: %v", plan.Missing)
		}
	}
	for _, name := range []string{"Exposure", "ChromaticAberrationB", "ChromaticAberrationR", "Defringe"} {
		if _, found := plan.Values[1][name]; found {
			t.Fatalf("adopted unregistered %s", name)
		}
	}
	for name := range numericPreflightExclusions {
		if plan.Values[1][name] != 1 {
			t.Fatalf("lost existing %s", name)
		}
	}
}

func TestNumericMissingSelectedAnchorsMatchExplicitControl(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 5)
	control := makeFrames(t, t.TempDir(), 5)
	for i := range frames {
		properties := map[string]string{"crs:Exposure2012": []string{"0", "1.5", "", "4.5", "4"}[i]}
		if i == 2 {
			delete(properties, "crs:Exposure2012")
		}
		if i%2 == 0 {
			properties["xmp:Rating"] = "4"
		}
		writeTestXMP(t, frames[i].Image, properties)
		if i == 2 {
			properties["crs:Exposure2012"] = "1.5"
		}
		writeTestXMP(t, control[i].Image, properties)
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	control, err = DiscoverSequence(filepath.Dir(control[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	_, expected := interpolationPackets(t, control, 50)
	for i := range packets {
		text, _, err := packets[i].GetScalar("crs:Exposure2012")
		want, _, _ := expected[i].GetScalar("crs:Exposure2012")
		if err != nil || text != want {
			t.Fatalf("frame %d got %s want %s err=%v", i, text, want, err)
		}
	}
	assertTransitionScalar(t, packets[2], "Exposure2012", 1.5)
}

func TestNumericMissingBothEndpointKeysInheritInteriorDouble(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 5)
	for i, frame := range frames {
		properties := map[string]string{}
		if i == 0 || i == 4 {
			properties["xmp:Rating"] = "4"
		}
		if i == 2 {
			properties["crs:Exposure2012"] = "-.123456789123"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for i, p := range packets {
		text, found, err := p.GetScalar("crs:Exposure2012")
		value, _ := scalarNumber(text)
		if err != nil || !found || math.Abs(value+.123456789123) > 1e-14 {
			t.Fatalf("frame %d got %s found=%t err=%v", i, text, found, err)
		}
	}
}

func TestNumericMissingCropKeysUseFilledValuesBeforeRatingSelection(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 7)
	for i, frame := range frames {
		properties := map[string]string{}
		if i == 0 || i == 6 {
			properties["xmp:Rating"] = "4"
		}
		if i == 2 || i == 4 {
			properties["xmp:Rating"] = "5"
		}
		if i == 1 {
			properties["crs:CropLeft"] = ".2"
		}
		if i == 5 {
			properties["crs:CropLeft"] = ".6"
		}
		if i == 3 {
			properties["crs:Exposure2012"] = "1"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for _, i := range []int{2, 4} {
		assertTransitionScalar(t, packets[i], "CropLeft", .2)
	}
	for _, p := range packets {
		assertTransitionScalar(t, p, "Exposure2012", 1)
	}
}

func TestNumericMissingMalformedValuesFailBeforeSidecarWrites(t *testing.T) {
	for _, value := range []string{"not-a-number", "", "NaN", "Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 3)
			for i, frame := range frames {
				properties := map[string]string{"crs:Exposure2012": "1"}
				if i != 1 {
					properties["xmp:Rating"] = "4"
				}
				if i == 2 {
					properties["crs:Exposure2012"] = value
				}
				writeTestXMP(t, frame.Image, properties)
			}
			original, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := Main([]string{"interpolate", filepath.Dir(frames[0].Image), "--write"}, &out, &stderr); code == 0 {
				t.Fatal("accepted malformed numeric input")
			}
			after, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("failed preflight modified earlier sidecar")
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
			if len(backups) != 0 {
				t.Fatal("failed preflight created backups")
			}
		})
	}
}

func TestInterpolationPropertyCountIncludesScalarChanges(t *testing.T) {
	plan := InterpolationPlan{Changes: []ProposedFrame{{Values: map[string]string{"Exposure2012": "1"}, ScalarValues: map[string]string{"crs:WhiteBalance": "Custom"}, ScalarRemovals: []string{"crs:Temperature"}}}}
	if plan.PropertyCount() != 3 {
		t.Fatalf("count=%d", plan.PropertyCount())
	}
}

func TestNumericMissingCLIWriteBackupAndRenderedExposure(t *testing.T) {
	frames := cropFixture(t, 5)
	var imageData bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 64})
		}
	}
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		if err := os.WriteFile(frame.Image, imageData.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		properties := map[string]string{}
		if i == 0 || i == 4 {
			properties["xmp:Rating"] = "4"
		}
		if i == 2 {
			properties["crs:Exposure2012"] = "1"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	original, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	dir := filepath.Dir(frames[0].Image)
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preview wrote a missing anchor")
	}
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for _, frame := range frames {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertTransitionScalar(t, p, "Exposure2012", 1)
	}
	backups, err := filepath.Glob(frames[0].Sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("missing-anchor backup differs from original")
	}
	beforeRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("repeat: %s", &stderr)
	}
	afterRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(afterRepeat) != len(beforeRepeat) {
		t.Fatal("no-op repeat created a backup")
	}
	output := filepath.Join(dir, "filled.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-exposure", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// Independent linear-sRGB exposure calculation for 64/255 at +1 EV.
	want := 255 * (1.055*math.Pow(2*math.Pow((64.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
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
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(data, imageData.Bytes()) {
			t.Fatal("render modified source image")
		}
	}
}
