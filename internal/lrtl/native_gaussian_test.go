package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func nativeFilterReferenceCPU() string {
	if nativeGaussianFused() {
		return "native"
	}
	return "0"
}

func TestNativeGaussianDispatch(t *testing.T) {
	profile := nativeFilterReferenceCPU()
	if expected := os.Getenv("LRTL_EXPECT_NATIVE_CPU"); expected != "" && profile != expected {
		t.Fatalf("native CPU selection=%s want%s", profile, expected)
	}
	t.Logf("selected native filter CPU profile: %s", profile)
}

func TestNativeGaussianFMA32Midpoints(t *testing.T) {
	tiny := float32(math.Ldexp(1, -100))
	for _, tc := range []struct {
		feedback, previous, current float32
		bits                        uint32
	}{
		{math.Float32frombits(0x3f000003), 3, tiny, 0x3fc00005},
		{math.Float32frombits(0x3f000001), 3, -tiny, 0x3fc00001},
		{-math.Float32frombits(0x3f000003), 3, -tiny, 0xbfc00005},
		{.5, 2, 0, 0x3f800000},
		{0, 1, 0, 0},
		{float32(math.Ldexp(1, -75)), float32(math.Ldexp(1, -75)), 0, 0},
		{float32(math.Ldexp(1, -75)), float32(math.Ldexp(1, -75)), math.SmallestNonzeroFloat32, 2},
		{math.MaxFloat32, 2, 0, 0x7f800000},
	} {
		got := gaussianFeedbackFMA32(tc.feedback, tc.previous, tc.current)
		if math.Float32bits(got) != tc.bits {
			t.Fatalf("FMA32(%g,%g,%g)=%08x want%08x", tc.feedback, tc.previous, tc.current, math.Float32bits(got), tc.bits)
		}
	}
	naive := float32(math.FMA(float64(math.Float32frombits(0x3f000003)), 3, float64(tiny)))
	if math.Float32bits(naive) != 0x3fc00004 {
		t.Fatal("midpoint regression no longer exercises double rounding")
	}
}

func nativeFilterReferenceCommand(t *testing.T, executable string, input []byte, args ...string) []byte {
	t.Helper()
	if !nativeGaussianFused() {
		args = append([]string{"-cpuflags", "0"}, args...)
	}
	return nativeColorCommand(t, executable, input, args...)
}

type nativeGaussianObservation struct {
	Width, Height, Depth    int
	Sigma                   float64
	Pattern, CPUFlags, Hash string
	Bytes                   int
}

func gaussianReferenceInput(width, height, depth int, pattern string) []uint16 {
	maximum := (1 << depth) - 1
	input := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := (x*13 + y*23 + x*y*7) % 101 * maximum / 100
			switch pattern {
			case "ramp":
				value = (x + y*width) % (maximum + 1)
			case "impulse":
				value = 0
				if x == width/2 && y == height/2 {
					value = maximum
				}
			}
			input[y*width+x] = uint16(value)
		}
	}
	return input
}

func gaussianReferenceBytes(plane []uint16, depth int) []byte {
	var raw []byte
	for _, value := range plane {
		if depth == 8 {
			raw = append(raw, byte(value))
		} else {
			raw = binary.LittleEndian.AppendUint16(raw, value)
		}
	}
	return raw
}

func TestNativeGaussianLiveReferences(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to compare Gaussian arithmetic")
	}
	for _, size := range [][2]int{{2, 2}, {8, 8}, {17, 9}, {31, 17}, {32, 16}, {64, 32}, {65, 33}, {127, 65}} {
		for _, depth := range []int{8, 10} {
			for _, sigma := range []float64{.5, 15} {
				for _, pattern := range []string{"noise", "ramp", "impulse"} {
					for _, cpu := range []string{"0", "native"} {
						if cpu == "native" && !nativeGaussianFused() {
							continue
						}
						width, height := size[0], size[1]
						t.Run(fmt.Sprintf("%dx%d/%d/%g/%s/%s", width, height, depth, sigma, pattern, cpu), func(t *testing.T) {
							input := gaussianReferenceInput(width, height, depth, pattern)
							format := "gray"
							if depth == 10 {
								format = "gray10le"
							}
							args := []string{"-v", "error", "-filter_threads", "1"}
							if cpu == "0" {
								args = append(args, "-cpuflags", "0")
							}
							args = append(args, "-f", "rawvideo", "-pixel_format", format, "-video_size", fmt.Sprintf("%dx%d", width, height), "-i", "pipe:0", "-vf", fmt.Sprintf("gblur=sigma=%g", sigma), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", format, "pipe:1")
							want := nativeColorCommand(t, executable, gaussianReferenceBytes(input, depth), args...)
							got := gaussianReferenceBytes(gaussianOutputPlaneArithmetic(input, width, height, sigma, (1<<depth)-1, cpu == "native"), depth)
							t.Logf("GAUSSIAN width=%d height=%d depth=%d sigma=%g pattern=%s cpu=%s bytes=%d sha256=%x", width, height, depth, sigma, pattern, cpu, len(want), sha256.Sum256(want))
							if !bytes.Equal(got, want) {
								t.Fatalf("Gaussian differs: hashes%x/%x", sha256.Sum256(got), sha256.Sum256(want))
							}
						})
					}
				}
			}
		}
	}
}

