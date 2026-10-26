package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
)

// dngTestLosslessJPEG writes small SOF3 streams independently of the decoder.
// Its fixed canonical table assigns one five-bit code to each difference
// category, keeping fixture bytes deterministic and easy to inspect.
func dngTestLosslessJPEG(t testing.TB, width, height, precision, predictor, pointTransform, restart int, samples []uint16) (full, tables, block []byte) {
	t.Helper()
	if len(samples) != width*height || predictor < 1 || predictor > 7 || pointTransform < 0 || pointTransform >= precision {
		t.Fatal("invalid test lossless JPEG parameters")
	}
	if restart != 0 && restart%width != 0 {
		t.Fatal("JPEG lossless restart intervals must align to complete rows")
	}
	var huffmanPayload [34]byte
	huffmanPayload[0] = 0    // DC table 0
	huffmanPayload[1+4] = 17 // Categories 0 through 16 have length-five codes.
	for category := 0; category <= 16; category++ {
		huffmanPayload[17+category] = byte(category)
	}
	dht := dngTestJPEGSegment(0xc4, huffmanPayload[:])
	frame := dngTestJPEGSegment(0xc3, []byte{
		byte(precision), byte(height >> 8), byte(height), byte(width >> 8), byte(width),
		1, 1, 0x11, 0,
	})
	var restartSegment []byte
	if restart != 0 {
		restartSegment = dngTestJPEGSegment(0xdd, []byte{byte(restart >> 8), byte(restart)})
	}
	scan := dngTestJPEGSegment(0xda, []byte{1, 1, 0, byte(predictor), 0, byte(pointTransform)})
	var writer dngTestJPEGBitWriter
	initial := 1 << uint(precision-pointTransform-1)
	modulus := 1 << uint(precision-pointTransform)
	restartSequence := 0
	for index, original := range samples {
		afterRestart := restart != 0 && index != 0 && index%restart == 0
		if afterRestart {
			writer.flushOnes()
			writer.data = append(writer.data, 0xff, byte(0xd0+restartSequence))
			restartSequence = (restartSequence + 1) & 7
		}
		x, y := index%width, index/width
		prediction := initial
		if index != 0 && !afterRestart {
			rowInInterval := index
			if restart != 0 {
				rowInInterval %= restart
			}
			if y == 0 || (restart != 0 && rowInInterval < width) {
				prediction = int(samples[index-1] >> uint(pointTransform))
			} else if x == 0 {
				prediction = int(samples[index-width] >> uint(pointTransform))
			} else {
				ra := int(samples[index-1] >> uint(pointTransform))
				rb := int(samples[index-width] >> uint(pointTransform))
				rc := int(samples[index-width-1] >> uint(pointTransform))
				prediction = dngTestJPEGSpecPredictor(predictor, ra, rb, rc)
			}
		}
		sample := int(original >> uint(pointTransform))
		difference := sample - prediction
		if difference < -32768 {
			difference += 1 << 16
		} else if difference > 32767 {
			difference -= 1 << 16
		}
		if difference < -modulus || difference >= modulus {
			t.Fatalf("test difference %d exceeds coding precision", difference)
		}
		category, amplitude := 0, 0
		category = dngTestJPEGCategory(difference)
		if difference < 0 {
			amplitude = difference + (1 << uint(category)) - 1
		} else {
			amplitude = difference
		}
		writer.writeBits(uint32(category), 5)
		if category != 0 && category != 16 {
			writer.writeBits(uint32(amplitude), category)
		}
	}
	writer.flushOnes()
	entropy := writer.data
	tables = append([]byte{0xff, 0xd8}, dht...)
	tables = append(tables, 0xff, 0xd9)
	block = []byte{0xff, 0xd8}
	block = append(block, frame...)
	block = append(block, restartSegment...)
	block = append(block, scan...)
	block = append(block, entropy...)
	block = append(block, 0xff, 0xd9)
	full = []byte{0xff, 0xd8}
	full = append(full, dht...)
	full = append(full, frame...)
	full = append(full, restartSegment...)
	full = append(full, scan...)
	full = append(full, entropy...)
	full = append(full, 0xff, 0xd9)
	return full, tables, block
}

func dngTestJPEGSegment(marker byte, payload []byte) []byte {
	length := len(payload) + 2
	segment := []byte{0xff, marker, byte(length >> 8), byte(length)}
	return append(segment, payload...)
}

type dngTestJPEGBitWriter struct {
	data []byte
	byte byte
	bits uint8
}

func (writer *dngTestJPEGBitWriter) writeBits(value uint32, count int) {
	for bit := count - 1; bit >= 0; bit-- {
		writer.byte = writer.byte<<1 | byte((value>>uint(bit))&1)
		writer.bits++
		if writer.bits == 8 {
			writer.appendEntropyByte(writer.byte)
			writer.byte, writer.bits = 0, 0
		}
	}
}

func (writer *dngTestJPEGBitWriter) flushOnes() {
	for writer.bits != 0 {
		writer.byte = writer.byte<<1 | 1
		writer.bits++
		if writer.bits == 8 {
			writer.appendEntropyByte(writer.byte)
			writer.byte, writer.bits = 0, 0
		}
	}
}

