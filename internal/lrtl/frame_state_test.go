package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

type frameStateReference struct {
	MetadataCompleted            bool   `json:"metadata_completed"`
	DefaultsCompleted            bool   `json:"defaults_completed"`
	ControllerConstructorSkipped bool   `json:"controller_constructor_skipped"`
	NeutralTag                   int    `json:"neutral_tag"`
	NeutralExiftoolArgument      string `json:"neutral_exiftool_argument"`
	Classifications              []struct {
		Filename, Neutral string
		MetadataQuality   string `json:"metadata_quality"`
		FlatFile          bool   `json:"flat_file"`
	}
	Defaults []struct {
		Quality  *string
		Source   string
		Expected map[string]*string
	}
	References []struct {
		Ratings       []int
		ExpectedIndex int `json:"expected_index"`
	}
}

func frameStateObservations(t *testing.T) frameStateReference {
	t.Helper()
	var reference frameStateReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-frame-state-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.MetadataCompleted || !reference.DefaultsCompleted || !reference.ControllerConstructorSkipped || reference.NeutralTag != 50728 || reference.NeutralExiftoolArgument != "-AsShotNeutral" || len(reference.Classifications) != 196 || len(reference.Defaults) != 224 || len(reference.References) != 6 {
		t.Fatal("invalid frame-state reference provenance")
	}
	return reference
}

func TestFrameStateNativeDefaultsAndFilePredicates(t *testing.T) {
	reference := frameStateObservations(t)
	for _, row := range reference.References {
		packets := make([]*XMPPacket, len(row.Ratings))
		for i, rating := range row.Ratings {
			packets[i] = EmptyXMPPacket()
			if err := packets[i].SetScalar("xmp:Rating", strconv.Itoa(rating)); err != nil {
				t.Fatal(err)
			}
		}
		if actual := nativeFrameStateReference(packets); actual != row.ExpectedIndex {
			t.Fatal(row, actual)
		}
	}
	for _, row := range reference.Classifications {
		if actual := nativeFlatFrameFile(row.Filename); actual != row.FlatFile {
			t.Fatal(row.Filename, actual, row.FlatFile)
		}
	}
	for i, row := range reference.Defaults {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, bare := range []bool{false, true} {
				packet := pasteTestPacket(t, row.Source)
				if !bare {
					packet = pasteTestPacket(t, `<x:xmpmeta xmlns:x="adobe:ns:meta/">`+row.Source+`</x:xmpmeta>`)
				}
				quality := transitionScalar{}
				if row.Quality != nil {
					quality = transitionScalar{*row.Quality, true}
				}
				if _, err := applyNativeWBDefaults(packet, quality); err != nil {
					t.Fatal(err)
				}
				for name, expected := range row.Expected {
					value, present, err := packet.GetRootScalar("crs:" + name)
					if err != nil {
						t.Fatal(err)
					}
					if expected == nil {
						if present {
							t.Fatal(name, "unexpected", value)
						}
						continue
					}
					if name != "WhiteBalance" {
						columns, err := parseSelectedColumns(name)
						if err != nil {
							t.Fatal(err)
						}
						value = normalizeSelectedColumn(columns[0], transitionScalar{value, present}).value
					}
					if !present || value != *expected {
						t.Fatal(name, value, *expected)
					}
				}
				original, _ := packet.Serialize()
				if changed, err := applyNativeWBDefaults(packet, quality); changed || err != nil {
					t.Fatal(changed, err)
				}
				after, _ := packet.Serialize()
				if !bytes.Equal(original, after) {
					t.Fatal("default helper did not stabilize")
				}
			}
		})
	}
}

// Small original TIFFs for metadata inspection, without image decoding.
func frameStateNeutralTIFF(order binary.ByteOrder, kind uint16, count uint32, value []byte, sub bool) []byte {
	ifd := 8
	if sub {
		ifd = 26
	}
	dataOffset := ifd + 18
	data := make([]byte, dataOffset+len(value))
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	if sub {
		order.PutUint16(data[8:10], 1)
		order.PutUint16(data[10:12], 330)
		order.PutUint16(data[12:14], 4)
		order.PutUint32(data[14:18], 1)
		order.PutUint32(data[18:22], uint32(ifd))
	}
	if kind == 0 {
		return data
	}
	order.PutUint16(data[ifd:ifd+2], 1)
	entry := data[ifd+2 : ifd+14]
	order.PutUint16(entry[:2], 50728)
	order.PutUint16(entry[2:4], kind)
	order.PutUint32(entry[4:8], count)
	if len(value) <= 4 {
		copy(entry[8:12], value)
	} else {
		order.PutUint32(entry[8:12], uint32(dataOffset))
		copy(data[dataOffset:], value)
	}
	return data
}

func frameStateNeutralFixtures(t *testing.T, dir string) map[string]bool {
	t.Helper()
	wantFlat := map[string]bool{}
	for oi, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		rational := make([]byte, 24)
		for i := 0; i < 3; i++ {
			order.PutUint32(rational[i*8:], 1)
			order.PutUint32(rational[i*8+4:], 2)
		}
		doubles := make([]byte, 24)
		for i := 0; i < 3; i++ {
			order.PutUint64(doubles[i*8:], math.Float64bits(.5))
		}
		for _, sub := range []bool{false, true} {
			for _, tc := range []struct {
				name  string
				kind  uint16
				count uint32
				value []byte
				flat  bool
			}{
				{"absent", 0, 0, nil, true}, {"rational", 5, 3, rational, false}, {"double", 12, 3, doubles, false},
				{"ascii", 2, 6, []byte("1 1 1\x00"), false}, {"blank", 2, 3, []byte("  \x00"), true},
				{"zero", 4, 1, []byte{0, 0, 0, 0}, false},
			} {
				name := fmt.Sprintf("%d-%t-%s.dng", oi, sub, tc.name)
				if err := os.WriteFile(filepath.Join(dir, name), frameStateNeutralTIFF(order, tc.kind, tc.count, tc.value, sub), 0600); err != nil {
					t.Fatal(err)
				}
				wantFlat[name] = tc.flat
			}
		}
	}
	return wantFlat
}

