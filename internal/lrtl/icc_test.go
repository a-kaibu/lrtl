package lrtl

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func testICCProfile(kind, text string) []byte {
	var data []byte
	switch kind {
	case "desc":
		data = make([]byte, 12)
		copy(data, kind)
		binary.BigEndian.PutUint32(data[8:], uint32(len(text)+1))
		data = append(data, []byte(text)...)
		data = append(data, 0)
	case "text":
		data = append([]byte("text\x00\x00\x00\x00"), []byte(text)...)
	case "mluc", "frFR", "ENus":
		data = make([]byte, 28)
		copy(data, "mluc")
		binary.BigEndian.PutUint32(data[8:], 1)
		binary.BigEndian.PutUint32(data[12:], 12)
		lang := kind
		if kind == "mluc" {
			lang = "enUS"
		}
		copy(data[16:], lang)
		units := utf16.Encode([]rune(text))
		binary.BigEndian.PutUint32(data[20:], uint32(len(units)*2))
		binary.BigEndian.PutUint32(data[24:], 28)
		for _, u := range units {
			data = binary.BigEndian.AppendUint16(data, u)
		}
	}
	p := make([]byte, 144)
	copy(p[12:], "mntrRGB XYZ ")
	p[8] = 4
	copy(p[36:], "acsp")
	binary.BigEndian.PutUint32(p[128:], 1)
	copy(p[132:], "desc")
	binary.BigEndian.PutUint32(p[136:], 144)
	binary.BigEndian.PutUint32(p[140:], uint32(len(data)))
	p = append(p, data...)
	binary.BigEndian.PutUint32(p, uint32(len(p)))
	return p
}

func testICCJPEG(profile []byte, parts int) []byte {
	b := []byte{255, 216}
	for i := parts; i > 0; i-- {
		p := append([]byte("ICC_PROFILE\x00"), byte(i), byte(parts))
		p = append(p, profile[(i-1)*len(profile)/parts:i*len(profile)/parts]...)
		b = append(b, 255, 226)
		b = binary.BigEndian.AppendUint16(b, uint16(len(p)+2))
		b = append(b, p...)
	}
	return append(b, 255, 217)
}

func testICCChunk(kind string, payload []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	b = append(b, kind...)
	b = append(b, payload...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b[4:]))
}

func testICCPNG(profile []byte) []byte {
	var compressed bytes.Buffer
	z := zlib.NewWriter(&compressed)
	z.Write(profile)
	z.Close()
	payload := append([]byte("profile\x00\x00"), compressed.Bytes()...)
	b := append([]byte("\x89PNG\r\n\x1a\n"), testICCChunk("iCCP", payload)...)
	return append(b, testICCChunk("IEND", nil)...)
}

func testICCTIFF(profile []byte, order binary.ByteOrder, big bool) []byte {
	var b []byte
	if big {
		b = make([]byte, 52)
		order.PutUint16(b[2:], 43)
		order.PutUint16(b[4:], 8)
		order.PutUint64(b[8:], 16)
		order.PutUint64(b[16:], 1)
		order.PutUint16(b[24:], 34675)
		order.PutUint16(b[26:], 7)
		order.PutUint64(b[28:], uint64(len(profile)))
		order.PutUint64(b[36:], 52)
	} else {
		b = make([]byte, 26)
		order.PutUint16(b[2:], 42)
		order.PutUint32(b[4:], 8)
		order.PutUint16(b[8:], 1)
		order.PutUint16(b[10:], 34675)
		order.PutUint16(b[12:], 7)
		order.PutUint32(b[14:], uint32(len(profile)))
		order.PutUint32(b[18:], 26)
	}
	if order == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}
	return append(b, profile...)
}

func TestICCDescriptionContainers(t *testing.T) {
	for _, kind := range []string{"desc", "text", "mluc", "frFR", "ENus"} {
		t.Run(kind, func(t *testing.T) {
			want := "ITU-R BT.2020 PQ"
			if kind == "frFR" {
				want = ""
			}
			profile := testICCProfile(kind, "  ITU-R BT.2020 PQ\r\n")
			inputs := map[string][]byte{"jpeg": testICCJPEG(profile, 3), "png": testICCPNG(profile), "tiff-le": testICCTIFF(profile, binary.LittleEndian, false), "tiff-be": testICCTIFF(profile, binary.BigEndian, false), "bigtiff-le": testICCTIFF(profile, binary.LittleEndian, true), "bigtiff-be": testICCTIFF(profile, binary.BigEndian, true)}
			for name, data := range inputs {
				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "image")
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					got, err := readImageICCDescription(path)
					if err != nil || got != want {
						t.Fatalf("got %q, %v; want %q", got, err, want)
					}
				})
			}
		})
	}
}

