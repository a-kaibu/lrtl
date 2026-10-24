package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

type captureDateCacheReference struct {
	Completed bool `json:"metadata_completed"`
	Cases     []struct {
		Seed                                *string
		Date, Subsecond, Fallback, Expected string
	}
}

func TestCaptureDateInitializationAndPreserve(t *testing.T) {
	for _, preserve := range []bool{false, true} {
		t.Run(fmt.Sprint(preserve), func(t *testing.T) {
			dir := t.TempDir()
			expected := map[string]string{}
			for _, row := range captureDateImages(t).Cases {
				if row.Name != "le-sub-100-none.jpg" && row.Name != "le-sub-1-none.jpg" && row.Name != "le-fallback.jpg" && row.Name != "le-sub-100-none.png" && row.Name != "le-fallback.png" && row.Name != "xmp-jpg-original.jpg" && row.Name != "xmp-jpg-create.jpg" && row.Name != "xmp-jpg-alias-foreign-prefix.jpg" {
					continue
				}
				path := filepath.Join(dir, row.Name)
				if err := os.WriteFile(path, row.Data, 0600); err != nil {
					t.Fatal(err)
				}
				writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": "2030-01-01T00:00:00", "xmp:CreateDate": "2031-01-01T00:00:00", "xmp:ModifyDate": "2032-01-01T00:00:00", "photoshop:DateCreated": "2033-01-01", "dc:description": "keep unknown metadata", "crs:Exposure2012": ".25"})
				ensureTestSidecarNewer(t, path, path+".xmp")
				expected[path] = row.SeededDate
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"init", dir}
			if preserve {
				args = append(args, "--frame-state", "preserve")
			}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			for path, date := range expected {
				packet, err := PacketForSidecar(path + ".xmp")
				if err != nil {
					t.Fatal(err)
				}
				for i, p := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate", "photoshop:DateCreated"} {
					want := date
					if preserve {
						want = []string{"2030-01-01T00:00:00", "2031-01-01T00:00:00", "2032-01-01T00:00:00", "2033-01-01"}[i]
					}
					got, found, err := captureTimestampScalar(packet, p)
					if err != nil || !found || got != want {
						t.Fatalf("%s=%q want %q err %v", p, got, want, err)
					}
				}
				if value, _, _ := packet.GetRootScalar("dc:description"); value != "keep unknown metadata" {
					t.Fatal("lost metadata")
				}
				if !bytes.Equal(before[path], denoiseRead(t, path)) {
					t.Fatal("changed image")
				}
				backups, err := filepath.Glob(path + ".xmp.*.bak")
				if err != nil || len(backups) != 1 || !bytes.Equal(before[path+".xmp"], denoiseRead(t, backups[0])) {
					t.Fatal("missing exact backup", err)
				}
			}
			first := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			if !reflect.DeepEqual(first, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat init changed files")
			}
		})
	}
}

func TestCaptureDateLateCorruptionPreflight(t *testing.T) {
	var row captureDateImageRow
	for _, r := range captureDateImages(t).Cases {
		if r.Name == "le-original.jpg" {
			row = r
		}
	}
	dir := t.TempDir()
	bad := append([]byte(nil), row.Data...)
	start := bytes.Index(bad, []byte("Exif\x00\x00")) + 6
	if start < 6 {
		t.Fatal("missing EXIF input")
	}
	binary.LittleEndian.PutUint32(bad[start+42:], 0xfffffff0)
	for name, data := range map[string][]byte{"frame-00.jpg": row.Data, "frame-01.jpg": bad} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted invalid EXIF directory")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("partial writes on date read failure")
	}
}

