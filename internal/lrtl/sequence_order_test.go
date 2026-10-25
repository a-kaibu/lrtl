package lrtl

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSequenceOrderInstalledComparators(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR to replay genuine filename/date comparators")
	}
	dir := t.TempDir()
	source, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes", "SequenceOrderOracle.java"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, source).CombinedOutput(); err != nil {
		t.Fatalf("compile %v %s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.SequenceOrderOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native %v %s", err, out)
	}
	var expected, actual any
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-sequence-order-7.5.5.json"), &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(denoiseRead(t, path), &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("genuine sequence order observations changed")
	}
}

func sequenceOrderImageFixture(t *testing.T) (string, []Frame) {
	t.Helper()
	frames := cropFixture(t, 3)
	names := []string{"frame2.png", "frame02.png", "frame10.png"}
	dates := []string{"2026-01-01T02:00:00+09:00", "2026-01-01T01:00:00Z", "2026-01-01T01:00:00+01:00"}
	exif := []string{"2026-01-01T00:00:02Z", "2026-01-01T00:00:03Z", "2026-01-01T00:00:01Z"}
	dir := t.TempDir()
	for i, frame := range frames {
		image := filepath.Join(dir, names[i])
		if err := copyFile(frame.Image, image); err != nil {
			t.Fatal(err)
		}
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("xmp:CreateDate", dates[i]); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("exif:DateTimeOriginal", exif[i]); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:Exposure2012", []string{"-1", "0", "1"}[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(image+".xmp", packet, false); err != nil {
			t.Fatal(err)
		}
		frames[i] = Frame{Image: image, Sidecar: image + ".xmp"}
	}
	// Indices identify the original discovery order even when playback changes.
	for i, index := range []int{1, 0, 2} {
		frames[index].Index = i
	}
	return dir, frames
}

func TestSequenceOrderCLIImagesVideoAndSubtitles(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		order []int
	}{
		{"name", []int{1, 0, 2}}, {"lrt-name", []int{0, 1, 2}}, {"lrt-ascii", []int{1, 2, 0}},
		{"lrt-date", []int{2, 1, 0}}, {"capture-time", []int{2, 0, 1}}, {"lrt-settings", []int{2, 1, 0}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir, frames := sequenceOrderImageFixture(t)
			if tc.mode == "lrt-settings" {
				writeLRTSequenceOrderSettings(t, dir, nativeLRTSequenceSortSetting(t, false))
			}
			before := renameTreeSnapshot(t, dir)
			ordered := make([]Frame, len(frames))
			for i, index := range tc.order {
				ordered[i] = frames[index]
			}
			var stdout, stderr bytes.Buffer
			run := func(args ...string) {
				t.Helper()
				if Main(args, &stdout, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			options := RenderOptions{FPS: 24, Quality: 90, ApplyXMPBasic: true, AutoOrient: true}
			actual, expected := filepath.Join(t.TempDir(), "actual.avi"), filepath.Join(t.TempDir(), "expected.avi")
			run("render", dir, "--sort", tc.mode, "--output", actual, "--apply-xmp-basic")
			if err := RenderSequence(ordered, expected, options); err != nil {
				t.Fatal(err)
			}
			payload := func(path string) []byte {
				data := denoiseRead(t, path)
				offset := bytes.Index(data, []byte("movi"))
				if offset < 0 {
					t.Fatal("missing AVI frames")
				}
				return data[offset:]
			}
			if !bytes.Equal(payload(actual), payload(expected)) {
				t.Fatal("sorted AVI differs from independent manual permutation")
			}
			for _, command := range []string{"export", "preview-images"} {
				a, b := filepath.Join(t.TempDir(), "actual"), filepath.Join(t.TempDir(), "expected")
				run(command, dir, "--sort", tc.mode, "--output", a, "--apply-xmp-basic")
				if command == "export" {
					if err := ExportTIFFSequence(ordered, b, options); err != nil {
						t.Fatal(err)
					}
				} else {
					previewOptions := options
					previewOptions.LongEdge = 1280
					previewOptions.ApplyXMPCrop = true
					if _, err := GeneratePreviewImages(ordered, b, previewOptions); err != nil {
						t.Fatal(err)
					}
				}
				// Manifest correspondence and encoded pixels both follow the permutation.
				relativeSnapshot := func(root string) map[string][]byte {
					result := map[string][]byte{}
					for path, data := range batchFileSnapshot(t, root) {
						rel, err := filepath.Rel(root, path)
						if err != nil {
							t.Fatal(err)
						}
						result[rel] = data
					}
					return result
				}
				if !reflect.DeepEqual(relativeSnapshot(a), relativeSnapshot(b)) {
					t.Fatal("sorted export differs", command)
				}
			}
			a, b := filepath.Join(t.TempDir(), "actual.srt"), filepath.Join(t.TempDir(), "expected.srt")
			run("subtitles", dir, "--sort", tc.mode, "--output", a)
			if err := writeSequenceSubtitles(ordered, b, 24, defaultTimestampFormat, false); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(denoiseRead(t, a), denoiseRead(t, b)) {
				t.Fatal("subtitle order differs from video")
			}
			if !reflect.DeepEqual(before, renameTreeSnapshot(t, dir)) {
				t.Fatal("sorting modified source")
			}
		})
	}
}

