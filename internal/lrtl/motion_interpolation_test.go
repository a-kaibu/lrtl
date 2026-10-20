package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type motionInterpolationObservation struct {
	Depth                               int
	ScaleFlags                          string
	FPS                                 string
	Speed                               float64
	Width, Height, Frames, OutputFrames int
	CPUFlags, Pattern, Filter, Hash     string
}

func motionInterpolation10Observations(t *testing.T) []motionInterpolationObservation {
	t.Helper()
	var rows []motionInterpolationObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-motion-interpolation-10bit-8.0.1.json"), &rows); err != nil || len(rows) != 288 {
		t.Fatalf("expected 288 native 10-bit motion observations: %v", err)
	}
	return rows
}

func motionInterpolationInput(tc motionInterpolationObservation) ([]byte, [][3][]byte) {
	frames := motionInterpolationFixture(tc.Width, tc.Height, tc.Frames, tc.Pattern)
	var raw []byte
	for f := range frames {
		for p, plane := range frames[f] {
			if tc.Depth == 10 {
				converted := make([]uint16, len(plane))
				for i, v := range plane {
					converted[i] = uint16(v)*4 + uint16((i%tc.Width+i/tc.Width+p+f)%4)
					raw = binary.LittleEndian.AppendUint16(raw, converted[i])
				}
				frames[f][p] = quantizeInterpolationPlane(converted, tc.Width)
			} else {
				raw = append(raw, plane...)
			}
		}
	}
	return raw, frames
}

func motionInterpolationObservations(t *testing.T) []motionInterpolationObservation {
	t.Helper()
	var rows []motionInterpolationObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-motion-interpolation-8.0.1.json"), &rows); err != nil || len(rows) != 432 {
		t.Fatalf("expected 432 native motion observations: %v", err)
	}
	return rows
}

func motionInterpolationFixture(width, height, count int, pattern string) [][3][]byte {
	frames := make([][3][]byte, count)
	for f := range frames {
		dx, dy := 0, 0
		switch pattern {
		case "pan-x":
			dx = 2 * f
		case "pan-y":
			dy = 2 * f
		case "diagonal":
			dx, dy = 2*f, f
		case "accelerate":
			dx = f * (f + 1) / 2
		case "cut":
			dx = 2 * f
		}
		for p := range frames[f] {
			frames[f][p] = make([]byte, width*height)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					sx, sy := ((x-dx)%width+width)%width, ((y-dy)%height+height)%height
					v := 48 + (sx*3+sy*5+sx*sy%17+p*13)%128
					switch pattern {
					case "square":
						v = 32 + p*48
						left, top := width/4+3*f, height/3+f%3
						if x >= left && x < left+12 && y >= top && y < top+10 {
							v = (180 + p*23 + (x-left)*3 + (y-top)*5) % 256
						}
					case "noise":
						v = (x*13 + y*23 + x*y*7 + f*19 + p*31) % 101 * 255 / 100
					case "cut":
						if f >= count/2 {
							v = 255 - v
						}
					}
					frames[f][p][y*width+x] = byte(v)
				}
			}
		}
	}
	return frames
}

func TestMotionInterpolationMeasuredPlanes(t *testing.T) {
	differences, total := 0, 0
	for _, tc := range append(motionInterpolationObservations(t), motionInterpolation10Observations(t)...) {
		_, input := motionInterpolationInput(tc)
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		clock, err := newInterpolationClock(rate, tc.FPS, float64(float32(tc.Speed)))
		if err != nil {
			t.Fatal(err)
		}
		motion, err := newMotionEstimator(tc.Width, tc.Height)
		if err != nil {
			t.Fatal(err)
		}
		var previous float64
		scenes := make([]bool, len(input))
		for f := 0; f+1 < len(input); f++ {
			scenes[f], previous = interpolationSceneChange(input[f][0], input[f+1][0], previous)
		}
		hash, blendHash := sha256.New(), sha256.New()
		var outputBytes, blendBytes []byte
		j := 0
		count := clock.count(len(input))
		total += count * tc.Width * tc.Height * 3
		for target := 0; target < count; target++ {
			for j < clock.last(len(input)) && clock.outputPTS(j+1) <= target {
				j++
			}
			left, alpha := clock.sample(j, len(input))
			for motion.next <= left+1 {
				f := motion.next
				if err := motion.estimate(f, input[max(0, f-1)][0], input[f][0], input[f+1][0]); err != nil {
					t.Fatal(err)
				}
			}
			planes, err := motion.interpolate(left, alpha, input[left], input[left+1], scenes[left])
			if err != nil {
				t.Fatal(err)
			}
			outputBytes, blendBytes = outputBytes[:0], blendBytes[:0]
			for p, plane := range planes {
				blend := blendInterpolationPlane(input[left][p], input[left+1][p], alpha, scenes[left])
				if tc.Depth == 10 {
					for i, v := range plane {
						outputBytes = binary.LittleEndian.AppendUint16(outputBytes, uint16(v)*4)
						blendBytes = binary.LittleEndian.AppendUint16(blendBytes, uint16(blend[i])*4)
					}
				} else {
					hash.Write(plane)
					blendHash.Write(blend)
				}
			}
			if tc.Depth == 10 {
				hash.Write(outputBytes)
				blendHash.Write(blendBytes)
			}
		}
		if count != tc.OutputFrames || hex.EncodeToString(hash.Sum(nil)) != tc.Hash {
			t.Errorf("depth%d %s speed%g %dx%d frames%d %s: count%d want%d hash differs", tc.Depth, tc.FPS, tc.Speed, tc.Width, tc.Height, tc.Frames, tc.Pattern, count, tc.OutputFrames)
		}
		if hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(blendHash.Sum(nil)) {
			differences++
		}
	}
	if differences == 0 {
		t.Fatal("motion compensation reduced to blend in all observations")
	}
	t.Logf("%d plane samples; %d cases differ from blend", total, differences)
}