func TestICCExifToolOracle(t *testing.T) {
	exe := os.Getenv("LRTL_ICC_EXIFTOOL")
	if exe == "" {
		t.Skip("set LRTL_ICC_EXIFTOOL for independent ICC parser comparison")
	}
	for _, kind := range []string{"desc", "text", "mluc", "frFR", "ENus"} {
		for _, text := range []string{"sRGB2014", "ITU-R BT.2020 Reference Display", "  ITU-R BT.2020 PQ\r\n", "日本語 2020 PQ"} {
			t.Run(kind+"/"+text, func(t *testing.T) {
				profile := testICCProfile(kind, text)
				path := filepath.Join(t.TempDir(), "profile.icc")
				if err := os.WriteFile(path, profile, 0600); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command(exe, "-b", "-ICC_Profile:ProfileDescription", path, "-m").Output()
				if err != nil {
					t.Fatal(err)
				}
				want := strings.TrimFunc(strings.ReplaceAll(string(out), "\r\n", " "), func(r rune) bool { return r <= 32 })
				got, err := iccDescription(profile)
				if err != nil || got != want {
					t.Fatalf("Go=%q/%v; ExifTool=%q", got, err, want)
				}
			})
		}
	}
}

func TestICCMalformed(t *testing.T) {
	p := testICCProfile("desc", "2020 PQ")
	for i := 0; i < len(p); i++ {
		if _, err := iccDescription(p[:i]); err == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
	for _, offset := range []int{0, 128, 136, 140, 152} {
		q := bytes.Clone(p)
		binary.BigEndian.PutUint32(q[offset:], 0xffffffff)
		if _, err := iccDescription(q); err == nil {
			t.Fatalf("accepted invalid size at %d", offset)
		}
	}
	jpeg := testICCJPEG(p, 3)
	jpeg[17] = 4
	if _, err := readJPEGICC(bytes.NewReader(jpeg)); err == nil {
		t.Fatal("accepted inconsistent chunks")
	}
	png := testICCPNG(p)
	png[len(png)-13] ^= 1
	if _, err := readPNGICC(bytes.NewReader(png)); err == nil {
		t.Fatal("accepted broken CRC")
	}
}

func FuzzICCDescription(f *testing.F) {
	for _, kind := range []string{"desc", "text", "mluc"} {
		f.Add(testICCProfile(kind, "2020 PQ"))
	}
	f.Fuzz(func(t *testing.T, p []byte) { _, _ = iccDescription(p) })
}

func TestICCContainerFailures(t *testing.T) {
	p := testICCProfile("mluc", "2020 PQ")
	for _, encoded := range [][]byte{testICCJPEG(p, 3), testICCPNG(p)} {
		for i := 0; i < len(encoded)-2; i++ {
			var err error
			if encoded[0] == 255 {
				_, err = readJPEGICC(bytes.NewReader(encoded[:i]))
			} else {
				_, err = readPNGICC(bytes.NewReader(encoded[:i]))
			}
			// A complete iCCP already provides the profile, without needing IEND.
			if encoded[0] != 255 && i >= len(encoded)-12 {
				continue
			}
			if err == nil {
				t.Fatalf("accepted truncated container at %d/%d", i, len(encoded))
			}
		}
	}
	for _, big := range []bool{false, true} {
		data := testICCTIFF(p, binary.LittleEndian, big)
		if big {
			binary.LittleEndian.PutUint64(data[36:], ^uint64(0))
		} else {
			binary.LittleEndian.PutUint32(data[18:], ^uint32(0))
		}
		path := filepath.Join(t.TempDir(), "bad.tif")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readImageICCDescription(path); err == nil {
			t.Fatal("accepted out-of-bounds TIFF ICC")
		}
	}
	// Bounded decompression also applies when the compressed chunk itself is tiny.
	var compressed bytes.Buffer
	z := zlib.NewWriter(&compressed)
	block := make([]byte, 1<<20)
	for i := 0; i < 33; i++ {
		if _, err := z.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	data := append([]byte("\x89PNG\r\n\x1a\n"), testICCChunk("iCCP", append([]byte("profile\x00\x00"), compressed.Bytes()...))...)
	if _, err := readPNGICC(bytes.NewReader(data)); err == nil {
		t.Fatal("accepted oversized decompressed ICC")
	}
}

func TestICCInstalledProfiles(t *testing.T) {
	exe := os.Getenv("LRTL_ICC_EXIFTOOL")
	if exe == "" {
		t.Skip("set LRTL_ICC_EXIFTOOL for optional installed profile observations")
	}
	for name, want := range map[string]string{"REC2020.icc": "ITU-R BT.2020 Reference Display", "REC709.icc": "ITU-R BT.709 Reference Display", "sRGB.icc": "sRGB2014"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(filepath.Dir(exe), "profiles", name)
			p, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				t.Skip("installed profile unavailable")
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := iccDescription(p)
			if err != nil || got != want {
				t.Fatalf("Go=%q/%v want=%q", got, err, want)
			}
			out, err := exec.Command(exe, "-b", "-ICC_Profile:ProfileDescription", path, "-m").Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(out)) != want {
				t.Fatalf("ExifTool=%q want=%q", out, want)
			}
		})
	}
}

