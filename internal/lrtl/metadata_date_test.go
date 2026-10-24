package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type metadataDateReference struct {
	Completed bool `json:"store_completed"`
	Stop      string
	Marker    string `json:"clock_marker"`
	Formatter string `json:"default_formatter"`
	Formats   []struct {
		Instant, Zone, Expected string
		Offset                  int `json:"offset_seconds"`
	}
	Cases []struct {
		Quality, Active, Date *string
		Previous, Status      string
		Error                 *string
		Target, Saved         string
	}
}

func metadataDateObservations(t testing.TB) metadataDateReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-metadata-date-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var r metadataDateReference
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Completed || !strings.Contains(r.Stop, "history") || len(r.Cases) != 512 || len(r.Formats) != 64 || r.Marker == "" {
		t.Fatal("invalid metadata-date observations")
	}
	return r
}

func TestMetadataDateNativeFormats(t *testing.T) {
	for _, row := range metadataDateObservations(t).Formats {
		stamp, err := time.Parse(time.RFC3339Nano, row.Instant)
		if err != nil {
			t.Fatal(err)
		}
		got := formatNativeMetadataDate(stamp.In(time.FixedZone(row.Zone, row.Offset)))
		if got != row.Expected {
			t.Errorf("%s %s: got %q want %q", row.Instant, row.Zone, got, row.Expected)
		}
	}
}

func TestMetadataDateNativeConditions(t *testing.T) {
	r := metadataDateObservations(t)
	failures := 0
	for i, row := range r.Cases {
		packet, err := parseXMP([]byte(row.Target))
		if err != nil {
			t.Fatal(err)
		}
		before, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		date := ""
		if row.Date != nil {
			date = *row.Date
		}
		changed, err := applyNativeMetadataDate(packet, date, r.Marker)
		if row.Status == "error" {
			failures++
			if err == nil {
				t.Fatalf("case %d: expected native composite-value error", i)
			}
			after, _ := packet.Serialize()
			if !bytes.Equal(before, after) {
				t.Fatal("failed clock update modified packet")
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		expected, err := parseXMP([]byte(row.Saved))
		if err != nil {
			t.Fatal(err)
		}
		gotNodes, err := pastePacketNodes(packet)
		if err != nil {
			t.Fatal(err)
		}
		wantNodes, err := pastePacketNodes(expected)
		if err != nil {
			t.Fatal(err)
		}
		key := pasteName{xmpNamespace, "MetadataDate"}
		got, want := selectedField(gotNodes, key), selectedField(wantNodes, key)
		if (got == nil) != (want == nil) || got != nil && !pasteNodesEqual(got, want) {
			t.Fatalf("case %d: native MetadataDate differs", i)
		}
		if changed != (want != nil && want.Value == r.Marker) {
			t.Fatal("wrong clock eligibility", i)
		}
	}
	if failures != 24 {
		t.Fatal("unexpected native error count", failures)
	}
}

func TestMetadataDateInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for native MetadataDate replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "MetadataDateOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	base := []string{"--enable-native-access=ALL-UNNAMED", "-cp", jar + string(os.PathListSeparator) + dir, "lrtlprobe.MetadataDateOracle", path}
	if out, err := exec.Command("java", base...).CombinedOutput(); err != nil {
		t.Fatalf("native observer: %v\n%s", err, out)
	}
	var replay metadataDateReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, metadataDateObservations(t)) {
		t.Fatal("native MetadataDate observations changed")
	}
	if out, err := exec.Command("java", append(base, "--live")...).CombinedOutput(); err != nil {
		t.Fatalf("native live clock: %v\n%s", err, out)
	}
	var live struct{ Before, After, Clock string }
	if err := json.Unmarshal(denoiseRead(t, path), &live); err != nil {
		t.Fatal(err)
	}
	before, err := time.Parse(time.RFC3339Nano, live.Before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := time.Parse(time.RFC3339Nano, live.After)
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := parseMetadataDateClock(live.Clock)
	if err != nil {
		t.Fatal(err)
	}
	if stamp.Before(before.Truncate(time.Second)) || stamp.After(after) {
		t.Fatal("native live clock outside save window", live)
	}
}

func parseMetadataDateClock(value string) (time.Time, error) {
	stamp, err := time.Parse("2006-01-02T15:04:05-0700", value)
	if err != nil {
		return time.Parse("2006-01-02T15:04:05-070000", value)
	}
	return stamp, nil
}

func TestMetadataDatePreparationIsAtomic(t *testing.T) {
	var good, bad *XMPPacket
	for _, row := range metadataDateObservations(t).Cases {
		if row.Quality == nil || *row.Quality != "RAW" || row.Active == nil || *row.Active != "True" || row.Date == nil || *row.Date == "" {
			continue
		}
		if row.Previous == "qualified" {
			good = pasteTestPacket(t, row.Target)
		}
		if row.Previous == "struct" {
			bad = pasteTestPacket(t, row.Target)
		}
	}
	changes := []initializationChange{{Frame: Frame{Image: "one.jpg"}, Packet: good, CaptureDate: "2026-01-01T00:00:00"}, {Frame: Frame{Image: "two.jpg"}, Packet: bad, CaptureDate: "2026-01-01T00:00:00"}}
	first, _ := good.Serialize()
	second, _ := bad.Serialize()
	calls := 0
	clock := func() time.Time {
		calls++
		return time.Date(2026, 10, 24, 3, 4, calls, 987654321, time.FixedZone("test", 32400))
	}
	if _, err := prepareInitializationWrites(changes, clock); err == nil {
		t.Fatal("accepted late structured MetadataDate")
	}
	if a, _ := good.Serialize(); !bytes.Equal(a, first) {
		t.Fatal("preflight changed first plan packet")
	}
	if a, _ := bad.Serialize(); !bytes.Equal(a, second) {
		t.Fatal("preflight changed invalid plan packet")
	}
	changes[1].Packet = &XMPPacket{Document: good.Document.Copy()}
	calls = 0
	prepared, err := prepareInitializationWrites(changes, clock)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("expected per-frame clock")
	}
	for i, change := range prepared {
		value, found, err := captureTimestampScalar(change.Packet, "xmp:MetadataDate")
		want := formatNativeMetadataDate(time.Date(2026, 10, 24, 3, 4, i+1, 0, time.FixedZone("test", 32400)))
		if err != nil || !found || value != want {
			t.Fatal("incorrect prepared clock", value, err)
		}
	}
	changes[0].CaptureDate = ""
	changes[1].CaptureDate = ""
	if _, err := prepareInitializationWrites(changes, func() time.Time { t.Fatal("clock used without native capture date"); return time.Time{} }); err != nil {
		t.Fatal(err)
	}
}

