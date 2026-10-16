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
	"strconv"
	"testing"
)

func TestNumericCopyAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-numeric-copy.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Property        string
			Input, Expected []float64
			Ratings         []int
			Copied          bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 14 {
		t.Fatal("incomplete installed component fixture")
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d", c.Property, n), func(t *testing.T) {
			packets := make([]*XMPPacket, len(c.Input))
			for i, value := range c.Input {
				packets[i] = EmptyXMPPacket()
				if err := packets[i].SetScalar("crs:"+c.Property, strconv.FormatFloat(value, 'g', -1, 64)); err != nil {
					t.Fatal(err)
				}
				if err := packets[i].SetScalar("xmp:Rating", fmt.Sprint(c.Ratings[i])); err != nil {
					t.Fatal(err)
				}
			}
			inputs, err := prepareNumericTransitionInputs(packets)
			if err != nil {
				t.Fatal(err)
			}
			normal, crop, _ := transitionKeyframeGroups(packets)
			keys := normal
			if c.Property == "CropLeft" {
				keys = crop
			}
			value, copied := numericColumnCopyReference(inputs.Values, c.Property, keys, globalTransitionSpecs[c.Property].integer)
			if copied != c.Copied || (copied && math.Float64bits(value) != math.Float64bits(c.Expected[0])) {
				t.Fatalf("gate=%t value=%g want gate=%t value=%g", copied, value, c.Copied, c.Expected[0])
			}
			if !copied {
				return // The fixture records only the gate and copy component.
			}
			frames := cropFixture(t, len(packets))
			for i, frame := range frames {
				writeTestXMP(t, frame.Image, map[string]string{"xmp:Rating": fmt.Sprint(c.Ratings[i]), "crs:" + c.Property: strconv.FormatFloat(c.Input[i], 'g', -1, 64), "crs:WhiteBalance": "Custom"})
			}
			for _, smoothness := range []int{0, 50, 100} {
				_, actual := interpolationPackets(t, frames, smoothness)
				for i, packet := range actual {
					text, found, err := packet.GetScalar("crs:" + c.Property)
					got, valid := scalarNumber(text)
					if err != nil || !found || !valid || math.Float64bits(got) != math.Float64bits(c.Expected[i]) {
						t.Fatalf("smoothness %d frame %d got %s want %g err=%v", smoothness, i, text, c.Expected[i], err)
					}
				}
			}
		})
	}
}

func TestNumericCopyCLIBackupAndUnclampedExposureVideo(t *testing.T) {
	frames := cropFixture(t, 3)
	var source bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 16})
		}
	}
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		if err := os.WriteFile(frame.Image, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		properties := map[string]string{"crs:Exposure2012": "0"}
		if i == 1 {
			properties["xmp:Rating"] = "4"
			properties["crs:Exposure2012"] = "6"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	original, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preview wrote sidecars")
	}
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertTransitionScalar(t, packet, "Exposure2012", 6)
	}
	backups, err := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup differs from original")
	}
	plan, err := buildPlan(frames)
	if err != nil || plan.PropertyCount() != 0 {
		t.Fatalf("repeat differs: %+v err=%v", plan, err)
	}
	output := filepath.Join(dir, "copy.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-exposure", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// Independent linear-sRGB exposure formula. +6 EV is outside the normal
	// transition clamp, but the native reference-copy branch retains that value.
	want := 255 * (1.055*math.Pow(64*math.Pow((16.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
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
		r, _, _, _ := frame.At(32, 16).RGBA()
		if math.Abs(float64(r>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d want %g", count, r>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != 3 {
		t.Fatalf("frames=%d", count)
	}
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(data, source.Bytes()) {
			t.Fatal("source changed")
		}
	}
}

func TestNumericCopyGateKeepsMissingColumnsAndEndpointDifferences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []map[string]float64
		keys   []int
	}{
		{"empty", nil, nil},
		{"singleton", []map[string]float64{{"Tint": 1}}, []int{0}},
		{"missing-reference", []map[string]float64{nil, {"Tint": 1}}, []int{0}},
		{"missing-endpoint", []map[string]float64{nil, {"Tint": 1}, {"Tint": 1}}, []int{1, 2}},
		{"different-endpoint", []map[string]float64{{"Tint": 2}, {"Tint": 1}, {"Tint": 1}}, []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if value, copied := numericColumnCopyReference(tc.values, "Tint", tc.keys, true); copied {
				t.Fatalf("copied %g", value)
			}
		})
	}
}