func TestICCTIFFDirectoryLinks(t *testing.T) {
	read := func(t *testing.T, data []byte) (string, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "image.tif")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return readImageICCDescription(path)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, big := range []bool{false, true} {
			for _, sub := range []bool{false, true} {
				original := testICCTIFF(testICCProfile("desc", "2020 PQ"), order, big)
				var data []byte
				if big {
					root, child := bytes.Clone(original[:52]), bytes.Clone(original[16:])
					order.PutUint16(root[24:], 256)
					order.PutUint16(root[26:], 18)
					order.PutUint64(root[28:], 1)
					order.PutUint64(root[36:], 1)
					if sub {
						order.PutUint16(root[24:], 330)
						order.PutUint64(root[36:], 52)
					} else {
						order.PutUint64(root[44:], 52)
					}
					order.PutUint64(child[20:], 88)
					data = append(root, child...)
				} else {
					root, child := bytes.Clone(original[:26]), bytes.Clone(original[8:])
					order.PutUint16(root[10:], 256)
					order.PutUint16(root[12:], 4)
					order.PutUint32(root[14:], 1)
					order.PutUint32(root[18:], 1)
					if sub {
						order.PutUint16(root[10:], 330)
						order.PutUint32(root[18:], 26)
					} else {
						order.PutUint32(root[22:], 26)
					}
					order.PutUint32(child[10:], 44)
					data = append(root, child...)
				}
				if got, err := read(t, data); err != nil || got != "2020 PQ" {
					t.Fatalf("big=%v sub=%v: %q/%v", big, sub, got, err)
				}
			}
		}
	}
	// A loop is visited once; a very deep noncyclic graph is rejected.
	cycle := make([]byte, 14)
	copy(cycle, "II")
	binary.LittleEndian.PutUint16(cycle[2:], 42)
	binary.LittleEndian.PutUint32(cycle[4:], 8)
	binary.LittleEndian.PutUint32(cycle[10:], 8)
	if got, err := read(t, cycle); err != nil || got != "" {
		t.Fatalf("cycle: %q/%v", got, err)
	}
	deep := make([]byte, 8+35*6)
	copy(deep, cycle[:8])
	for i := 0; i < 34; i++ {
		binary.LittleEndian.PutUint32(deep[10+i*6:], uint32(14+i*6))
	}
	if _, err := read(t, deep); err == nil {
		t.Fatal("accepted excessive directory depth")
	}
}

func FuzzICCContainers(f *testing.F) {
	p := testICCProfile("mluc", "2020 PQ")
	f.Add(testICCJPEG(p, 3))
	f.Add(testICCPNG(p))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		if len(b) > 1 && b[0] == 255 {
			_, _ = readJPEGICC(bytes.NewReader(b))
		} else {
			_, _ = readPNGICC(bytes.NewReader(b))
		}
	})
}
