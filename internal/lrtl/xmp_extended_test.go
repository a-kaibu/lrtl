package lrtl

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func xmpExtendedObservations(t testing.TB) xmpSourceReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-xmp-extended-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference xmpSourceReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 30 {
		t.Fatal("invalid extended-XMP reference")
	}
	return reference
}

func strictExtendedXMPCase(name string) bool {
	switch name {
	case "bad-extension-xml", "chunks-before-standard", "empty-link", "gap", "invalid-link", "lowercase-chunk", "lowercase-link", "missing-extension", "unrelated-extension", "wrong-digest":
		return true
	}
	return false
}

func TestXMPExtendedNativeReader(t *testing.T) {
	strict := 0
	for _, row := range xmpExtendedObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			data, found, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(row.Data))
			if strictExtendedXMPCase(row.Name) {
				strict++
				if err == nil {
					t.Fatal("expected strict integrity error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if row.Result == nil {
				if found {
					t.Fatal("unlinked extension must not supply metadata")
				}
				return
			}
			if !found {
				t.Fatal("missing packet")
			}
			packet, err := parseXMP(data)
			if err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, *row.Result)
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			before := batchFileSnapshot(t, filepath.Dir(frame.Image))
			packet, found, _, err = loadFramePacketSource(frame, true)
			if err != nil || !found {
				t.Fatalf("file reader: %v", err)
			}
			assertPastedMetadata(t, packet, *row.Result)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, filepath.Dir(frame.Image))) {
				t.Fatal("changed source files")
			}
		})
	}
	if strict != 10 {
		t.Fatal("unexpected strict cases", strict)
	}
}

func TestXMPExtendedInstalledReader(t *testing.T) {
	replayInstalledXMPReader(t, xmpExtendedObservations(t))
}

func TestXMPExtendedInitializeAndRender(t *testing.T) {
	for _, row := range xmpExtendedObservations(t).Cases {
		if row.Name == "linked" || row.Name == "three-chunks" || row.Name == "two-links" {
			t.Run(row.Name, func(t *testing.T) { testMergedXMPInitializeAndRender(t, row) })
		}
	}
}

func TestXMPExtendedMalformedPreflight(t *testing.T) {
	reference := xmpExtendedObservations(t)
	var valid []byte
	for _, row := range reference.Cases {
		if row.Name == "linked" {
			valid = row.Data
		}
	}
	for _, row := range reference.Cases {
		if !strictExtendedXMPCase(row.Name) {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range map[string][]byte{"frame-00.jpg": valid, "frame-01.jpg": row.Data} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code == 0 {
				t.Fatal("accepted corrupt or missing extension")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("partial writes before extension failure")
			}
		})
	}
}

func FuzzXMPExtendedJPEG(f *testing.F) {
	for _, row := range xmpExtendedObservations(f).Cases {
		f.Add(row.Data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		before := append([]byte(nil), data...)
		packet, found, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(data))
		if !bytes.Equal(before, data) {
			t.Fatal("changed input bytes")
		}
		if err == nil && found {
			if _, err := parseXMP(packet); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestXMPExtendedChunkValidation(t *testing.T) {
	data := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description/></rdf:RDF></x:xmpmeta>`)
	digest := md5.Sum(data)
	guid := fmt.Sprintf("%x", digest)
	mid := len(data) / 2
	a := extendedXMPChunk{offset: 0, data: data[:mid]}
	b := extendedXMPChunk{offset: uint32(mid), data: data[mid:]}
	conflicting := append([]byte(nil), a.data...)
	conflicting[0] = '!'
	for _, tc := range []struct {
		name   string
		chunks []extendedXMPChunk
		valid  bool
	}{
		{"ordered", []extendedXMPChunk{a, b}, true},
		{"reversed", []extendedXMPChunk{b, a}, true},
		{"duplicate", []extendedXMPChunk{a, b, a, b}, true},
		{"consistent-contained", []extendedXMPChunk{a, b, {offset: 1, data: data[1:5]}}, true},
		{"conflicting", []extendedXMPChunk{a, b, {offset: 0, data: conflicting}}, false},
		{"incomplete", []extendedXMPChunk{a}, false},
		{"gap", []extendedXMPChunk{{offset: 0, data: data[:mid-1]}, b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assembleExtendedXMP(guid, &extendedXMP{size: uint32(len(data)), chunks: tc.chunks})
			if tc.valid {
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("valid assembly: %v", err)
				}
			} else if err == nil {
				t.Fatal("accepted incomplete or conflicting chunks")
			}
		})
	}
}

func TestXMPExtendedAggregateLimit(t *testing.T) {
	const chunkSize = 60000
	payload := append([]byte(jpegExtendedXMPHeader), []byte(strings.Repeat("0", 32))...)
	fields := make([]byte, 8)
	binary.BigEndian.PutUint32(fields, uint32(chunkSize))
	payload = append(payload, fields...)
	payload = append(payload, make([]byte, chunkSize)...)
	segment := []byte{255, 225, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	// The limit also applies to unlinked chunks which are otherwise ignored.
	image := append([]byte{255, 216}, bytes.Repeat(segment, maxEmbeddedXMPSize/chunkSize+1)...)
	image = append(image, 255, 217)
	if _, _, err := readEmbeddedJPEGXMPFromReader(bytes.NewReader(image)); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("unbounded unlinked chunks: %v", err)
	}
}