func (writer *dngTestJPEGBitWriter) appendEntropyByte(value byte) {
	writer.data = append(writer.data, value)
	if value == 0xff {
		writer.data = append(writer.data, 0)
	}
}

func dngTestJPEGCategory(difference int) int {
	if difference < 0 {
		difference = -difference
	}
	category := 0
	for difference != 0 {
		difference >>= 1
		category++
	}
	return category
}

func dngTestJPEGSpecPredictor(selector, ra, rb, rc int) int {
	switch selector {
	case 1:
		return ra
	case 2:
		return rb
	case 3:
		return rc
	case 4:
		return ra + rb - rc
	case 5:
		return ra + ((rb - rc) >> 1)
	case 6:
		return rb + ((ra - rc) >> 1)
	default:
		return (ra + rb) >> 1
	}
}

func TestDNGLosslessJPEGAllPredictorsAndTables(t *testing.T) {
	values := []uint16{14, 17, 25, 30, 16, 22, 35, 42, 5, 13, 33, 55}
	for predictor := 1; predictor <= 7; predictor++ {
		t.Run(fmt.Sprintf("predictor-%d", predictor), func(t *testing.T) {
			jpegData, _, _ := dngTestLosslessJPEG(t, 4, 3, 8, predictor, 0, 0, values)
			mask, err := dngTestSemanticMask(t, binary.LittleEndian, 4, 3, 8, 7, 1, 1, 0, 0, jpegData, nil)
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range values {
				want := float32(float64(value) / 255)
				if mask.values[index] != want {
					t.Fatalf("sample %d=%g want %g", index, mask.values[index], want)
				}
			}
		})
	}

	t.Run("TIFF-JPEGTables-and-point-transform", func(t *testing.T) {
		values := []uint16{40, 44, 52, 60, 44, 56, 72, 84, 24, 40, 72, 100}
		_, tables, block := dngTestLosslessJPEG(t, 4, 3, 8, 5, 2, 0, values)
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			mask, err := dngTestSemanticMask(t, order, 4, 3, 8, 7, 1, 1, 0, 0, block, nil, tables)
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range values {
				if got, want := mask.values[index], float32(float64(value)/255); got != want {
					t.Fatalf("%s-endian sample %d=%g want %g", orderName(order), index, got, want)
				}
			}
		}
	})

	t.Run("16-bit-and-restart-interval", func(t *testing.T) {
		values := []uint16{12000, 12008, 12020, 12040, 12012, 12024, 12044, 12068, 11980, 12000, 12028, 12060}
		jpegData, _, _ := dngTestLosslessJPEG(t, 4, 3, 16, 6, 0, 4, values)
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			mask, err := dngTestSemanticMask(t, order, 4, 3, 16, 7, 1, 1, 0, 0, jpegData, nil)
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range values {
				if got, want := mask.values[index], float32(float64(value)/65535); got != want {
					t.Fatalf("%s-endian sample %d=%g want %g", orderName(order), index, got, want)
				}
			}
		}
	})

	t.Run("16-bit-category-16", func(t *testing.T) {
		values := []uint16{0, 65535}
		jpegData, _, _ := dngTestLosslessJPEG(t, 2, 1, 16, 1, 0, 0, values)
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			mask, err := dngTestSemanticMask(t, order, 2, 1, 16, 7, 1, 1, 0, 0, jpegData, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mask.values[0] != 0 || mask.values[1] != 1 {
				t.Fatalf("%s-endian category-16 samples=%v, want [0 1]", orderName(order), mask.values)
			}
		}
	})

	t.Run("padded-tile", func(t *testing.T) {
		jpegData, _, _ := dngTestLosslessJPEG(t, 2, 2, 8, 3, 0, 0, []uint16{0, 255, 32, 64})
		mask, err := dngTestSemanticMask(t, binary.LittleEndian, 2, 1, 8, 7, 1, 1, 2, 2, jpegData, nil)
		if err != nil {
			t.Fatal(err)
		}
		if mask.values[0] != 0 || mask.values[1] != 1 {
			t.Fatalf("padded tile samples=%v, want [0 1]", mask.values)
		}
	})
}

func orderName(order binary.ByteOrder) string {
	if order == binary.BigEndian {
		return "big"
	}
	return "little"
}