func TestCaptureDateReaderBounds(t *testing.T) {
	images := map[string][]byte{}
	for _, row := range captureDateImages(t).Cases {
		images[row.Name] = row.Data
	}
	check := func(name, ext string, data []byte, message string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), name+ext)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readNativeCaptureDate(path); err == nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("error changed source")
		}
	}
	long := append([]byte(nil), images["le-original.tif"]...)
	binary.LittleEndian.PutUint32(long[526:], 257)
	check("long", ".tif", long, "string exceeds safety limit")
	data := make([]byte, 8+35*18)
	copy(data, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	for i := 0; i < 35; i++ {
		at := 8 + 18*i
		binary.LittleEndian.PutUint16(data[at:], 1)
		binary.LittleEndian.PutUint16(data[at+2:], 34665)
		binary.LittleEndian.PutUint16(data[at+4:], 4)
		binary.LittleEndian.PutUint32(data[at+6:], 1)
		if i < 34 {
			binary.LittleEndian.PutUint32(data[at+10:], uint32(at+18))
		}
	}
	check("depth", ".tif", data, "graph exceeds safety limits")
	png := append([]byte(nil), images["le-original.png"]...)
	at := bytes.Index(png, []byte("eXIf"))
	if at < 4 {
		t.Fatal("missing eXIf")
	}
	length := int(binary.BigEndian.Uint32(png[at-4 : at]))
	png[at+4+length] ^= 1
	check("crc", ".png", png, "invalid PNG eXIf checksum")
	jpeg := append([]byte(nil), images["le-original.jpg"]...)
	jpeg[4], jpeg[5] = 255, 255
	check("segment", ".jpg", jpeg, "truncated JPEG EXIF segment")
}

func TestCaptureDateInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for native cache/save replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "CaptureDateCacheOracle.java", "CaptureDateSaveOracle.java", "CaptureDateImageOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observers: %v\n%s", err, out)
	}
	replay := func(class, reference string, extra ...string) {
		t.Helper()
		path := filepath.Join(dir, class+".json")
		args := []string{"--enable-native-access=ALL-UNNAMED", "-cp", jar + string(os.PathListSeparator) + dir, "lrtlprobe." + class, path}
		args = append(args, extra...)
		if out, err := exec.Command("java", args...).CombinedOutput(); err != nil {
			t.Fatalf("native observer: %v\n%s", err, out)
		}
		var got, want any
		if err := json.Unmarshal(denoiseRead(t, path), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(denoiseRead(t, reference), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("native observations changed", class)
		}
	}
	replay("CaptureDateCacheOracle", "testdata/lrt-capture-date-cache-7.5.5.json")
	replay("CaptureDateSaveOracle", "testdata/lrt-capture-date-save-7.5.5.json")
	tool := os.Getenv("LRTL_EXIFTOOL")
	if tool == "" {
		return
	}
	images := filepath.Join(dir, "images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, row := range captureDateImages(t).Cases {
		path := filepath.Join(images, row.Name)
		if err := os.WriteFile(path, row.Data, 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	var csvInput bytes.Buffer
	writer := csv.NewWriter(&csvInput)
	for start := 0; start < len(paths); start += 50 {
		end := start + 50
		if end > len(paths) {
			end = len(paths)
		}
		args := append([]string{"-csv", "-f", "-DateTimeOriginal", "-CreateDate", "-SubSecDateTimeOriginal"}, paths[start:end]...)
		output, err := exec.Command(tool, args...).Output()
		if err != nil {
			t.Fatal("ExifTool date replay", err)
		}
		rows, err := csv.NewReader(bytes.NewReader(output)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows[1:] {
			columns := map[string]string{}
			for i, header := range rows[0] {
				columns[header] = row[i]
			}
			fields := make([]string, 16)
			for i := range fields {
				fields[i] = "-"
			}
			fields[0], fields[1], fields[2], fields[9], fields[12] = columns["SourceFile"], "64x32", columns["DateTimeOriginal"], columns["CreateDate"], columns["SubSecDateTimeOriginal"]
			if err := writer.Write(fields); err != nil {
				t.Fatal(err)
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "metadata.csv")
	if err := os.WriteFile(input, csvInput.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	replay("CaptureDateImageOracle", "testdata/lrt-capture-date-images-7.5.5.json", input)
}

func FuzzCaptureDateImage(f *testing.F) {
	for _, row := range captureDateImages(f).Cases {
		f.Add(row.Data)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		ext := ".tif"
		if len(data) >= 2 && data[0] == 255 && data[1] == 216 {
			ext = ".jpg"
		}
		if len(data) >= 2 && data[0] == 137 && data[1] == 'P' {
			ext = ".png"
		}
		path := filepath.Join(dir, "date"+ext)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		date, found, err := readNativeCaptureDate(path)
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("changed image")
		}
		if err == nil && found && len(utf16.Encode([]rune(date))) > 22 {
			t.Fatal("unbounded native date cache")
		}
	})
}

type captureDateImageRow struct {
	Name       string
	Data       []byte
	Date       string
	SeededDate string `json:"seeded_date"`
}
type captureDateImageReference struct {
	Completed bool `json:"metadata_completed"`
	Cases     []captureDateImageRow
}

func captureDateImages(t testing.TB) captureDateImageReference {
	t.Helper()
	var reference captureDateImageReference
	data, err := os.ReadFile("testdata/lrt-capture-date-images-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 309 {
		t.Fatal("invalid image date observations")
	}
	return reference
}

func TestCaptureDateCacheNative(t *testing.T) {
	var reference captureDateCacheReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-capture-date-cache-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 440 {
		t.Fatal("invalid cache observations")
	}
	for i, row := range reference.Cases {
		if got := nativeCaptureDateFields(row.Seed, row.Date, row.Subsecond, row.Fallback); got != row.Expected {
			t.Errorf("case %d: got %q want %q", i, got, row.Expected)
		}
	}
}

func TestCaptureDateImagesNative(t *testing.T) {
	for _, row := range captureDateImages(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			if filepath.Base(row.Name) != row.Name || strings.Contains(row.Name, ":") {
				t.Fatal("unsafe image name")
			}
			path := filepath.Join(t.TempDir(), row.Name)
			if err := os.WriteFile(path, row.Data, 0600); err != nil {
				t.Fatal(err)
			}
			date, available, err := readNativeCaptureDate(path)
			if err != nil || date != row.Date || available != (row.Date != "n/a") {
				t.Fatalf("got %q, available %t, err %v; want %q", date, available, err, row.Date)
			}
			seed := "2031-01-01T00:00:00"
			date, _, err = readNativeCaptureDateWithSeed(path, &seed)
			if err != nil || date != row.SeededDate {
				t.Fatalf("seeded cache got %q want %q err %v", date, row.SeededDate, err)
			}
		})
	}
}

func TestCaptureDateSaveNative(t *testing.T) {
	var reference struct {
		Completed bool `json:"store_completed"`
		Stop      string
		Cases     []struct {
			Quality       string
			Active        bool
			Date          *string
			Target, Saved string
			Clock         bool `json:"metadata_date_written"`
		}
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-capture-date-save-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.Completed || len(reference.Cases) != 20 || !strings.Contains(reference.Stop, "history") {
		t.Fatal("invalid pre-history save observations")
	}
	for _, row := range reference.Cases {
		packet, err := parseXMP([]byte(row.Target))
		if err != nil {
			t.Fatal(err)
		}
		date := ""
		if row.Date != nil {
			date = *row.Date
		}
		if err := applyNativeCaptureDate(packet, date, row.Active); err != nil {
			t.Fatal(err)
		}
		expected, err := parseXMP([]byte(row.Saved))
		if err != nil {
			t.Fatal(err)
		}
		for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate", "photoshop:DateCreated"} {
			got, found, err := captureTimestampScalar(packet, property)
			if err != nil {
				t.Fatal(err)
			}
			want, present, err := captureTimestampScalar(expected, property)
			if err != nil {
				t.Fatal(err)
			}
			if got != want || found != present {
				t.Fatalf("%s %s date=%q active=%t: got %q want %q", row.Quality, property, date, row.Active, got, want)
			}
		}
		if row.Clock != (row.Active && date != "") {
			t.Fatal("unexpected native clock-write condition")
		}
	}
}
