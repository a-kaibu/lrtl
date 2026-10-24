package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/image/tiff"
)

func xmpTIFFMultipleObservations(t testing.TB) xmpSourceReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-xmp-tiff-multiple-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference xmpSourceReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 120 {
		t.Fatal("invalid TIFF-XMP reference")
	}
	return reference
}

func TestXMPTIFFMultipleNativeReader(t *testing.T) {
	strict := 0
	for _, row := range xmpTIFFMultipleObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			before := batchFileSnapshot(t, filepath.Dir(frame.Image))
			packet, found, _, err := loadFramePacketSource(frame, true)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, filepath.Dir(frame.Image))) {
				t.Fatal("modified source")
			}
			if strings.HasSuffix(row.Name, "-bad-first") {
				strict++
				if err == nil {
					t.Fatal("malformed eligible tag must fail strict preflight")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if row.Result == nil {
				if found {
					t.Fatal("adopted ignored page metadata")
				}
				return
			}
			if !found {
				t.Fatal("missing metadata")
			}
			assertPastedMetadata(t, packet, *row.Result)
		})
	}
	if strict != 6 {
		t.Fatal("unexpected strict count", strict)
	}
}

func TestXMPTIFFMultipleInstalledReader(t *testing.T) {
	replayInstalledXMPReader(t, xmpTIFFMultipleObservations(t))
}

func TestXMPTIFFMultipleMalformedPreflight(t *testing.T) {
	reference := xmpTIFFMultipleObservations(t)
	for _, bad := range reference.Cases {
		if !strings.HasSuffix(bad.Name, "-bad-first") {
			continue
		}
		t.Run(bad.Name, func(t *testing.T) {
			dir := t.TempDir()
			var valid xmpSourceObservation
			for _, row := range reference.Cases {
				if row.Name == strings.TrimSuffix(bad.Name, "bad-first")+"sub" {
					valid = row
				}
			}
			if len(valid.Data) == 0 {
				t.Fatal("missing positive control")
			}
			ext := filepath.Ext(bad.Filename)
			for name, data := range map[string][]byte{"frame-00" + ext: valid.Data, "frame-01" + ext: bad.Data} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code == 0 {
				t.Fatal("accepted malformed TIFF metadata")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("partial sidecar writes")
			}
		})
	}
}

func FuzzXMPTIFFMultiple(f *testing.F) {
	for _, row := range xmpTIFFMultipleObservations(f).Cases {
		f.Add(row.Data)
	}
	path := filepath.Join(f.TempDir(), "fuzz.tif")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		packet, found, err := readEmbeddedTIFFXMP(path)
		if !bytes.Equal(denoiseRead(t, path), data) {
			t.Fatal("reader modified image")
		}
		if err == nil && found {
			if _, err := parseXMP(packet); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// Attach a new root and SubIFD to an actual Go-encoded TIFF, keeping the raster
// strip offsets intact. Only synthetic pixels/XML are used.
func tiffWithTwoXMPPackets(t *testing.T, root, child []byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(20 + x), uint8(30 + y), 40, 255})
		}
	}
	var encoded bytes.Buffer
	if err := tiff.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	if string(data[:2]) != "II" {
		t.Fatal("expected little-endian TIFF encoder")
	}
	order := binary.LittleEndian
	old := int(order.Uint32(data[4:8]))
	count := int(order.Uint16(data[old : old+2]))
	entries := make([][]byte, 0, count+2)
	for i := 0; i < count; i++ {
		entries = append(entries, append([]byte(nil), data[old+2+12*i:old+14+12*i]...))
	}
	rootOffset := len(data)
	childOffset := rootOffset + 2 + 12*(count+2) + 4
	childPacketOffset := childOffset + 18
	rootPacketOffset := childPacketOffset + len(child)
	entry := func(tag, kind uint16, count, value uint32) []byte {
		out := make([]byte, 12)
		order.PutUint16(out, tag)
		order.PutUint16(out[2:], kind)
		order.PutUint32(out[4:], count)
		order.PutUint32(out[8:], value)
		return out
	}
	entries = append(entries, entry(330, 4, 1, uint32(childOffset)), entry(700, 7, uint32(len(root)), uint32(rootPacketOffset)))
	sort.Slice(entries, func(i, j int) bool { return order.Uint16(entries[i]) < order.Uint16(entries[j]) })
	directory := make([]byte, 2+12*len(entries)+4)
	order.PutUint16(directory, uint16(len(entries)))
	for i, e := range entries {
		copy(directory[2+12*i:], e)
	}
	data = append(data, directory...)
	sub := make([]byte, 18)
	order.PutUint16(sub, 1)
	copy(sub[2:], entry(700, 7, uint32(len(child)), uint32(childPacketOffset)))
	data = append(data, sub...)
	data = append(data, child...)
	data = append(data, root...)
	order.PutUint32(data[4:8], uint32(rootOffset))
	return data
}