func metadataDateFrame(t *testing.T, dir, name, active, previous string) Frame {
	t.Helper()
	var image []byte
	for _, row := range captureDateImages(t).Cases {
		if row.Name == "le-original.jpg" {
			image = row.Data
		}
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	body := "<xmp:MetadataDate>previous</xmp:MetadataDate>"
	if previous == "qualified" {
		body = "<xmp:MetadataDate xml:lang=\"en-US\">previous</xmp:MetadataDate>"
	}
	if previous == "struct" {
		body = "<xmp:MetadataDate rdf:parseType=\"Resource\"><f:Value>previous</f:Value></xmp:MetadataDate>"
	}
	xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:exif="http://ns.adobe.com/exif/1.0/" xmlns:f="urn:lrtl:test" crs:HasSettings="%s" crs:Exposure2012=".25" xmp:CreateDate="2031-01-01T00:00:00" xmp:ModifyDate="2032-01-01T00:00:00" exif:DateTimeOriginal="2030-01-01T00:00:00" f:Keep="unchanged">%s</rdf:Description></rdf:RDF></x:xmpmeta>`, active, body)
	if err := os.WriteFile(path+".xmp", []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	ensureTestSidecarNewer(t, path, path+".xmp")
	return Frame{Image: path, Sidecar: path + ".xmp"}
}

func TestMetadataDateCLIWriteWindowAndNoop(t *testing.T) {
	dir := t.TempDir()
	frame := metadataDateFrame(t, dir, "frame.jpg", "True", "qualified")
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if !strings.Contains(out.String(), "MetadataDate: updated when saved") || !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preview changed files or omitted clock effect")
	}
	start := time.Now()
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	end := time.Now()
	packet, err := PacketForSidecar(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := captureTimestampScalar(packet, "xmp:MetadataDate")
	if err != nil || !found {
		t.Fatal("missing saved clock", err)
	}
	stamp, err := parseMetadataDateClock(value)
	if err != nil {
		t.Fatal(err)
	}
	if stamp.Before(start.Truncate(time.Second)) || stamp.After(end) {
		t.Fatal("saved clock outside write window", value)
	}
	for _, p := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate"} {
		if v, _, _ := captureTimestampScalar(packet, p); v != "2026-10-24T03:04:05" {
			t.Fatal("clock replaced capture date", p, v)
		}
	}
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	clock := selectedField(nodes, pasteName{xmpNamespace, "MetadataDate"})
	if pasteQualifier(clock, xmlNamespace, "lang") != "en-US" {
		t.Fatal("clock lost qualifier")
	}
	first := batchFileSnapshot(t, dir)
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if !reflect.DeepEqual(first, batchFileSnapshot(t, dir)) {
		t.Fatal("no-op init changed clock or backups")
	}
	backups, err := filepath.Glob(frame.Sidecar + ".*.bak")
	if err != nil || len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
		t.Fatal("wrong backup", err)
	}
	if !bytes.Equal(denoiseRead(t, frame.Image), before[frame.Image]) {
		t.Fatal("clock write changed image")
	}
}

func TestMetadataDateCLIDisabledAndPreserve(t *testing.T) {
	for _, tc := range []struct {
		active, previous string
		preserve         bool
	}{{"False", "scalar", false}, {"False", "struct", false}, {"True", "qualified", true}, {" true ", "scalar", false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			dir := t.TempDir()
			frame := metadataDateFrame(t, dir, "frame.jpg", tc.active, tc.previous)
			original, err := PacketForSidecar(frame.Sidecar)
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			args := []string{"init", dir, "--write"}
			if tc.preserve {
				args = append(args, "--frame-state", "preserve")
			}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			packet, err := PacketForSidecar(frame.Sidecar)
			if err != nil {
				t.Fatal(err)
			}
			for _, property := range []string{"xmp:MetadataDate", "exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate"} {
				diff, err := packet.RootPropertyDiffers(original, property)
				if err != nil || diff {
					t.Fatal("disabled/preserve date changed", property, err)
				}
			}
		})
	}
}

func TestMetadataDateCLILateErrorHasNoWrites(t *testing.T) {
	dir := t.TempDir()
	metadataDateFrame(t, dir, "frame-00.jpg", "True", "scalar")
	metadataDateFrame(t, dir, "frame-01.jpg", "True", "struct")
	before := batchFileSnapshot(t, dir)
	for _, write := range []bool{false, true} {
		var out, stderr bytes.Buffer
		args := []string{"init", dir}
		if write {
			args = append(args, "--write")
		}
		if Main(args, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "MetadataDate") {
			t.Fatal("late structured clock accepted", stderr.String())
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("failed preflight changed files")
		}
	}
}

func FuzzMetadataDate(f *testing.F) {
	for _, row := range metadataDateObservations(f).Cases {
		date := ""
		if row.Date != nil {
			date = *row.Date
		}
		f.Add([]byte(row.Target), date, int64(0), int32(0))
	}
	f.Fuzz(func(t *testing.T, data []byte, date string, seconds int64, offset int32) {
		if len(data) > 1<<16 || len(date) > 1024 {
			t.Skip()
		}
		packet, err := parseXMP(data)
		if err != nil {
			return
		}
		stamp := time.Unix(seconds%253402300800, 123456789).In(time.FixedZone("fuzz", int(offset%64801)))
		value := formatNativeMetadataDate(stamp)
		if formatNativeMetadataDate(stamp.Add(time.Nanosecond)) != value || strings.Contains(value, ".") {
			t.Fatal("native clock did not discard fractions")
		}
		before, err := packet.Serialize()
		if err != nil {
			return
		}
		changed, err := applyNativeMetadataDate(packet, date, value)
		if err != nil || !changed {
			after, _ := packet.Serialize()
			if !bytes.Equal(before, after) {
				t.Fatal("skipped/failed clock changed metadata")
			}
			return
		}
		got, found, err := captureTimestampScalar(packet, "xmp:MetadataDate")
		if err != nil || !found || got != value {
			t.Fatal("clock update lost value", err)
		}
	})
}