func writeLRTSequenceOrderSettings(t *testing.T, dir string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func nativeLRTSequenceSortSetting(t *testing.T, filenameSort bool) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-deflicker-settings-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		t.Fatalf("read installed LRT settings fixture: rows=%d err=%v", len(rows), err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(rows[0], &settings); err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(filenameSort)
	if err != nil {
		t.Fatal(err)
	}
	settings["filenameSort"] = value
	result, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSequenceOrderNativeFilenameSortSetting(t *testing.T) {
	_, frames := sequenceOrderImageFixture(t)
	for _, tc := range []struct {
		filenameSort bool
		want         []int
	}{
		{true, []int{0, 1, 2}},
		{false, []int{2, 1, 0}},
	} {
		writeLRTSequenceOrderSettings(t, filepath.Dir(frames[0].Image), nativeLRTSequenceSortSetting(t, tc.filenameSort))
		got, err := orderSequenceFrames(frames, "lrt-settings")
		if err != nil {
			t.Fatal(err)
		}
		for i, sourceIndex := range tc.want {
			if got[i] != frames[sourceIndex] {
				t.Fatalf("filenameSort=%v: frame %d got %s want %s", tc.filenameSort, i, got[i].Image, frames[sourceIndex].Image)
			}
		}
	}
}

func TestSequenceOrderNativeSettingsRejectsMissingOrMalformedValues(t *testing.T) {
	dir, frames := sequenceOrderImageFixture(t)
	for _, data := range [][]byte{[]byte("{"), []byte(`{"filenameSort":"false"}`), []byte(`{"imageCount":3}`)} {
		writeLRTSequenceOrderSettings(t, dir, data)
		if _, err := orderSequenceFrames(frames, "lrt-settings"); err == nil {
			t.Fatalf("accepted invalid LRT settings %s", data)
		}
	}
}

func TestSequenceOrderNativeComparators(t *testing.T) {
	var reference struct {
		Pattern string
		Pairs   []struct {
			Left, Right string
			Natural     *int
			ASCII       int
			Error       *string
		}
		DateCases []struct {
			Names, Dates []string
			Order        []int
		} `json:"date_cases"`
	}
	data, err := os.ReadFile("testdata/lrt-sequence-order-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Pairs) != 64*64 || len(reference.DateCases) != 8 || reference.Pattern != `(\d+|\D+)` {
		t.Fatal("native fixture incomplete", len(reference.Pairs))
	}
	for _, tc := range reference.Pairs {
		order, err := compareLRTFilenameKeys(lrtFilenameKeyFor(tc.Left), lrtFilenameKeyFor(tc.Right))
		if tc.Error != nil {
			if err == nil {
				t.Fatal("missing native numeric error", tc.Left, tc.Right)
			}
		} else if err != nil || order != *tc.Natural {
			t.Fatalf("native natural %q/%q: %d %v, want %v", tc.Left, tc.Right, order, err, tc.Natural)
		}
		got := strings.Compare(javaOrderKey(tc.Left), javaOrderKey(tc.Right))
		if got != tc.ASCII {
			t.Fatalf("native ASCII %q/%q: %d != %d", tc.Left, tc.Right, got, tc.ASCII)
		}
	}
	for _, tc := range reference.DateCases {
		got, err := lrtReferenceDateOrder(tc.Names, tc.Dates)
		if err != nil || !reflect.DeepEqual(got, tc.Order) {
			t.Fatal("native date order", tc, got, err)
		}
	}
}

func TestSequenceOrderIdentityAndErrors(t *testing.T) {
	frames := []Frame{{Index: 8, Image: "a10.png", Sidecar: "ten.xmp"}, {Index: 2, Image: "a2.png", Sidecar: "two.xmp"}, {Index: 5, Image: "a02.png", Sidecar: "zero-two.xmp"}}
	before := append([]Frame(nil), frames...)
	for _, tc := range []struct {
		mode    string
		indices []int
	}{{"name", []int{5, 2, 8}}, {"lrt-name", []int{2, 5, 8}}, {"lrt-ascii", []int{5, 8, 2}}} {
		got, err := orderSequenceFrames(frames, tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		var indices []int
		for _, frame := range got {
			indices = append(indices, frame.Index)
		}
		if !reflect.DeepEqual(indices, tc.indices) {
			t.Fatal(tc.mode, indices)
		}
	}
	if !reflect.DeepEqual(before, frames) {
		t.Fatal("changed input slice")
	}
	if _, err := orderSequenceFrames(frames, "unsupported"); err == nil {
		t.Fatal("accepted bad mode")
	}
	if _, err := orderSequenceFrames([]Frame{{Image: "a9223372036854775808.png"}, {Image: "a2.png"}}, "lrt-name"); err == nil {
		t.Fatal("accepted numeric overflow")
	}
	if _, err := orderSequenceFrames([]Frame{{Image: "bad\xff.png"}}, "lrt-ascii"); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestSequenceOrderRootQualifiedDates(t *testing.T) {
	dir, frames := sequenceOrderImageFixture(t)
	bodies := []string{`<xmp:CreateDate xml:lang="en">z</xmp:CreateDate>`, `<xmp:CreateDate rdf:resource="a"/>`, `<xmp:CreateDate rdf:parseType="Resource"><rdf:value>m</rdf:value><f:Unit>day</f:Unit></xmp:CreateDate>`}
	for i, frame := range frames {
		text := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:f="urn:fixture">` + bodies[i] + `<f:Nested rdf:parseType="Resource"><xmp:CreateDate>nested-only</xmp:CreateDate></f:Nested></rdf:Description></rdf:RDF>`
		if err := os.WriteFile(frame.Sidecar, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := orderSequenceFrames(frames, "lrt-date")
	if err != nil {
		t.Fatal(err)
	}
	for i, index := range []int{1, 2, 0} {
		if got[i] != frames[index] {
			t.Fatal("qualified global dates not used", got)
		}
	}
	for _, frame := range frames {
		text := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:f="urn:fixture"><f:Nested rdf:parseType="Resource"><xmp:CreateDate>nested-only</xmp:CreateDate></f:Nested></rdf:Description></rdf:RDF>`
		if err := os.WriteFile(frame.Sidecar, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err = orderSequenceFrames(frames, "lrt-date")
	if err != nil {
		t.Fatal(err)
	}
	for i, index := range []int{1, 2, 0} {
		if got[i] != frames[index] {
			t.Fatal("nested dates affected filename fallback", got)
		}
	}
	if len(renameTreeSnapshot(t, dir)) != 7 {
		t.Fatal("unexpected date-sort side effects")
	}
}

func TestSequenceOrderErrorsBeforeOutput(t *testing.T) {
	for _, kind := range []string{"unknown", "partial-dates", "invalid-capture", "overflow-name", "missing-native-settings"} {
		t.Run(kind, func(t *testing.T) {
			dir, frames := sequenceOrderImageFixture(t)
			mode := "lrt-date"
			switch kind {
			case "unknown":
				mode = "unknown"
			case "partial-dates":
				p, err := PacketForFrame(frames[0])
				if err != nil {
					t.Fatal(err)
				}
				if err := p.RemoveProperty("xmp:CreateDate"); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(frames[0].Sidecar, p, false); err != nil {
					t.Fatal(err)
				}
			case "invalid-capture":
				mode = "capture-time"
				p, err := PacketForFrame(frames[0])
				if err != nil {
					t.Fatal(err)
				}
				if err := p.SetScalar("exif:DateTimeOriginal", "invalid"); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(frames[0].Sidecar, p, false); err != nil {
					t.Fatal(err)
				}
			case "overflow-name":
				mode = "lrt-name"
				companionWrite(t, dir, "frame9223372036854775808.png", []byte("opaque"))
			case "missing-native-settings":
				mode = "lrt-settings"
			}
			before := renameTreeSnapshot(t, dir)
			for _, command := range []string{"render", "export", "preview-images", "subtitles"} {
				output := filepath.Join(t.TempDir(), "output")
				if command == "render" {
					output += ".avi"
				}
				if command == "subtitles" {
					output += ".srt"
				}
				var out, stderr bytes.Buffer
				if Main([]string{command, dir, "--sort", mode, "--output", output}, &out, &stderr) == 0 {
					t.Fatal("missing sort error", command, kind)
				}
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("sort error created output", command, err)
				}
			}
			if !reflect.DeepEqual(before, renameTreeSnapshot(t, dir)) {
				t.Fatal("sort error changed source")
			}
		})
	}
}

func TestSequenceOrderFilterBeforeSortAndSubtitleSelection(t *testing.T) {
	dir, frames := sequenceOrderImageFixture(t)
	if err := os.WriteFile(frames[1].Sidecar, []byte("<broken>"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	for _, command := range []string{"render", "export", "preview-images", "subtitles"} {
		output := filepath.Join(t.TempDir(), "output")
		if command == "render" {
			output += ".avi"
		}
		if command == "subtitles" {
			output += ".srt"
		}
		if Main([]string{command, dir, "--filename-contains", "frame2.png", "--sort", "lrt-date", "--output", output}, &out, &stderr) != 0 {
			t.Fatal(command, &stderr)
		}
	}
	// Selection JSON also precedes date loading, as with the video command.
	selection := filepath.Join(t.TempDir(), "selection.json")
	if Main([]string{"select", dir, "--filename-contains", "frame2.png", "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	output := filepath.Join(t.TempDir(), "selected.srt")
	if Main([]string{"subtitles", dir, "--selection", selection, "--sort", "capture-time", "--output", output}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	text := string(denoiseRead(t, output))
	if strings.Count(text, " --> ") != 1 || !strings.Contains(text, "00:00:02") {
		t.Fatal("subtitle selection/order mismatch", text)
	}
}

func FuzzSequenceOrderComparator(f *testing.F) {
	for _, pair := range [][2]string{{"frame01.png", "frame1.png"}, {"a_2", "a@2"}, {"١٢", "٢"}, {"9223372036854775808", "2"}, {"𐐀", "𐐨"}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 2048 || len(b) > 2048 {
			return
		}
		left, right := lrtFilenameKeyFor(a), lrtFilenameKeyFor(b)
		ab, errAB := compareLRTFilenameKeys(left, right)
		ba, errBA := compareLRTFilenameKeys(right, left)
		if (errAB == nil) != (errBA == nil) || errAB == nil && ab != -ba {
			t.Fatal("comparator asymmetric", ab, ba, errAB, errBA)
		}
		if self, err := compareLRTFilenameKeys(left, left); err == nil && self != 0 {
			t.Fatal("non-reflexive comparator", self)
		}
	})
}

func TestSequenceOrderBatchAndPreviewWriteback(t *testing.T) {
	dir, frames := sequenceOrderImageFixture(t)
	writeLRTSequenceOrderSettings(t, dir, nativeLRTSequenceSortSetting(t, false))
	ordered := []Frame{frames[2], frames[1], frames[0]}
	var out, stderr bytes.Buffer
	batch := filepath.Join(t.TempDir(), "videos")
	if Main([]string{"batch-render", dir, "--output", batch, "--format", "avi", "--", "--sort", "lrt-settings", "--apply-xmp-basic"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	expected := filepath.Join(t.TempDir(), "expected.avi")
	if err := RenderSequence(ordered, expected, RenderOptions{FPS: 24, Quality: 90, ApplyXMPBasic: true, AutoOrient: true}); err != nil {
		t.Fatal(err)
	}
	actualAVI := denoiseRead(t, filepath.Join(batch, filepath.Base(dir)+".avi"))
	expectedAVI := denoiseRead(t, expected)
	actualMOVI, expectedMOVI := bytes.Index(actualAVI, []byte("movi")), bytes.Index(expectedAVI, []byte("movi"))
	if actualMOVI < 0 || expectedMOVI < 0 || !bytes.Equal(actualAVI[actualMOVI:], expectedAVI[expectedMOVI:]) {
		t.Fatal("batch video ignored sort order")
	}
	previewRoot := filepath.Join(t.TempDir(), "previews")
	if Main([]string{"batch-preview-images", dir, "--output", previewRoot, "--", "--sort", "lrt-settings"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	batchManifest := readBatchPreviewManifest(t, previewRoot)
	manifest := readPreviewManifest(t, filepath.Join(previewRoot, batchManifest.Sequences[0].Directory))
	for i, frame := range manifest.Frames {
		if frame.Source != filepath.Base(ordered[i].Image) || frame.SourceIndex != ordered[i].Index {
			t.Fatal("batch preview source correspondence changed", manifest)
		}
	}
	preview := filepath.Join(t.TempDir(), "writeback")
	if Main([]string{"preview-images", dir, "--output", preview, "--sort", "lrt-settings", "--write-preview-ev"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	manifest = readPreviewManifest(t, preview)
	for i, row := range manifest.Frames {
		packet, err := PacketForFrame(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		text, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
		if err != nil || !found {
			t.Fatal(text, found, err)
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.Abs(number-row.ExternalExposureDefault) > 1e-6 {
			t.Fatal("sorted measurement saved to wrong original", row, text, err)
		}
		backups, err := filepath.Glob(ordered[i].Sidecar + ".lrtl-*.bak")
		if err != nil || len(backups) != 1 {
			t.Fatal("missing original XMP backup", backups, err)
		}
	}
}

func TestSequenceOrderCaptureOffsetsAndStableTies(t *testing.T) {
	_, frames := sequenceOrderImageFixture(t)
	for i, date := range []string{"2026-01-01T02:00:00+09:00", "2026-01-01T01:00:00Z", "2025-12-31T17:00:00Z"} {
		packet, err := PacketForFrame(frames[i])
		if err != nil {
			t.Fatal(err)
		}
		if err := setCaptureTimestampScalar(packet, "exif:DateTimeOriginal", date); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frames[i].Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	input := []Frame{frames[2], frames[0], frames[1]}
	got, err := orderSequenceFrames(input, "capture-time")
	if err != nil || !reflect.DeepEqual(got, input) {
		t.Fatal("offset normalization/stable equal timestamps", got, err)
	}
}