func TestXMPTIFFMultipleInitializeAndRender(t *testing.T) {
	xml := func(body string) []byte {
		return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:multiple">` + body + `</rdf:Description></rdf:RDF></x:xmpmeta>`)
	}
	root := xml(`<crs:Exposure2012>1</crs:Exposure2012><f:First>A</f:First>`)
	child := xml(`<crs:Exposure2012>2</crs:Exposure2012><f:Second>B</f:Second>`)
	expected := string(xml(`<crs:Exposure2012>1</crs:Exposure2012><f:First>A</f:First><f:Second>B</f:Second>`))
	testMergedXMPInitializeAndRender(t, xmpSourceObservation{Filename: "frame.tif", Data: tiffWithTwoXMPPackets(t, root, child), Result: &expected})
}

func TestXMPTIFFMultipleLimitsAndRawScope(t *testing.T) {
	reference := xmpTIFFMultipleObservations(t)
	var single, sub xmpSourceObservation
	for _, row := range reference.Cases {
		if row.Name == "dng-le-root" {
			single = row
		}
		if row.Name == "dng-le-sub" {
			sub = row
		}
	}
	for _, ext := range []string{".nef", ".cr2", ".rw2", ".arw"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frame"+ext)
			if err := os.WriteFile(path, sub.Data, 0600); err != nil {
				t.Fatal(err)
			}
			data, found, err := readEmbeddedTIFFXMP(path)
			if err != nil || !found {
				t.Fatalf("RAW extended support changed: %v", err)
			}
			packet, err := parseXMP(data)
			if err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, *single.Result)
		})
	}
	// A small legal root packet followed by a declared 32 MiB Exif packet
	// exceeds the aggregate limit before attempting the out-of-file read.
	rootData, found, err := func() ([]byte, bool, error) {
		path := filepath.Join(t.TempDir(), "single.tif")
		if err := os.WriteFile(path, single.Data, 0600); err != nil {
			t.Fatal(err)
		}
		return readEmbeddedTIFFXMP(path)
	}()
	if err != nil || !found {
		t.Fatal("missing root control", err)
	}
	order := binary.LittleEndian
	// BigTIFF keeps its existing extended-support path; the native classic-
	// TIFF observations are not used to redefine its directory selection.
	big := make([]byte, 108)
	copy(big, []byte{'I', 'I', 43, 0, 8, 0, 0, 0})
	order.PutUint64(big[8:], 16)
	order.PutUint64(big[16:], 2)
	bigEntry := func(at int, tag, kind uint16, count, value uint64) {
		order.PutUint16(big[at:], tag)
		order.PutUint16(big[at+2:], kind)
		order.PutUint64(big[at+4:], count)
		order.PutUint64(big[at+12:], value)
	}
	bigEntry(24, 330, 18, 1, 72)
	bigEntry(44, 700, 7, uint64(len(rootData)), 108)
	order.PutUint64(big[72:], 1)
	childData := []byte(*sub.Result)
	bigEntry(80, 700, 7, uint64(len(childData)), uint64(108+len(rootData)))
	big = append(big, rootData...)
	big = append(big, childData...)
	for _, ext := range []string{".tif", ".dng"} {
		path := filepath.Join(t.TempDir(), "big"+ext)
		if err := os.WriteFile(path, big, 0600); err != nil {
			t.Fatal(err)
		}
		data, found, err := readEmbeddedTIFFXMP(path)
		if err != nil || !found {
			t.Fatalf("BigTIFF support changed: %v", err)
		}
		packet, err := parseXMP(data)
		if err != nil {
			t.Fatal(err)
		}
		assertPastedMetadata(t, packet, *single.Result)
	}
	data := make([]byte, 56)
	copy(data, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	order.PutUint16(data[8:], 2)
	entry := func(at int, tag, kind uint16, count, value uint32) {
		order.PutUint16(data[at:], tag)
		order.PutUint16(data[at+2:], kind)
		order.PutUint32(data[at+4:], count)
		order.PutUint32(data[at+8:], value)
	}
	entry(10, 700, 7, uint32(len(rootData)), 56)
	entry(22, 34665, 4, 1, 38)
	order.PutUint16(data[38:], 1)
	entry(40, 700, 7, maxEmbeddedTIFFXMPSize, 56)
	data = append(data, rootData...)
	path := filepath.Join(t.TempDir(), "aggregate.tif")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readEmbeddedTIFFXMP(path); err == nil || !strings.Contains(err.Error(), "packets exceed safety limit") {
		t.Fatalf("aggregate limit: %v", err)
	}
	// An acyclic graph must also stop at the bounded recursion depth.
	data = make([]byte, 8+18*35)
	copy(data, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	for i := 0; i < 35; i++ {
		at := 8 + 18*i
		order.PutUint16(data[at:], 1)
		value := uint32(at + 18)
		if i == 34 {
			value = 0
		}
		entry(at+2, 330, 4, 1, value)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readEmbeddedTIFFXMP(path); err == nil || !strings.Contains(err.Error(), "graph exceeds safety limits") {
		t.Fatalf("depth limit: %v", err)
	}
}
