package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedCaptureTimeJPEG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.jpg")
	xml := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:exif="http://ns.adobe.com/exif/1.0/" exif:DateTimeOriginal="2026-10-24T03:04:05.123+09:00"/></rdf:RDF></x:xmpmeta>`)
	writeEmbeddedTransitionJPEG(t, path, xml)
	got, err := readFrameCaptureTime(Frame{Image: path})
	want, _ := time.Parse(time.RFC3339Nano, "2026-10-24T03:04:05.123+09:00")
	if err != nil || !got.Equal(want) {
		t.Fatalf("embedded timestamp: got %s, err %v", got, err)
	}
}

func embeddedCaptureObservations(t testing.TB) xmpSourceReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-embedded-capture-time-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference xmpSourceReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 45 {
		t.Fatal("invalid embedded capture-date reference")
	}
	return reference
}

func TestEmbeddedCaptureTimeNativePackets(t *testing.T) {
	wants := map[string]string{
		"alias-attribute": "2025-01-02T03:04:05Z", "alias-foreign-prefix": "2025-01-02T03:04:05Z", "alias-qualifier": "2025-01-02T03:04:05Z", "alias-nested": "2027-01-02T03:04:05Z",
		"original": "2026-10-24T03:04:05.123+09:00", "all": "2026-10-24T03:04:05.123+09:00",
		"create": "2027-01-02T03:04:05Z", "nested-create": "2027-01-02T03:04:05Z",
		"modify": "2028-01-02T03:04:05Z", "digitized-only": "2025-01-02T03:04:05Z", "colon-original": "2026-10-24T03:04:05Z",
		"space-original": "2026-10-24T03:04:05Z", "fraction-no-zone": "2026-10-24T03:04:05.123Z",
	}
	for _, row := range embeddedCaptureObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			before := batchFileSnapshot(t, filepath.Dir(frame.Image))
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			if row.Result == nil {
				t.Fatal("native file reader lost metadata")
			}
			assertPastedMetadata(t, packet, *row.Result)
			got, found, err := captureTimeFromXMP(frame)
			name := strings.SplitN(row.Name, "-", 2)[1]
			if name == "invalid-original" {
				if err == nil {
					t.Fatal("invalid preferred timestamp accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			value := wants[name]
			if value == "" {
				if found {
					t.Fatal("used nested or digitization-only timestamp")
				}
			} else {
				want, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					t.Fatal(err)
				}
				if !found || !got.Equal(want) {
					t.Fatalf("got %s, found %t, want %s", got, found, want)
				}
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, filepath.Dir(frame.Image))) {
				t.Fatal("timestamp read modified inputs")
			}
		})
	}
}

func TestEmbeddedCaptureTimeInstalledReader(t *testing.T) {
	replayInstalledXMPReader(t, embeddedCaptureObservations(t))
}

func TestEmbeddedCaptureTimeLongTermClock(t *testing.T) {
	for _, row := range embeddedCaptureObservations(t).Cases {
		if row.Name != "jpg-nested-only" && row.Name != "jpg-nested-create" && row.Name != "jpg-alias-qualifier" && row.Name != "jpg-invalid-original" {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			stamp, available, valid, err := longTermCaptureClock(frame)
			if err != nil {
				t.Fatal(err)
			}
			switch row.Name {
			case "jpg-nested-only":
				if available {
					t.Fatal("used nested preset date")
				}
			case "jpg-invalid-original":
				if !available || valid {
					t.Fatal("changed native invalid-date filter semantics")
				}
			default:
				want := 2027
				if row.Name == "jpg-alias-qualifier" {
					want = 2025
				}
				if !available || !valid || stamp.Year() != want {
					t.Fatalf("clock=%s available=%t valid=%t", stamp, available, valid)
				}
			}
		})
	}
}

func TestEmbeddedCaptureTimeShiftPreservesMetadata(t *testing.T) {
	for _, row := range embeddedCaptureObservations(t).Cases {
		if !strings.HasSuffix(row.Name, "-all") && !strings.HasSuffix(row.Name, "-alias-qualifier") && !strings.HasSuffix(row.Name, "-alias-nested") {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			dir := t.TempDir()
			frame := writeXMPSourceObservation(t, dir, row)
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"timeshift", dir, "--offset", "1h"}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			packet, err := PacketForSidecar(frame.OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			wanted := strings.NewReplacer("2026-10-24T03:04:05.123+09:00", "2026-10-24T04:04:05.123+09:00", "2027-01-02T03:04:05Z", "2027-01-02T04:04:05Z", "2025-01-02T03:04:05Z", "2025-01-02T04:04:05Z").Replace(*row.Result)
			assertPastedMetadata(t, packet, wanted)
			first := denoiseRead(t, frame.OutputSidecar())
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			backups, err := filepath.Glob(frame.OutputSidecar() + ".*.bak")
			if err != nil || len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), first) {
				t.Fatal("missing exact previous sidecar backup", err)
			}
			if !bytes.Equal(denoiseRead(t, frame.Image), row.Data) {
				t.Fatal("shift modified original image")
			}
		})
	}
}

func TestEmbeddedCaptureTimeImportSubtitlesAndVideo(t *testing.T) {
	var row xmpSourceObservation
	for _, r := range embeddedCaptureObservations(t).Cases {
		if r.Name == "jpg-original" {
			row = r
		}
	}
	source, control := t.TempDir(), t.TempDir()
	old := "2026-10-24T03:04:05.123+09:00"
	seconds := []int{20, 0, 10}
	for i, second := range seconds {
		stamp := fmt.Sprintf("2026-10-24T03:04:%02d.123+09:00", second)
		data := bytes.ReplaceAll(row.Data, []byte(old), []byte(stamp))
		for _, dir := range []string{source, control} {
			image := filepath.Join(dir, fmt.Sprintf("frame-%02d.jpg", i))
			if err := os.WriteFile(image, data, 0600); err != nil {
				t.Fatal(err)
			}
			if dir == control {
				if err := os.WriteFile(image+".xmp", []byte(strings.ReplaceAll(*row.Result, old, stamp)), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before := batchFileSnapshot(t, source)
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		if Main(args, &out, &stderr) != 0 {
			t.Fatalf("%v: %s", args, stderr.String())
		}
	}
	for _, profile := range []string{"standard", "lrt-7.5.5"} {
		left, right := filepath.Join(t.TempDir(), "embedded.srt"), filepath.Join(t.TempDir(), "external.srt")
		for _, pair := range [][2]string{{source, left}, {control, right}} {
			args := []string{"subtitles", pair[0], "--output", pair[1], "--profile", profile, "--fps", "24"}
			if profile == "lrt-7.5.5" {
				args = append(args, "--every", "1")
			}
			run(args...)
		}
		if !bytes.Equal(denoiseRead(t, left), denoiseRead(t, right)) {
			t.Fatal("embedded subtitle differs from external metadata", profile)
		}
	}
	videoA, videoB := filepath.Join(t.TempDir(), "embedded.avi"), filepath.Join(t.TempDir(), "external.avi")
	for _, pair := range [][2]string{{source, videoA}, {control, videoB}} {
		run("render", pair[0], "--output", pair[1], "--timestamp-overlay", "--timestamp-format", "15:04:05", "--apply-xmp-basic")
	}
	a, b := denoiseRead(t, videoA), denoiseRead(t, videoB)
	i, j := bytes.Index(a, []byte("movi")), bytes.Index(b, []byte("movi"))
	if i < 0 || j < 0 || !bytes.Equal(a[i:], b[j:]) {
		t.Fatal("embedded timestamps changed AVI frames/index")
	}
	output := filepath.Join(t.TempDir(), "imported")
	plan, err := planImport(source, output, ImportOptions{SortByCapture: true, RenameByDate: true})
	if err != nil {
		t.Fatal(err)
	}
	for i, index := range []int{1, 2, 0} {
		if filepath.Base(plan[i].source) != fmt.Sprintf("frame-%02d.jpg", index) {
			t.Fatal("wrong capture ordering")
		}
	}
	run("import", source, "--output", output, "--sort", "capture-time", "--rename-by-date")
	frames, err := DiscoverSequence(output)
	if err != nil || len(frames) != 3 {
		t.Fatal("missing imported sequence", err)
	}
	for i, index := range []int{1, 2, 0} {
		if !bytes.Equal(denoiseRead(t, frames[i].Image), before[filepath.Join(source, fmt.Sprintf("frame-%02d.jpg", index))]) {
			t.Fatal("import changed image or order")
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, source)) {
		t.Fatal("read/import/render modified source")
	}
}

func TestEmbeddedCaptureTimeFailureKeepsOutputs(t *testing.T) {
	dir := t.TempDir()
	for _, row := range embeddedCaptureObservations(t).Cases {
		name := ""
		if row.Name == "jpg-original" {
			name = "frame-00.jpg"
		}
		if row.Name == "jpg-invalid-original" {
			name = "frame-01.jpg"
		}
		if name != "" {
			if err := os.WriteFile(filepath.Join(dir, name), row.Data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := batchFileSnapshot(t, dir)
	for _, command := range []string{"render", "subtitles"} {
		ext := ".avi"
		if command == "subtitles" {
			ext = ".srt"
		}
		output := filepath.Join(t.TempDir(), "existing"+ext)
		if err := os.WriteFile(output, []byte("keep existing output"), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{command, dir, "--output", output, "--overwrite"}
		if command == "render" {
			args = append(args, "--timestamp-overlay")
		}
		var out, stderr bytes.Buffer
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("accepted invalid late capture time", command)
		}
		if string(denoiseRead(t, output)) != "keep existing output" {
			t.Fatal("failed command replaced output", command)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed command changed sources")
	}
}

func FuzzEmbeddedCaptureTime(f *testing.F) {
	for _, row := range embeddedCaptureObservations(f).Cases {
		f.Add([]byte(*row.Result))
	}
	f.Add([]byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:e="http://ns.adobe.com/exif/1.0/" e:DateTimeDigitized="2026-01-02T03:04:05Z"/></rdf:RDF>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		packet, err := parseXMP(data)
		if err != nil {
			return
		}
		serialized, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		again, err := parseXMP(serialized)
		if err != nil {
			t.Fatal(err)
		}
		for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate"} {
			value, found, err := captureTimestampScalar(packet, property)
			if err != nil {
				continue
			}
			next, exists, err := captureTimestampScalar(again, property)
			if err != nil || next != value || exists != found {
				t.Fatal("capture alias normalization is not stable", property)
			}
		}
	})
}

func TestEmbeddedCaptureTimeSidecarPriorityAndPreflight(t *testing.T) {
	var good, bad xmpSourceObservation
	for _, row := range embeddedCaptureObservations(t).Cases {
		if row.Name == "jpg-original" {
			good = row
		}
		if row.Name == "jpg-invalid-original" {
			bad = row
		}
	}
	dir := t.TempDir()
	frame := writeXMPSourceObservation(t, dir, good)
	writeTestXMP(t, frame.Image, map[string]string{"exif:DateTimeOriginal": "2030-01-01T00:00:00Z"})
	got, found, err := captureTimeFromXMP(frame)
	if err != nil || !found || got.Year() != 2030 {
		t.Fatal("implicit sidecar was not selected", got, err)
	}
	if err := os.WriteFile(frame.OutputSidecar(), []byte("<invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureTimeFromXMP(frame); err == nil {
		t.Fatal("invalid sidecar fell back to embedded timestamp")
	}
	late := t.TempDir()
	for name, row := range map[string]xmpSourceObservation{"frame-00.jpg": good, "frame-01.jpg": bad} {
		if err := os.WriteFile(filepath.Join(late, name), row.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, late)
	var out, stderr bytes.Buffer
	if Main([]string{"timeshift", late, "--offset", "1h", "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted invalid late timestamp")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, late)) {
		t.Fatal("partial time-shift writes")
	}
}
