package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestXMPGrayscaleParsing(t *testing.T) {
	for _, value := range []string{"True", " true ", "1", "False", "0"} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("crs:ConvertToGrayscale", value); err != nil {
			t.Fatal(err)
		}
		for i, band := range xmpHSLBands {
			if err := packet.SetScalar("crs:GrayMixer"+band.name, fmt.Sprint(i*20-70)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := readXMPBasicPacket(packet)
		want := value == "True" || value == " true " || value == "1"
		if err != nil || got.grayscale != want {
			t.Fatalf("%q: grayscale=%t, %v", value, got.grayscale, err)
		}
		for i, v := range got.grayMix {
			if v != float64(i*20-70) {
				t.Fatal("wrong mixer band")
			}
		}
		if got.globalActive() != want {
			t.Fatal("inactive grayscale mixer changed color processing")
		}
	}
	for _, property := range []string{"ConvertToGrayscale", "GrayMixerRed", "GrayMixerOrange", "GrayMixerYellow", "GrayMixerGreen", "GrayMixerAqua", "GrayMixerBlue", "GrayMixerPurple", "GrayMixerMagenta"} {
		values := []string{"NaN", "Infinity", "oops", ""}
		if property != "ConvertToGrayscale" {
			values = append(values, "-100.01", "100.01")
		}
		for _, value := range values {
			packet := EmptyXMPPacket()
			if err := packet.SetScalar("crs:"+property, value); err != nil {
				t.Fatal(err)
			}
			if _, err := readXMPBasicPacket(packet); err == nil || !strings.Contains(err.Error(), property) {
				t.Fatalf("accepted %s=%q: %v", property, value, err)
			}
		}
	}
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><r:RDF xmlns:r="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><r:Description xmlns:c="http://ns.adobe.com/camera-raw-settings/1.0/"><c:ConvertToGrayscale>True</c:ConvertToGrayscale><c:GrayMixerBlue>-100</c:GrayMixerBlue></r:Description></r:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := readXMPBasicPacket(packet)
	if err != nil || !got.grayscale || got.grayMix[5] != -100 {
		t.Fatalf("element/prefix parsing: %+v %v", got, err)
	}
}

func TestXMPGrayscaleLuminanceAndMixer(t *testing.T) {
	for _, pixel := range [][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {.2, .4, .6}, {0, 0, 0}, {1, 1, 1}} {
		want := .2126*pixel[0] + .7152*pixel[1] + .0722*pixel[2]
		if got := xmpGrayscaleLuminance(pixel[0], pixel[1], pixel[2], [8]float64{}); math.Abs(got-want) > 1e-14 {
			t.Fatalf("neutral conversion %v: %g != %g", pixel, got, want)
		}
	}
	for index, hue := range xmpHSLHueCenters {
		r, g, b := hslToSRGB(hue, 1, .5)
		r, g, b = srgb16ToLinear[encodeSRGB16(r)], srgb16ToLinear[encodeSRGB16(g)], srgb16ToLinear[encodeSRGB16(b)]
		base := xmpGrayscaleLuminance(r, g, b, [8]float64{})
		var mixer [8]float64
		mixer[index] = 100
		bright := xmpGrayscaleLuminance(r, g, b, mixer)
		mixer[index] = -100
		dark := xmpGrayscaleLuminance(r, g, b, mixer)
		if !(dark < base && base < bright) {
			t.Fatalf("band %d does not control its color", index)
		}
		if math.Abs(bright/base-4) > .001 || math.Abs(dark/base-.25) > .001 {
			t.Fatal("mixer stop range changed")
		}
		mixer = [8]float64{}
		mixer[(index+4)%8] = 100
		if got := xmpGrayscaleLuminance(r, g, b, mixer); math.Abs(got-base) > 1e-10 {
			t.Fatal("unrelated band changed luminance")
		}
	}
	var mixer [8]float64
	for i := range mixer {
		mixer[i] = float64(i*25 - 100)
	}
	for _, gray := range []float64{0, .01, .18, .5, 1} {
		if got := xmpGrayscaleLuminance(gray, gray, gray, mixer); math.Abs(got-gray) > 1e-14 {
			t.Fatal("gray mixer changed neutral pixels")
		}
	}
}

func TestXMPGrayscalePipeline(t *testing.T) {
	img := precisionToneImage()
	before := append([]byte(nil), img.Pix...)
	adjustments := xmpBasicAdjustments{grayscale: true}
	mono := applyXMPBasic(img, adjustments)
	left, right := mono.NRGBA64At(15, 19), mono.NRGBA64At(30, 19)
	if left.R != left.G || left.G != left.B || left.R == right.R || left.R%257 == 0 || left.A != 50000 || mono.Bounds() != img.Bounds() {
		t.Fatalf("lost monochrome/precision/alpha/bounds: %v %v", left, right)
	}
	if !bytes.Equal(before, img.Pix) {
		t.Fatal("modified input")
	}
	adjustments.hasColorMix = true
	adjustments.colorMix[0] = xmpHSLColorAdjustment{hue: 100, saturation: 100, luminance: 100}
	adjustments.saturation, adjustments.vibrance = 100, 100
	if !bytes.Equal(mono.Pix, applyXMPBasic(img, adjustments).Pix) {
		t.Fatal("color mixer affected monochrome")
	}
	adjustments.colorGrading = xmpColorGrading{active: true, global: xmpColorGradeTone{hue: 30, saturation: 50}, blending: 50}
	tinted := applyXMPBasic(img, adjustments).NRGBA64At(15, 19)
	if tinted.R == tinted.B {
		t.Fatal("grayscale discarded subsequent color grading")
	}
	adjustments = xmpBasicAdjustments{grayscale: true, exposure: 1, hasWhiteBalance: true, whiteBalance: [3]float64{1.1, 1, .8}}
	got := applyXMPBasic(img, adjustments).NRGBA64At(15, 19)
	p := img.NRGBA64At(15, 19)
	want := encodeLinearSRGB16(2 * (.2126*srgb16ToLinear[p.R]*1.1 + .7152*srgb16ToLinear[p.G] + .0722*srgb16ToLinear[p.B]*.8))
	if got.R != want || got.G != want || got.B != want {
		t.Fatalf("exposure/WB order: %v want %d", got, want)
	}
}

func TestXMPGrayscaleExportPreviewAndVideo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "frame.png")
	img := image.NewNRGBA64(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{uint16(20000 + x*500), uint16(10000 + y*600), uint16(40000 - x*200), 65535})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{"crs:ConvertToGrayscale": "True", "crs:GrayMixerRed": "40", "crs:GrayMixerBlue": "-30"})
	before := batchFileSnapshot(t, root)
	output, preview := filepath.Join(t.TempDir(), "tiff"), filepath.Join(t.TempDir(), "jpeg")
	video := filepath.Join(t.TempDir(), "mono.avi")
	var out, stderr bytes.Buffer
	for _, args := range [][]string{
		{"export", root, "--output", output, "--apply-xmp-basic"},
		{"preview-images", root, "--output", preview},
		{"render", root, "--output", video, "--apply-xmp-basic"},
	} {
		if Main(args, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	decoded := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			p := color.NRGBA64Model.Convert(decoded.At(x, y)).(color.NRGBA64)
			if p.R != p.G || p.G != p.B {
				t.Fatal("export stayed colored")
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("rendering changed source files")
	}
	manifest := readPreviewManifest(t, preview)
	if len(manifest.Frames) != 1 {
		t.Fatal("missing monochrome preview")
	}
	for _, path := range []string{video, filepath.Join(preview, manifest.Frames[0].File)} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			t.Fatal("missing JPEG frame")
		}
		frame, err := jpeg.Decode(bytes.NewReader(data[start:]))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 16; y++ {
			for x := 0; x < 32; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				if math.Abs(float64(r)-float64(g)) > 257 || math.Abs(float64(g)-float64(b)) > 257 {
					t.Fatal("preview/video stayed colored")
				}
			}
		}
	}
}