func TestFrameStateDNGNeutralAndReaderFailures(t *testing.T) {
	dir := t.TempDir()
	for name, flat := range frameStateNeutralFixtures(t, dir) {
		quality, err := nativeSequenceQuality(filepath.Join(dir, name))
		if err != nil || (quality == "FLAT") != flat {
			t.Fatal(name, quality, flat, err)
		}
	}
	bad := frameStateNeutralTIFF(binary.LittleEndian, 5, 3, make([]byte, 24), false)
	binary.LittleEndian.PutUint32(bad[18:22], math.MaxUint32)
	path := filepath.Join(dir, "truncated.dng")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeSequenceQuality(path); err == nil {
		t.Fatal("accepted out-of-file neutral value")
	}
	if _, err := nativeSequenceQuality(filepath.Join(dir, "missing.dng")); err == nil {
		t.Fatal("accepted missing DNG")
	}
}

func TestFrameStateInstalledComponents(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for metadata/WB-default and real DNG-tag comparisons")
	}
	dir := t.TempDir()
	probeRoot, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(probeRoot, "MetadataPasteOracle.java"), filepath.Join(probeRoot, "FrameStateOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	run := func(path string, args ...string) {
		t.Helper()
		command := []string{"--enable-native-access=ALL-UNNAMED", "--sun-misc-unsafe-memory-access=allow", "-cp", dir + string(os.PathListSeparator) + jar, "lrtlprobe.FrameStateOracle", path}
		if out, err := exec.Command("java", append(command, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("native frame state: %v\n%s", err, out)
		}
	}
	result := filepath.Join(dir, "replay.json")
	run(result)
	var replay frameStateReference
	if err := json.Unmarshal(denoiseRead(t, result), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, frameStateObservations(t)) {
		t.Fatal("native frame-state reference changed")
	}
	files := filepath.Join(dir, "files")
	if err := os.Mkdir(files, 0700); err != nil {
		t.Fatal(err)
	}
	wantFlat := frameStateNeutralFixtures(t, files)
	run(result, "--neutral", files)
	var observed []struct {
		Filename string
		Flat     bool
	}
	if err := json.Unmarshal(denoiseRead(t, result), &observed); err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(wantFlat) {
		t.Fatal("missing native file observations")
	}
	for _, row := range observed {
		want, ok := wantFlat[row.Filename]
		quality, err := nativeSequenceQuality(filepath.Join(files, row.Filename))
		if !ok || err != nil || row.Flat != want || (quality == "FLAT") != row.Flat {
			t.Fatal(row, quality, want, err)
		}
	}
}

func FuzzFrameStatePreparation(f *testing.F) {
	f.Add(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description crs:WhiteBalance=""/></rdf:RDF>`, false)
	data, err := os.ReadFile("testdata/lrt-frame-state-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference frameStateReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for i, row := range reference.Defaults {
		if i%11 == 0 {
			f.Add(row.Source, i%2 == 0)
		}
	}
	f.Fuzz(func(t *testing.T, source string, flat bool) {
		if len(source) > 32768 {
			return
		}
		packet, err := parseXMP([]byte(source))
		if err != nil {
			return
		}
		before, err := packet.Serialize()
		if err != nil {
			return
		}
		quality := "RAW"
		if flat {
			quality = "FLAT"
		}
		editErr := prepareNativeFrameState(packet, quality)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if editErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed preparation mutated packet")
			}
			return
		}
		packet, err = parseXMP(after)
		if err != nil {
			t.Fatal(err)
		}
		if value, present, _ := packet.GetRootScalar("lrt:Quality"); !present || value != quality {
			t.Fatal("wrong prepared quality", value)
		}
		// Reloaded tiny crops can change HasCrop after six-decimal rounding.
		// A second fresh initialization incorporates that loss, then stabilizes.
		if err := prepareNativeFrameState(packet, quality); err != nil {
			t.Fatal(err)
		}
		settled, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		packet, err = parseXMP(settled)
		if err != nil {
			t.Fatal(err)
		}
		if err := prepareNativeFrameState(packet, quality); err != nil {
			t.Fatal(err)
		}
		repeated, err := packet.Serialize()
		if err != nil || !bytes.Equal(settled, repeated) {
			t.Fatal("reloaded preparation did not stabilize", err)
		}
	})
}

func FuzzFrameStateDNGMetadata(f *testing.F) {
	f.Add(frameStateNeutralTIFF(binary.LittleEndian, 0, 0, nil, false))
	f.Add(frameStateNeutralTIFF(binary.BigEndian, 5, 3, make([]byte, 24), true))
	f.Add([]byte("II\x2a\x00\x08\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32768 {
			return
		}
		path := filepath.Join(t.TempDir(), "input.dng")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), data...)
		quality, err := nativeSequenceQuality(path)
		if err == nil && quality != "RAW" && quality != "FLAT" {
			t.Fatal("invalid classification", quality)
		}
		if !bytes.Equal(before, denoiseRead(t, path)) {
			t.Fatal("classification changed image")
		}
	})
}