func TestMotionInterpolationLiveReferences(t *testing.T) {
	ffmpeg := os.Getenv("LRTL_MOTION_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set LRTL_MOTION_FFMPEG for native motion references")
	}
	for _, tc := range append(motionInterpolationObservations(t), motionInterpolation10Observations(t)...) {
		raw, _ := motionInterpolationInput(tc)
		format := "yuv444p"
		if tc.Depth == 10 {
			format = "yuv444p10le"
		}
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		filters := []string{tc.Filter}
		if tc.Pattern == "pan-x" && tc.Frames == 5 && tc.FPS == "24" && tc.Speed == .5 {
			filters = append(filters, strings.ReplaceAll(strings.ReplaceAll(tc.Filter, "mc_mode=aobmc", "mc_mode=obmc"), "vsbmc=1", "vsbmc=0"))
		}
		for _, filter := range filters {
			args := []string{"-v", "error"}
			if tc.CPUFlags == "0" {
				args = append(args, "-cpuflags", "0")
			}
			if tc.ScaleFlags != "" {
				args = append(args, "-sws_flags", tc.ScaleFlags)
			}
			args = append(args, "-f", "rawvideo", "-pixel_format", format, "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", rate.String(), "-i", "pipe:0", "-vf", filter, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", format, "pipe:1")
			cmd := exec.Command(ffmpeg, args...)
			var stderr bytes.Buffer
			cmd.Stdin = bytes.NewReader(raw)
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("native motion graph %v %s", err, stderr.String())
			}
			hash := sha256.Sum256(out)
			if hex.EncodeToString(hash[:]) != tc.Hash {
				t.Fatalf("native motion output changed: depth%d %s speed%g %dx%d frames%d %s", tc.Depth, tc.FPS, tc.Speed, tc.Width, tc.Height, tc.Frames, tc.Pattern)
			}
		}
	}
}

