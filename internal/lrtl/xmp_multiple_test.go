package lrtl

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func xmpMultipleObservations(t testing.TB) xmpSourceReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-xmp-multiple-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference xmpSourceReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 20 {
		t.Fatal("invalid multiple-XMP observations")
	}
	return reference
}

func TestXMPMultipleNativeReader(t *testing.T) {
	for _, row := range xmpMultipleObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			before := append([]byte(nil), row.Data...)
			data, found, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(row.Data))
			if !bytes.Equal(before, row.Data) {
				t.Fatal("changed input")
			}
			if row.Name == "all-bad" || row.Name == "single-bad" {
				if err == nil {
					t.Fatal("malformed-only XMP must fail strict preflight")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !found || row.Result == nil {
				t.Fatal("missing packet")
			}
			packet, err := parseXMP(data)
			if err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, *row.Result)
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			packet, found, _, err = loadFramePacketSource(frame, true)
			if err != nil || !found {
				t.Fatalf("source import: %v", err)
			}
			assertPastedMetadata(t, packet, *row.Result)
		})
	}
}

func TestXMPMultipleInstalledReader(t *testing.T) {
	replayInstalledXMPReader(t, xmpMultipleObservations(t))
}

func replayInstalledXMPReader(t *testing.T, reference xmpSourceReference) {
	t.Helper()
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for installed multiple-XMP reader replay")
	}
	dir := t.TempDir()
	inputs := filepath.Join(dir, "inputs")
	for _, row := range reference.Cases {
		writeXMPSourceObservation(t, filepath.Join(inputs, row.Name), row)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(root, "MetadataPasteOracle.java"), filepath.Join(root, "XMPSourceOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.XMPSourceOracle", path, inputs).CombinedOutput(); err != nil {
		t.Fatalf("native reader: %v\n%s", err, out)
	}
	var replay xmpSourceReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, reference) {
		t.Fatal("native multiple-XMP results changed")
	}
}

func FuzzXMPMultipleJPEG(f *testing.F) {
	for _, row := range xmpMultipleObservations(f).Cases {
		f.Add(row.Data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		before := append([]byte(nil), data...)
		packet, found, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(data))
		if !bytes.Equal(before, data) {
			t.Fatal("changed JPEG input")
		}
		if err == nil && found {
			if _, err := parseXMP(packet); err != nil {
				t.Fatalf("reader returned invalid XML: %v", err)
			}
		}
	})
}

func TestXMPMultipleInitializeAndRender(t *testing.T) {
	var row xmpSourceObservation
	for _, candidate := range xmpMultipleObservations(t).Cases {
		if candidate.Name == "disjoint-conflict" {
			row = candidate
		}
	}
	testMergedXMPInitializeAndRender(t, row)
}

func testMergedXMPInitializeAndRender(t *testing.T, row xmpSourceObservation) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame-%02d.jpg", i)), row.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if code := Main([]string{"init", dir}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preview changed inputs")
	}
	video := filepath.Join(t.TempDir(), "merged.avi")
	if code := Main([]string{"render", dir, "--output", video, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	beforeVideo := denoiseRead(t, video)
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	frames, err := DiscoverSequence(dir)
	if err != nil || len(frames) != 3 {
		t.Fatalf("frames=%d %v", len(frames), err)
	}
	for _, frame := range frames {
		if !bytes.Equal(denoiseRead(t, frame.Image), row.Data) {
			t.Fatal("init changed JPEG")
		}
		packet, err := PacketForSidecar(frame.Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		assertExposureValue(t, packet, globalXMPExposureTarget(), 2)
		for _, name := range []string{"First", "Second"} {
			if property := packet.findRootProperty("urn:lrtl:multiple", name); property.element == nil && property.attribute == nil {
				t.Fatal("lost earlier packet field", name)
			}
		}
	}
	afterInit := batchFileSnapshot(t, dir)
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	if !reflect.DeepEqual(afterInit, batchFileSnapshot(t, dir)) {
		t.Fatal("repeat init changed sidecars")
	}
	videoAfter := filepath.Join(t.TempDir(), "initialized.avi")
	if code := Main([]string{"render", dir, "--output", videoAfter, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	afterVideo := denoiseRead(t, videoAfter)
	// The AVI library writes a current-time comment before movi. Compare the
	// main header and complete frame/index stream, excluding that comment.
	i, j := bytes.Index(beforeVideo, []byte("movi")), bytes.Index(afterVideo, []byte("movi"))
	h, k := bytes.Index(beforeVideo, []byte("avih")), bytes.Index(afterVideo, []byte("avih"))
	if i < 0 || j < 0 || h < 0 || k < 0 || h+64 > len(beforeVideo) || k+64 > len(afterVideo) || !bytes.Equal(beforeVideo[i:], afterVideo[j:]) || !bytes.Equal(beforeVideo[h:h+64], afterVideo[k:k+64]) {
		t.Fatal("initialization changed AVI main header, frames or index")
	}
}

func TestXMPMultipleMalformedPreflight(t *testing.T) {
	dir := t.TempDir()
	for _, row := range xmpMultipleObservations(t).Cases {
		name := ""
		if row.Name == "disjoint-conflict" {
			name = "frame-00.jpg"
		}
		if row.Name == "all-bad" {
			name = "frame-01.jpg"
		}
		if name != "" {
			if err := os.WriteFile(filepath.Join(dir, name), row.Data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code == 0 {
		t.Fatal("accepted malformed-only image XMP")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("partial writes before malformed late image")
	}
}

func TestXMPMultipleExtendedAndSizeLimit(t *testing.T) {
	segment := func(header string, data []byte) []byte {
		payload := append([]byte(header), data...)
		out := []byte{255, 225, 0, 0}
		binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
		return append(out, payload...)
	}
	var data []byte
	for _, row := range xmpMultipleObservations(t).Cases {
		if row.Name == "single" {
			data = row.Data
		}
	}
	packet, _, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	digest := md5.Sum(packet)
	guid := strings.ToUpper(fmt.Sprintf("%x", digest))
	standard := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:xmpNote="http://ns.adobe.com/xmp/note/" xmpNote:HasExtendedXMP="` + guid + `"/></rdf:RDF></x:xmpmeta>`)
	chunk := append([]byte(guid), make([]byte, 8)...)
	binary.BigEndian.PutUint32(chunk[32:36], uint32(len(packet)))
	chunk = append(chunk, packet...)
	image := append([]byte{255, 216}, segment(jpegXMPHeader, standard)...)
	image = append(image, segment(jpegExtendedXMPHeader, chunk)...)
	image = append(image, 255, 217)
	got, found, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(image))
	if err != nil || !found {
		t.Fatalf("extended regression: %v", err)
	}
	expected, err := parseXMP(packet)
	if err != nil {
		t.Fatal(err)
	}
	if err := expected.SetScalar("xmpNote:HasExtendedXMP", guid); err != nil {
		t.Fatal(err)
	}
	want, err := expected.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := parseXMP(got)
	if err != nil {
		t.Fatal(err)
	}
	assertPastedMetadata(t, actual, string(want))
	// Many individually legal APP1 segments must not create an unbounded merge.
	large := append(packet, bytes.Repeat([]byte(" "), 60000)...)
	app := segment(jpegXMPHeader, large)
	image = append([]byte{255, 216}, bytes.Repeat(app, maxEmbeddedXMPSize/len(large)+1)...)
	image = append(image, 255, 217)
	if _, _, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(image)); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("unbounded merge: %v", err)
	}
}