func FuzzXMPGrayscale(f *testing.F) {
	f.Add(uint16(24000), uint16(12000), uint16(50000), int8(80))
	f.Add(uint16(65535), uint16(65535), uint16(65535), int8(-100))
	f.Fuzz(func(t *testing.T, r, g, b uint16, slider int8) {
		v := math.Max(-100, math.Min(100, float64(slider)))
		var mixer [8]float64
		for i := range mixer {
			mixer[i] = v
		}
		got := xmpGrayscaleLuminance(srgb16ToLinear[r], srgb16ToLinear[g], srgb16ToLinear[b], mixer)
		if math.IsNaN(got) || math.IsInf(got, 0) || got < 0 || got > 4.000001 {
			t.Fatalf("invalid gray value %g", got)
		}
		if r == g && g == b && math.Abs(got-srgb16ToLinear[r]) > 1e-14 {
			t.Fatal("neutral pixel changed")
		}
	})
}

func TestXMPGrayscaleInvalidDoesNotPublish(t *testing.T) {
	for _, property := range []string{"ConvertToGrayscale", "GrayMixerBlue"} {
		t.Run(property, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "frame.png")
			writeRenderFrame(t, path, "png", 32, 16)
			writeTestXMP(t, path, map[string]string{"crs:" + property: "invalid"})
			before := batchFileSnapshot(t, root)
			parent := t.TempDir()
			var out, stderr bytes.Buffer
			if Main([]string{"preview-images", root, "--output", filepath.Join(parent, "previews"), "--write-preview-ev"}, &out, &stderr) == 0 {
				t.Fatal("invalid grayscale accepted")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("invalid grayscale published output or modified source")
			}
		})
	}
}

func TestXMPGrayscaleRealRAW(t *testing.T) {
	source := os.Getenv("LRTL_RAW_SAMPLE")
	if source == "" {
		t.Skip("set LRTL_RAW_SAMPLE to check real RAW monochrome generation")
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := writeTestXMP(t, filepath.Join(t.TempDir(), "frame.raw"), map[string]string{"crs:ConvertToGrayscale": "True", "crs:GrayMixerBlue": "-30", "crs:GrayMixerOrange": "25"})
	output := filepath.Join(t.TempDir(), "previews")
	m, err := GeneratePreviewImages([]Frame{{Image: source, Sidecar: sidecar}}, output, RenderOptions{Quality: 94, LongEdge: 128, RawDevelop: true, ApplyXMPBasic: true, AutoOrient: true, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER")})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, m.Frames[0].File))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	minimum, maximum := uint32(65535), uint32(0)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if math.Abs(float64(r)-float64(g)) > 257 || math.Abs(float64(g)-float64(b)) > 257 {
				t.Fatal("RAW preview stayed colored")
			}
			minimum, maximum = min(minimum, r), max(maximum, r)
		}
	}
	if maximum-minimum < 257 {
		t.Fatal("RAW preview lost image content")
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("RAW source modified")
	}
}