func TestMotionInterpolationValidation(t *testing.T) {
	for _, size := range [][2]int{{31, 64}, {64, 31}, {0, 32}} {
		if _, err := newMotionEstimator(size[0], size[1]); err == nil {
			t.Fatal("accepted small motion frame")
		}
	}
	motion, err := newMotionEstimator(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := motion.estimate(1, nil, nil, nil); err == nil {
		t.Fatal("accepted nonsequential motion input")
	}
	if err := motion.estimate(0, nil, nil, nil); err == nil {
		t.Fatal("accepted wrong luma dimensions")
	}
	input := motionInterpolationFixture(64, 64, 3, "static")
	if _, err := motion.interpolate(0, 512, input[0], input[1], false); err == nil {
		t.Fatal("accepted missing vector fields")
	}
	if _, err := motion.interpolate(0, 512, [3][]byte{}, input[1], false); err == nil {
		t.Fatal("accepted wrong output plane dimensions")
	}
	for f := 0; f < 2; f++ {
		if err := motion.estimate(f, input[max(0, f-1)][0], input[f][0], input[f+1][0]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := motion.interpolate(0, 0, input[0], input[1], false); err != nil {
		t.Fatal(err)
	}
	if got := medianMotion(motionVector{3, -3}, motionVector{1, 5}, motionVector{2, 1}); got != (motionVector{2, 1}) {
		t.Fatal(fmt.Sprint("invalid motion median ", got))
	}
	for _, alpha := range []int{-1, 1025} {
		if _, err := motion.interpolate(0, alpha, input[0], input[1], false); err == nil {
			t.Fatal("accepted invalid motion phase")
		}
	}
}

func TestMotionInterpolationCostCacheWrap(t *testing.T) {
	input := motionInterpolationFixture(64, 64, 4, "pan-x")
	first, err := newMotionEstimator(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := newMotionEstimator(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, motion := range []*motionEstimator{first, reference} {
		for frame := 0; frame < 2; frame++ {
			if err := motion.estimate(frame, input[max(0, frame-1)][0], input[frame][0], input[frame+1][0]); err != nil {
				t.Fatal(err)
			}
		}
	}
	first.stamp = ^uint32(0)
	for i := range first.visited {
		first.visited[i], first.costs[i] = 1, 0
	}
	for _, motion := range []*motionEstimator{first, reference} {
		if err := motion.estimate(2, input[1][0], input[2][0], input[3][0]); err != nil {
			t.Fatal(err)
		}
	}
	if first.stamp == 0 || !reflect.DeepEqual(first.fields[2], reference.fields[2]) {
		t.Fatal("motion cost memoization reused stale scores after stamp wrap")
	}
}

func motionImageFixture(t *testing.T, count int, pattern string) string {
	t.Helper()
	dir := t.TempDir()
	for f, planes := range motionInterpolationFixture(64, 64, count, pattern) {
		img := image.NewNRGBA64(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				i := y*64 + x
				img.SetNRGBA64(x, y, color.NRGBA64{uint16(planes[0][i]) * 257, uint16(planes[1][i]) * 257, uint16(planes[2][i]) * 257, 65535})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", f))
		if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"xmp:CreateDate": fmt.Sprintf("2026-10-21T12:00:%02dZ", f)})
	}
	return dir
}

func TestMotionInterpolationStreamingOwnership(t *testing.T) {
	for _, count := range []int{3, 8, 12} {
		for _, speed := range []float64{.0625, .3, .5, .7, 1, 2.3} {
			speed = float64(float32(speed))
			input := outputDeflickerSynthetic(count, 64, 64)
			before := make([][]byte, count)
			for f := range input {
				before[f] = bytes.Clone(input[f])
			}
			want, ids := interpolationReferenceRGB(input, 64, 64, count, 2, speed, "29.97")
			calls := make([]int, count)
			stream, err := newPlaybackFrameStream(64, 64, count, 0, 2, speed, frameRate{30000, 1001}, "29.97", func(i int) ([]byte, error) { calls[i]++; return input[i], nil })
			if err != nil {
				t.Fatal(err)
			}
			for f := range want {
				got, id, err := stream.frame(f)
				if err != nil || id != ids[f] || !bytes.Equal(got, want[f]) {
					t.Fatalf("motion count%d speed%g frame%d: %v", count, speed, f, err)
				}
				clear(got)
				if len(stream.cache) > max(3, stream.window) || (stream.motion != nil && len(stream.motion.fields) > 2) {
					t.Fatal("unbounded motion frame/vector cache")
				}
			}
			if err := stream.finish(); err != nil {
				t.Fatal(err)
			}
			for _, n := range calls {
				if n != 1 {
					t.Fatal("motion reread or skipped a source")
				}
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("motion modified source RGB")
			}
		}
	}
	metadata, err := newPlaybackFrameStream(0, 0, 3, 0, 2, .5, frameRate{24, 1}, "24", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := metadata.frame(0); err == nil {
		t.Fatal("metadata-only stream attempted pixel interpolation")
	}
	if _, err := newPlaybackFrameStream(16, 32, 3, 0, 2, .5, frameRate{24, 1}, "24", func(int) ([]byte, error) { t.Fatal("small interpolation read an input"); return nil, nil }); err == nil {
		t.Fatal("accepted undersized motion frame source")
	}
}

func TestMotionInterpolationOutputLayers(t *testing.T) {
	dir := motionImageFixture(t, 8, "accelerate")
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "base.mp4"), "--ffmpeg", executable)
	raw := denoiseRead(t, capture)
	var input [][]byte
	for i := 0; i < len(raw); i += 64 * 64 * 6 {
		input = append(input, raw[i:i+64*64*6])
	}
	filtered := outputDeflickerReferenceRGB(input, 64, 64, true, true)
	filtered = denoiseReferenceRGB(filtered, 64, 64, 2)
	filtered = airplaneReferenceRGB(filtered, 64, 64, 1)
	filtered, _ = interpolationReferenceRGB(filtered, 64, 64, len(input), 2, .5, "2")
	plan, err := planInterpolatedVideoFrames(len(input), .5, 2, 1, 2, frameRate{2, 1}, "2")
	if err != nil {
		t.Fatal(err)
	}
	fade, err := planVideoFade(plan.count(), frameRate{2, 1}, "2", 1)
	if err != nil {
		t.Fatal(err)
	}
	face, err := newTimestampOverlayFace(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	var expected []byte
	for f := plan.start; f < plan.end; f++ {
		rgb, err := weightedRGB48Frames([][]byte{filtered[f-1], filtered[f]}, lrtMotionBlurWeights(2))
		if err != nil {
			t.Fatal(err)
		}
		sharpenOutputRGB48(rgb, 64, 64)
		casOutputRGB48(rgb, 64, 64)
		img, err := imageFromRGB48(rgb, 64, 64)
		if err != nil {
			t.Fatal(err)
		}
		rgb, err = imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
		if err != nil {
			t.Fatal(err)
		}
		fadeRGB48(rgb, fade.gain(f-plan.start, frameRate{2, 1}))
		expected = append(expected, rgb...)
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "motion.mp4"), "--ffmpeg", executable, "--fps", "2", "--interpolation", "2", "--speed", ".5", "--output-deflicker-luma", "--output-deflicker-chroma", "--denoise", "2", "--remove-airplanes", "1", "--motion-blur", "2", "--sharpen", "--cas", "--watermark", "TEST", "--watermark-position", "top-left", "--fade-out", "1")
	if !bytes.Equal(expected, denoiseRead(t, capture)) {
		t.Fatal("motion output filter order or ownership differs")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("motion output changed sources")
	}
}

func TestMotionInterpolationJPEGAndFailureRetention(t *testing.T) {
	images := motionImageFixture(t, 8, "square")
	frames, err := DiscoverSequence(images)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var input [][]byte
	for f, frame := range frames {
		img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, frame.Image)))
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.jpg", f)), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(decoded)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, rgb)
	}
	before := batchFileSnapshot(t, dir)
	filtered, _ := interpolationReferenceRGB(input, 64, 64, len(input), 2, .5, "24")
	output := filepath.Join(t.TempDir(), "motion.avi")
	markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--interpolation", "2", "--speed", ".5")
	data := denoiseRead(t, output)
	pos := bytes.Index(data, []byte("movi")) + 4
	if pos < 4 {
		t.Fatal("AVI has no movie list")
	}
	for f, rgb := range filtered {
		img, err := imageFromRGB48(rgb, 64, 64)
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		if pos+8 > len(data) || string(data[pos:pos+4]) != "00dc" {
			t.Fatal("missing motion AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		if pos+size > len(data) || !bytes.Equal(encoded.Bytes(), data[pos:pos+size]) {
			t.Fatalf("motion AVI frame%d", f)
		}
		pos += size + size%2
	}
	if pos+4 <= len(data) && string(data[pos:pos+4]) == "00dc" {
		t.Fatal("extra motion AVI frame")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("motion changed JPEG inputs")
	}
	for _, fault := range []string{"small", "future"} {
		output := filepath.Join(t.TempDir(), "keep.avi")
		if err := os.WriteFile(output, []byte("original video"), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"render", images, "--output", output, "--overwrite", "--interpolation", "2", "--speed", ".5"}
		if fault == "small" {
			args = append(args, "--output-height", "16")
		} else if err := os.WriteFile(frames[len(frames)-1].Image, []byte("corrupt future"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if Main(args, &stdout, &stderr) == 0 {
			t.Fatal("ignored motion input failure", fault)
		}
		assertPreviewEVFileBytes(t, output, []byte("original video"))
		files, err := os.ReadDir(filepath.Dir(output))
		if err != nil || len(files) != 1 {
			t.Fatal("failed motion left a temporary video", err)
		}
	}
}

func TestMotionInterpolationActualVideo(t *testing.T) {
	verifyInterpolationActualVideo(t, motionImageFixture(t, 8, "diagonal"), 64, 64, 2)
}

func FuzzMotionInterpolation(f *testing.F) {
	f.Add([]byte{0, 1, 2}, uint16(512))
	f.Add([]byte{255, 0, 128, 64}, uint16(341))
	f.Add([]byte{17}, uint16(0))
	f.Fuzz(func(t *testing.T, seed []byte, phase uint16) {
		if len(seed) == 0 || len(seed) > 128 {
			return
		}
		width, height := 32+int(seed[0]%3)*16, 32+int(seed[len(seed)-1]%3)*16
		input := make([][3][]byte, 3)
		original := make([][3][]byte, 3)
		for frame := range input {
			for p := range input[frame] {
				input[frame][p] = make([]byte, width*height)
				for i := range input[frame][p] {
					input[frame][p][i] = seed[(i+frame*7+p*11)%len(seed)]
				}
				original[frame][p] = bytes.Clone(input[frame][p])
			}
		}
		motion, err := newMotionEstimator(width, height)
		if err != nil {
			t.Fatal(err)
		}
		for frame := 0; frame < 2; frame++ {
			if err := motion.estimate(frame, input[max(0, frame-1)][0], input[frame][0], input[frame+1][0]); err != nil {
				t.Fatal(err)
			}
		}
		for _, blocks := range motion.fields {
			for _, block := range blocks {
				for _, v := range block {
					if absInt(v.x) > 32 || absInt(v.y) > 32 {
						t.Fatal("motion escaped search bounds")
					}
				}
			}
		}
		out, err := motion.interpolate(0, int(phase%1025), input[0], input[1], false)
		if err != nil {
			t.Fatal(err)
		}
		for _, plane := range out {
			if len(plane) != width*height {
				t.Fatal("invalid motion output dimensions")
			}
			clear(plane)
		}
		if !reflect.DeepEqual(input, original) {
			t.Fatal("motion mutated input planes")
		}
	})
}

func TestMotionInterpolationOverlapReferenceCap(t *testing.T) {
	const width, height = 128, 128
	m, err := newMotionEstimator(width, height)
	if err != nil {
		t.Fatal(err)
	}
	for frame := 0; frame < 2; frame++ {
		m.fields[frame] = make([]motionBlock, m.columns*m.rows)
		for by := 0; by < m.rows; by++ {
			for bx := 0; bx < m.columns; bx++ {
				dx, dy := 28, 28
				if bx*16+8 > width/2 {
					dx = -28
				}
				if by*16+8 > height/2 {
					dy = -28
				}
				m.fields[frame][by*m.columns+bx] = motionBlock{{dx, dy}, {dx, dy}}
			}
		}
	}
	input := motionInterpolationFixture(width, height, 2, "noise")
	capReached := false
	for _, alpha := range []int{1, 341, 512, 683, 1023} {
		got, err := m.interpolate(0, alpha, input[0], input[1], false)
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				var sums [3]uint64
				weightSum, pairs := uint64(0), 0
				for direction := 0; direction < 2; direction++ {
					for by := 0; by < m.rows; by++ {
						for bx := 0; bx < m.columns; bx++ {
							v := m.fields[1-direction][by*m.columns+bx][direction]
							fraction := 1024 - alpha
							if direction == 1 {
								fraction = alpha
							}
							sx, sy := bx*16-8+v.x*fraction/1024, by*16-8+v.y*fraction/1024
							if x < max(0, min(width-1, sx)) || x >= max(0, min(width-1, sx+32)) || y < max(0, min(height-1, sy)) || y >= max(0, min(height-1, sy+32)) {
								continue
							}
							fx := 1 - math.Abs(float64(2*(x-sx)+1-32)/32)
							fy := 1 - math.Abs(float64(2*(y-sy)+1-32)/32)
							weight := uint64(math.Round(fx*fy*64)) * 4
							if weight == 0 {
								continue
							}
							if pairs == 16 {
								capReached = true
								continue
							}
							pairs++
							if direction == 1 {
								v.x, v.y = -v.x, -v.y
							}
							ax, ay := max(0, min(width-1, x+v.x*alpha/1024)), max(0, min(height-1, y+v.y*alpha/1024))
							bx1, by1 := max(0, min(width-1, x-v.x*(1024-alpha)/1024)), max(0, min(height-1, y-v.y*(1024-alpha)/1024))
							for p := range sums {
								sums[p] += weight * uint64((1024-alpha)*int(input[0][p][ay*width+ax])+alpha*int(input[1][p][by1*width+bx1]))
							}
							weightSum += weight * 1024
						}
					}
				}
				for p := range sums {
					want := byte(((1024-alpha)*int(input[0][p][y*width+x]) + alpha*int(input[1][p][y*width+x]) + 512) / 1024)
					if weightSum > 0 {
						want = byte((sums[p] + weightSum/2) / weightSum)
					}
					if got[p][y*width+x] != want {
						t.Fatalf("overlap phase%d p%d xy%d,%d got%d want%d", alpha, p, x, y, got[p][y*width+x], want)
					}
				}
			}
		}
	}
	if !capReached {
		t.Fatal("overlap fixture did not exercise the 32-reference limit")
	}
}