func TestNativeGaussianOfflineReferences(t *testing.T) {
	var rows []nativeGaussianObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-gaussian-arithmetic-8.0.1.json"), &rows); err != nil || len(rows) != 192 {
		t.Fatalf("expected 192 Gaussian arithmetic observations: %v", err)
	}
	for _, row := range rows {
		input := gaussianReferenceInput(row.Width, row.Height, row.Depth, row.Pattern)
		before := append([]uint16(nil), input...)
		got := gaussianReferenceBytes(gaussianOutputPlaneArithmetic(input, row.Width, row.Height, row.Sigma, (1<<row.Depth)-1, row.CPUFlags == "native"), row.Depth)
		if len(got) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != row.Hash {
			t.Fatalf("Gaussian reference changed: %+v got%x", row, sha256.Sum256(got))
		}
		for i := range input {
			if input[i] != before[i] {
				t.Fatal("Gaussian modified source")
			}
		}
	}
}

func TestNativeGaussianSmallFrameThreadDispatch(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" || !nativeGaussianFused() {
		t.Skip("requires bundled FFmpeg and the measured native AVX2 dispatch")
	}
	input := gaussianReferenceInput(8, 8, 10, "noise")
	raw := gaussianReferenceBytes(input, 10)
	args := []string{"-v", "error", "-f", "rawvideo", "-pixel_format", "gray10le", "-video_size", "8x8", "-i", "pipe:0", "-vf", "gblur=sigma=15", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray10le", "pipe:1"}
	single := nativeColorCommand(t, executable, raw, append([]string{"-filter_threads", "1"}, args...)...)
	normal := nativeColorCommand(t, executable, raw, args...)
	want := gaussianReferenceBytes(gaussianOutputPlaneArithmetic(input, 8, 8, 15, 1023, true), 10)
	if !bytes.Equal(single, want) || len(normal) != len(single) {
		t.Fatal("single-thread native arithmetic changed")
	}
	differences := 0
	for i := 0; i < len(normal); i += 2 {
		a, b := binary.LittleEndian.Uint16(normal[i:]), binary.LittleEndian.Uint16(single[i:])
		if a != b {
			differences++
		}
		if a > b {
			t.Fatal("tiny-frame default dispatch exceeded the single-pass postscale")
		}
	}
	// Zero-length SIMD postscale jobs can overlap; the exact default-thread
	// tiny-frame result is intentionally not treated as a deterministic oracle.
	t.Logf("default versus one-thread 8x8 Gaussian: %d differing components", differences)
}

func FuzzNativeGaussianArithmetic(f *testing.F) {
	f.Add([]byte{0, 255, 16, 128, 64}, uint8(8), uint8(10))
	f.Add([]byte{255, 255, 0, 0, 127, 128}, uint8(17), uint8(8))
	f.Fuzz(func(t *testing.T, data []byte, shape, depthSelector uint8) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}
		width, height := int(shape%32)+2, int(data[0]%32)+2
		depth := 8
		if depthSelector%2 == 0 {
			depth = 10
		}
		peak := (1 << depth) - 1
		input := make([]uint16, width*height)
		for i := range input {
			input[i] = uint16(int(data[i%len(data)]) * peak / 255)
		}
		before := append([]uint16(nil), input...)
		for _, sigma := range []float64{.5, 15} {
			for _, fused := range []bool{false, true} {
				got := gaussianOutputPlaneArithmetic(input, width, height, sigma, peak, fused)
				if len(got) != len(input) {
					t.Fatal("Gaussian changed dimensions")
				}
				for i, v := range got {
					if int(v) > peak || input[i] != before[i] {
						t.Fatal("Gaussian changed source or exceeded its depth")
					}
				}
			}
		}
	})
}
