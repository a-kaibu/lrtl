package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderInputProfileTransformReferenceColors(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(7, 11, 8, 12))
	input.SetNRGBA64(7, 11, color.NRGBA64{R: 52428, G: 26214, B: 13107, A: 65535}) // 0.8, 0.4, 0.2
	for _, tc := range []struct {
		profile string
		want    color.RGBA64
		tol     uint16
	}{
		{profile: "display-p3", want: color.RGBA64{R: 56332, G: 24274, B: 8089, A: 65535}, tol: 4},
		{profile: "adobe-rgb", want: color.RGBA64{R: 59492, G: 26246, B: 11140, A: 65535}, tol: 16},
		{profile: "prophoto-rgb", want: color.RGBA64{R: 65535, G: 20958, B: 12192, A: 65535}, tol: 4},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			got, err := convertRenderInputProfile(input, tc.profile)
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != input.Bounds() {
				t.Fatalf("bounds %v, want %v", got.Bounds(), input.Bounds())
			}
			pixel := got.At(7, 11)
			r, g, b, a := pixel.RGBA()
			values := []uint16{uint16(r), uint16(g), uint16(b), uint16(a)}
			want := []uint16{tc.want.R, tc.want.G, tc.want.B, tc.want.A}
			for i := range values {
				if difference16(values[i], want[i]) > tc.tol {
					t.Fatalf("RGBA64=%v, want %v (tolerance %d)", values, want, tc.tol)
				}
			}
		})
	}
}

func TestRenderInputProfileResolutionIsExplicit(t *testing.T) {
	frames := []Frame{{Image: "must-not-be-opened"}}
	for _, profile := range []string{"adobe-rgb", "display-p3", "prophoto-rgb"} {
		t.Run(profile, func(t *testing.T) {
			got, err := resolveRenderICC(frames, RenderOptions{InputColor: profile, DynamicRange: "pq"}, true)
			if err != nil {
				t.Fatal(err)
			}
			if got.inputProfile != profile || got.InputColor != "bt709" || got.DynamicRange != "pq" {
				t.Fatalf("resolved input %+v; want explicit %s converted to BT.709, keeping PQ setting", got, profile)
			}
			reresolved, err := resolveRenderICC(frames, got, true)
			if err != nil || reresolved.inputProfile != profile || reresolved.InputColor != "bt709" {
				t.Fatalf("second resolution lost explicit input profile: %+v/%v", reresolved, err)
			}
		})
	}
}

func TestRenderInputProfileTransformPreservesAlphaAndRejectsUnknown(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(-2, 4, -1, 6))
	input.SetNRGBA64(-2, 4, color.NRGBA64{R: 0x8000, G: 0x8000, B: 0x8000, A: 0x8000})
	input.SetNRGBA64(-2, 5, color.NRGBA64{R: 65535, G: 0, B: 0, A: 0})
	got, err := convertRenderInputProfile(input, "adobe-rgb")
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != input.Bounds() {
		t.Fatalf("bounds %v, want %v", got.Bounds(), input.Bounds())
	}
	semi := color.RGBAModel.Convert(got.At(-2, 4)).(color.RGBA)
	_, _, _, alpha := got.At(-2, 4).RGBA()
	if alpha != 0x8000 || semi.R == 0 || semi.G == 0 || semi.B == 0 {
		t.Fatalf("half-alpha pixel=%v / alpha=%d", semi, alpha)
	}
	transparent := got.At(-2, 5)
	_, _, _, alpha = transparent.RGBA()
	if alpha != 0 {
		t.Fatalf("transparent alpha=%d, want 0", alpha)
	}
	if _, err := convertRenderInputProfile(input, "unknown-rgb"); err == nil {
		t.Fatal("unknown input profile accepted")
	}
}

func TestRenderInputProfileCLIConvertsRGB48AndAutoKeepsPixels(t *testing.T) {
	dir := t.TempDir()
	input := image.NewNRGBA64(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			input.SetNRGBA64(x, y, color.NRGBA64{R: 52428, G: 26214, B: 13107, A: 65535})
		}
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, input); err != nil {
		t.Fatal(err)
	}
	profileChunk := testICCPNG(testICCProfile("desc", "Display P3"))
	encoded := append([]byte{}, pngBytes.Bytes()[:33]...)
	encoded = append(encoded, profileChunk[8:len(profileChunk)-12]...)
	encoded = append(encoded, pngBytes.Bytes()[33:]...)
	for frame := 0; frame < 3; frame++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.png", frame)), encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []struct {
		profile string
		want    [3]uint16
		tol     uint16
		output  string
		extra   []string
		frames  int
	}{
		{profile: "display-p3", want: [3]uint16{56332, 24274, 8089}, tol: 4, output: "lrt-7.5.5", frames: 3},
		{profile: "adobe-rgb", want: [3]uint16{59492, 26246, 11140}, tol: 16, output: "lrt-7.5.5", extra: []string{"--deflicker"}, frames: 3},
		{profile: "prophoto-rgb", want: [3]uint16{65535, 20958, 12192}, tol: 4, output: "lrt-7.5.5", extra: []string{"--motion-blur", "2"}, frames: 1},
		{profile: "auto", want: [3]uint16{52428, 26214, 13107}, tol: 0, output: "standard", frames: 3},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "output.mp4")
			args := []string{"render", dir, "--output", output, "--ffmpeg", executable, "--output-color", tc.output}
			if tc.profile != "auto" {
				args = append(args, "--input-color", tc.profile)
			}
			args = append(args, tc.extra...)
			markerCommand(t, args...)
			raw := denoiseRead(t, capture)
			frameBytes := 16 * 16 * 6
			if len(raw) != tc.frames*frameBytes {
				t.Fatalf("captured %d bytes, want %d RGB48 frames", len(raw), tc.frames)
			}
			for frame := 0; frame < tc.frames; frame++ {
				pixel := raw[frame*frameBytes : frame*frameBytes+6]
				for channel, want := range tc.want {
					got := binary.LittleEndian.Uint16(pixel[channel*2:])
					if difference16(got, want) > tc.tol {
						t.Fatalf("frame %d channel %d=%d, want %d (profile %s)", frame, channel, got, want, tc.profile)
					}
				}
			}
			var flags []string
			if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(flags, " ")
			if !strings.Contains(joined, "-pixel_format rgb48le") {
				t.Fatalf("profile transform did not route through RGB48: %s", joined)
			}
		})
	}
}

func difference16(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}