func TestDNGLosslessJPEGPointTransformAndRestartFailures(t *testing.T) {
	values := []uint16{200, 204, 212, 220, 204, 216, 232, 244, 184, 200, 228, 252}
	jpegData, _, _ := dngTestLosslessJPEG(t, 4, 3, 8, 7, 2, 4, values)
	raw, err := decodeDNGLosslessJPEG(jpegData, nil, 4, 3, 8, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{200, 204, 212, 220, 204, 216, 232, 244, 184, 200, 228, 252}) {
		t.Fatalf("decoded point-transform samples %v", raw)
	}
	corrupt := append([]byte(nil), jpegData...)
	marker := bytes.Index(corrupt, []byte{0xff, 0xd0})
	if marker < 0 {
		t.Fatal("fixture has no restart marker")
	}
	corrupt[marker+1] = 0xd1
	if _, err := decodeDNGLosslessJPEG(corrupt, nil, 4, 3, 8, binary.LittleEndian); err == nil {
		t.Fatal("wrong restart sequence was accepted")
	}
	badInterval := append([]byte(nil), jpegData...)
	dri := bytes.Index(badInterval, []byte{0xff, 0xdd})
	if dri < 0 {
		t.Fatal("fixture has no restart interval segment")
	}
	badInterval[dri+4], badInterval[dri+5] = 0, 3
	if _, err := decodeDNGLosslessJPEG(badInterval, nil, 4, 3, 8, binary.LittleEndian); err == nil {
		t.Fatal("restart interval that splits rows was accepted")
	}
}

func TestDNGLosslessJPEGStuffedBytesAndMalformedInput(t *testing.T) {
	rng := rand.New(rand.NewSource(7831))
	var jpegData []byte
	for attempt := 0; attempt < 100; attempt++ {
		values := make([]uint16, 128)
		for index := range values {
			values[index] = uint16(rng.Intn(256))
		}
		jpegData, _, _ = dngTestLosslessJPEG(t, len(values), 1, 8, 1, 0, 0, values)
		if bytes.Contains(jpegData, []byte{0xff, 0x00}) {
			break
		}
	}
	if !bytes.Contains(jpegData, []byte{0xff, 0x00}) {
		t.Fatal("deterministic JPEG fixture did not exercise entropy byte stuffing")
	}
	if _, err := decodeDNGLosslessJPEG(jpegData, nil, 128, 1, 8, binary.LittleEndian); err != nil {
		t.Fatalf("decode stuffed entropy bytes: %v", err)
	}
	for name, malformed := range map[string][]byte{
		"truncated entropy": jpegData[:len(jpegData)-4],
		"oversubscribed table": func() []byte {
			copyData := append([]byte(nil), jpegData...)
			// The first DHT has its 16 code-length counts after marker, length, selector.
			copyData[11] = 255
			return copyData
		}(),
		"bad frame dimensions": append([]byte(nil), jpegData...),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "bad frame dimensions" {
				// The frame dimensions are checked against the TIFF mask before decoding.
				if _, err := decodeDNGLosslessJPEG(malformed, nil, 127, 1, 8, binary.LittleEndian); err == nil {
					t.Fatal("mismatched frame dimensions were accepted")
				}
				return
			}
			if _, err := decodeDNGLosslessJPEG(malformed, nil, 128, 1, 8, binary.LittleEndian); err == nil {
				t.Fatal("malformed JPEG stream was accepted")
			}
		})
	}
}

func TestDNGLosslessJPEGPredictorRounding(t *testing.T) {
	want := []int{4, 2, 5, 1, 2, 1, 3}
	for selector := 1; selector <= 7; selector++ {
		if got := dngLosslessPredictor(selector, 4, 2, 5); got != want[selector-1] {
			t.Errorf("predictor %d = %d, want %d", selector, got, want[selector-1])
		}
	}
}

func TestDNGLosslessJPEGReferenceFixture(t *testing.T) {
	// SOF3 stream generated by github.com/cocosip/go-dicom-codecs v0.8.2 for
	// the known 8-bit row [0, 64, 128, 192, 255]. Keeping bytes here avoids a
	// runtime test dependency and gives the decoder an independently encoded fixture.
	encoded, err := hex.DecodeString("ffd8ffe000104a46494600010100000100010000ffc3000b080001000501011100ffc400160001010100000000000000000000000000070608ffda0008010100010000cfe8080817ff00ffd9")
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeDNGLosslessJPEG(encoded, nil, 5, 1, 8, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0, 64, 128, 192, 255}; !bytes.Equal(got, want) {
		t.Fatalf("decoded reference fixture %v, want %v", got, want)
	}

	// Independently encoded interleaved SOF3 frame with three components.
	rgb, err := hex.DecodeString("ffd8ffe000104a46494600010100000100010000ffc30011080002000203011100021100031100ffc4001800000301010000000000000000000000000607080405ffda000c030100020003000400009fd26a7601e1e501415047fdecbfffd9")
	if err != nil {
		t.Fatal(err)
	}
	got, err = decodeDNGJPEGLossless(rgb, nil, 2, 2, 3, 8, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0, 20, 40, 64, 80, 100, 128, 150, 170, 255, 240, 220}; !bytes.Equal(got, want) {
		t.Fatalf("decoded interleaved RGB reference fixture %v, want %v", got, want)
	}
}

func FuzzDNGLosslessJPEG(f *testing.F) {
	seed, _, _ := dngTestLosslessJPEG(f, 2, 1, 8, 1, 0, 0, []uint16{12, 20})
	f.Add(seed)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = decodeDNGLosslessJPEG(data, nil, 2, 1, 8, binary.LittleEndian)
	})
}
