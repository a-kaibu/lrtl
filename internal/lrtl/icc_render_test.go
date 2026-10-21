package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestICCRenderResolution(t *testing.T) {
	for _, label := range []string{"", "sRGB2014", "ITU-R BT.2020 Reference Display", "ITU-R BT.2020 PQ", "2020 pq", "2020 PQ\r\n"} {
		t.Run(label, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "first.png")
			if err := os.WriteFile(path, testICCPNG(testICCProfile("mluc", label)), 0600); err != nil {
				t.Fatal(err)
			}
			frames := []Frame{{Image: path}, {Image: "second-must-not-be-read"}}
			for _, autoPQ := range []bool{false, true} {
				got, err := resolveRenderICC(frames, RenderOptions{DetectDynamicRange: autoPQ, DynamicRange: "sdr"}, true)
				wantInput, wantRange := "bt709", "sdr"
				if strings.Contains(label, "2020") {
					wantInput = "bt2020"
				}
				if autoPQ && strings.HasSuffix(strings.TrimSpace(label), "PQ") {
					wantRange = "pq"
				}
				if err != nil || got.InputColor != wantInput || got.DynamicRange != wantRange {
					t.Fatalf("got %+v/%v; want %s/%s", got, err, wantInput, wantRange)
				}
			}
		})
	}
	frames := []Frame{{Image: "must-not-be-opened"}}
	for _, options := range []RenderOptions{{InputColor: "bt709"}, {InputColor: "bt2020"}, {RawDevelop: true}, {RawPreview: true}, {LRTPreview: "visual"}} {
		if _, err := resolveRenderICC(frames, options, true); err != nil {
			t.Fatal("explicit/derived input was inspected", err)
		}
	}
	if _, err := resolveRenderICC(frames, RenderOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRenderICC(frames, RenderOptions{}, true); err == nil {
		t.Fatal("missing auto source accepted")
	}
}

func TestICCRenderCLI(t *testing.T) {
	dir := nativePlaybackFixture(t, 3)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := frames[0].Image
	imageData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	profilePNG := testICCPNG(testICCProfile("desc", "ITU-R BT.2020 PQ"))
	// Valid image data stays intact; insert iCCP immediately after IHDR.
	data := append([]byte{}, imageData[:33]...)
	data = append(data, profilePNG[8:len(profilePNG)-12]...)
	data = append(data, imageData[33:]...)
	if err := os.WriteFile(first, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	t.Setenv("LRTL_TEST_FFMPEG_FORMATS", "yuv420p yuv420p10le yuv422p10le yuv444p10le")
	preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h265", FrameRate: "24", OutputResV: 32, Speed: 1, Gamut: 1, DynamicRange: 0})
	for _, mode := range []string{"auto", "sdr", "bt709", "preset", "preset-sdr", "filtered"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "out.mp4"), "--ffmpeg", fake, "--output-color", "lrt-7.5.5"}
			if mode == "bt709" {
				args = append(args, "--input-color", "bt709")
			} else if mode != "filtered" {
				args = append(args, "--output-gamut", "bt2020")
			}
			if strings.HasPrefix(mode, "preset") {
				args = append(args, "--lrt-render-preset", preset)
			}
			if mode == "filtered" {
				args = append(args, "--filename-contains", filepath.Base(frames[1].Image))
			}
			if mode == "sdr" || mode == "preset-sdr" {
				args = append(args, "--dynamic-range", "sdr")
			}
			markerCommand(t, args...)
			raw, err := os.ReadFile(capture + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var flags []string
			if err := json.Unmarshal(raw, &flags); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(flags, " ")
			if (mode == "auto" || mode == "preset") != strings.Contains(joined, "smpte2084") {
				t.Fatal("incorrect PQ inference", joined)
			}
			if (mode != "bt709" && mode != "filtered") != strings.Contains(joined, "color_primaries=bt2020") {
				t.Fatal("incorrect source inference", joined)
			}
		})
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", fake, "--lrt-render-preset", preset)
	batchArgs, err := os.ReadFile(capture + ".json")
	if err != nil || !bytes.Contains(batchArgs, []byte("smpte2084")) {
		t.Fatalf("batch lost automatic PQ detection: %s/%v", batchArgs, err)
	}
	// A damaged embedded profile fails before replacing an existing video.
	bad := bytes.Clone(data)
	bad[45] ^= 1
	if err := os.WriteFile(first, bad, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(out, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", out, "--overwrite", "--output-color", "lrt-7.5.5", "--ffmpeg", fake}, &stdout, &stderr) == 0 {
		t.Fatal("accepted broken ICC")
	}
	assertPreviewEVFileBytes(t, out, []byte("original"))
	if err := os.WriteFile(first, data, 0600); err != nil {
		t.Fatal(err)
	}
	for path, contents := range before {
		assertPreviewEVFileBytes(t, path, contents)
	}
}
