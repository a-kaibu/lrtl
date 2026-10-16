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
	"testing"
)

func TestWBNumericColumnAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-wb-numeric-column.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Smoothness int
		Cases      []struct {
			Property string
			Integer  bool
			Input    []*float64
			Keys     []int
			Expected []float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 16 || fixture.Smoothness != 100 {
		t.Fatal("incomplete component fixture")
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d", c.Property, n), func(t *testing.T) {
			if globalTransitionSpecs[c.Property].integer != c.Integer {
				t.Fatal("registered numeric type differs from installed LRT")
			}
			frames := cropFixture(t, len(c.Input))
			for i, frame := range frames {
				properties := map[string]string{"crs:WhiteBalance": "Custom"}
				for _, key := range c.Keys {
					if key == i {
						properties["xmp:Rating"] = "4"
					}
				}
				if c.Input[i] != nil {
					properties["crs:"+c.Property] = fmt.Sprintf("%.15g", *c.Input[i])
				}
				writeTestXMP(t, frame.Image, properties)
			}
			_, packets := interpolationPackets(t, frames, fixture.Smoothness)
			for i, packet := range packets {
				text, found, err := packet.GetScalar("crs:" + c.Property)
				value, valid := scalarNumber(text)
				if err != nil || !found || !valid || math.Abs(value-c.Expected[i]) > 1e-12 {
					t.Fatalf("frame %d got %s found=%t want %.15g err=%v", i, text, found, c.Expected[i], err)
				}
			}
		})
	}
}

func TestWBNumericMissingKeyUsesNonKeyDonorAtAllSmoothnessValues(t *testing.T) {
	for _, name := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
		for _, smoothness := range []int{0, 50, 100} {
			t.Run(fmt.Sprintf("%s/%d", name, smoothness), func(t *testing.T) {
				frames, control := cropFixture(t, 5), cropFixture(t, 5)
				values := []string{"10", "20", "", "30", "40"}
				if name == "Temperature" {
					values = []string{"3000", "4000", "", "6000", "7000"}
				}
				for i := range frames {
					properties := map[string]string{"crs:WhiteBalance": "Custom"}
					if i%2 == 0 {
						properties["xmp:Rating"] = "4"
					}
					if values[i] != "" {
						properties["crs:"+name] = values[i]
					}
					writeTestXMP(t, frames[i].Image, properties)
					if i == 2 {
						properties["crs:"+name] = values[1]
					}
					writeTestXMP(t, control[i].Image, properties)
				}
				_, actual := interpolationPackets(t, frames, smoothness)
				_, expected := interpolationPackets(t, control, smoothness)
				for i := range actual {
					got, found, err := actual[i].GetScalar("crs:" + name)
					want, _, _ := expected[i].GetScalar("crs:" + name)
					if err != nil || !found || got != want {
						t.Fatalf("frame %d got %s want %s err=%v", i, got, want, err)
					}
				}
				want, _ := scalarNumber(values[1])
				assertTransitionScalar(t, actual[2], name, want)
			})
		}
	}
}

func TestWBNumericMissingKeyCLIWriteAndVideo(t *testing.T) {
	frames, control := cropFixture(t, 5), cropFixture(t, 5)
	var source bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 128})
		}
	}
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	// This Tint input/output pair was independently observed in the installed
	// column component. The control sequence contains its complete output.
	input := []string{"10", "", "", "30", ""}
	expected := []float64{10, 10, 10, 20, 30}
	for i := range frames {
		for _, group := range [][]Frame{frames, control} {
			if err := os.WriteFile(group[i].Image, source.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		properties := map[string]string{"crs:WhiteBalance": "Custom"}
		if i%2 == 0 {
			properties["xmp:Rating"] = "4"
		}
		if input[i] != "" {
			properties["crs:Tint"] = input[i]
		}
		writeTestXMP(t, frames[i].Image, properties)
		properties["crs:Tint"] = formatNumber(expected[i])
		writeTestXMP(t, control[i].Image, properties)
	}
	original, err := os.ReadFile(frames[2].OutputSidecar())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir, "--smoothness", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[2].OutputSidecar())
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preview changed missing keyframe")
	}
	if code := Main([]string{"interpolate", dir, "--smoothness", "100", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertTransitionScalar(t, packet, "Tint", expected[i])
	}
	backups, err := filepath.Glob(frames[2].OutputSidecar() + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("missing-key backup differs from original")
	}
	plan, err := buildPlanWithSmoothness(frames, 100)
	if err != nil || plan.PropertyCount() != 0 {
		t.Fatalf("repeat differs: %+v err=%v", plan, err)
	}
	var videos [][]byte
	for _, group := range [][]Frame{frames, control} {
		groupDir := filepath.Dir(group[0].Image)
		output := filepath.Join(groupDir, "wb.avi")
		if code := Main([]string{"render", groupDir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
			t.Fatalf("render: %s", &stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		videos = append(videos, data)
	}
	if !bytes.Equal(videos[0], videos[1]) {
		t.Fatal("WB interpolation video differs from complete native-component metadata control")
	}
	data, count := videos[0], 0
	var first, last color.NRGBA
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 {
			t.Fatal("truncated video frame")
		}
		frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		last = color.NRGBAModel.Convert(frame.At(32, 16)).(color.NRGBA)
		if count == 0 {
			first = last
		}
		count++
		data = data[end+2:]
	}
	if count != 5 || first == last {
		t.Fatalf("video did not progress: count=%d first=%v last=%v", count, first, last)
	}
	for _, group := range [][]Frame{frames, control} {
		for _, frame := range group {
			data, err := os.ReadFile(frame.Image)
			if err != nil || !bytes.Equal(data, source.Bytes()) {
				t.Fatal("source image modified")
			}
		}
	}
}
