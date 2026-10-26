package lrtl

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func dngTestCFAMetadata(order binary.ByteOrder, white uint32) map[uint16]dngTestField {
	metadata := dngRGBIdentityCameraMetadata(order, nil)
	metadata[262] = dngTestShort(order, 32803)
	metadata[33421] = dngTestShort(order, 2, 2)
	metadata[33422] = dngTestField{typeID: 1, count: 4, data: []byte{0, 1, 1, 2}}
	metadata[50710] = dngTestField{typeID: 1, count: 3, data: []byte{0, 1, 2}}
	metadata[50711] = dngTestShort(order, 1)
	metadata[50717] = dngTestLong(order, white, white, white)
	return metadata
}

func dngTestBayerPixels(width, height int, red, green, blue uint16) []uint16 {
	return dngTestCFAPixels(width, height, []byte{0, 1, 1, 2}, red, green, blue)
}

func dngTestCFAPixels(width, height int, pattern []byte, red, green, blue uint16) []uint16 {
	return dngTestCFAPixelsAtOrigin(width, height, 0, 0, pattern, red, green, blue)
}

func dngTestCFAPixelsAtOrigin(width, height, originX, originY int, pattern []byte, red, green, blue uint16) []uint16 {
	pixels := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			switch pattern[(positiveMod(y-originY, 2))*2+positiveMod(x-originX, 2)] {
			case 0:
				pixels[y*width+x] = red
			case 1:
				pixels[y*width+x] = green
			case 2:
				pixels[y*width+x] = blue
			}
		}
	}
	return pixels
}

func dngTestLinearRGBPixels(width, height int, red, green, blue uint16) []uint16 {
	pixels := make([]uint16, width*height*3)
	for index := 0; index < width*height; index++ {
		pixels[index*3], pixels[index*3+1], pixels[index*3+2] = red, green, blue
	}
	return pixels
}

func dngTestPackedCFADNG(t *testing.T, order binary.ByteOrder, width, height, depth, compression, predictor int, pixels []uint16, metadata map[uint16]dngTestField) []byte {
	t.Helper()
	mask := uint16((uint32(1) << uint(depth)) - 1)
	rowBytes := (width*depth + 7) / 8
	packed := make([]byte, rowBytes*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := pixels[y*width+x]
			if value > mask {
				t.Fatalf("test sample %d exceeds %d-bit precision", value, depth)
			}
			if predictor == 2 && x > 0 {
				value = (value - pixels[y*width+x-1]) & mask
			}
			bitOffset := x * depth
			for bit := 0; bit < depth; bit++ {
				if value&(uint16(1)<<uint(depth-bit-1)) != 0 {
					position := bitOffset + bit
					packed[y*rowBytes+position/8] |= byte(1 << uint(7-position%8))
				}
			}
		}
	}
	if compression == 8 {
		var encoded bytes.Buffer
		writer := zlib.NewWriter(&encoded)
		if _, err := writer.Write(packed); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		packed = encoded.Bytes()
	}
	fields := make(map[uint16]dngTestField, len(metadata)+3)
	for tag, field := range metadata {
		fields[tag] = field
	}
	fields[258] = dngTestShort(order, uint16(depth))
	fields[259] = dngTestShort(order, uint16(compression))
	fields[317] = dngTestShort(order, uint16(predictor))
	data := linearDNGFixture(order, width, height, 1, pixels, fields)
	blockOffset := len(data)
	data = append(data, packed...)
	for index := 0; index < int(order.Uint16(data[8:10])); index++ {
		entry := 10 + index*12
		switch order.Uint16(data[entry:]) {
		case 273:
			order.PutUint32(data[entry+8:], uint32(blockOffset))
		case 279:
			order.PutUint32(data[entry+8:], uint32(len(packed)))
		}
	}
	return data
}

func TestCFADNGBayerDevelopMatchesLinearDNGReference(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(orderName(order), func(t *testing.T) {
			const width, height = 8, 8
			const red, green, blue, white = 10000, 20000, 30000, 65535
			metadata := dngTestCFAMetadata(order, white)
			cfa := linearDNGFixture(order, width, height, 1, dngTestBayerPixels(width, height, red, green, blue), metadata)
			rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
			rgbMetadata[50717] = dngTestLong(order, white, white, white)
			reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)

			got, err := decodeLinearDNG(cfa)
			if err != nil {
				t.Fatalf("decode CFA DNG: %v", err)
			}
			want, err := decodeLinearDNG(reference)
			if err != nil {
				t.Fatalf("decode LinearRaw reference: %v", err)
			}
			assertLinearDNGImagesEqual(t, got, want)

			root := t.TempDir()
			source := filepath.Join(root, "bayer.dng")
			if err := os.WriteFile(source, cfa, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", t.TempDir())
			t.Setenv("ProgramFiles", t.TempDir())
			t.Setenv("ProgramFiles(x86)", t.TempDir())
			gotTIFF, config, format, err := readRenderInputWithOptions(Frame{Image: source}, imageInputOptions{RawDevelop: true})
			if err != nil {
				t.Fatalf("develop CFA DNG without an external converter: %v", err)
			}
			wantTIFF, err := encodeDevelopedDNGTIFF(want, 1)
			if err != nil {
				t.Fatal(err)
			}
			if format != "tiff" || config.Width != width || config.Height != height || !bytes.Equal(gotTIFF, wantTIFF) {
				t.Fatalf("CFA raw-develop result format=%s config=%+v matches reference=%t", format, config, bytes.Equal(gotTIFF, wantTIFF))
			}
		})
	}
}

func TestCFADNGSupportsAllBayerPhases(t *testing.T) {
	patterns := map[string][]byte{
		"RGGB": {0, 1, 1, 2},
		"BGGR": {2, 1, 1, 0},
		"GRBG": {1, 0, 2, 1},
		"GBRG": {1, 2, 0, 1},
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for name, cfaPattern := range patterns {
			t.Run(orderName(order)+"/"+name, func(t *testing.T) {
				const width, height = 8, 8
				const red, green, blue, white = 10000, 20000, 30000, 65535
				metadata := dngTestCFAMetadata(order, white)
				metadata[33422] = dngTestField{typeID: 1, count: 4, data: cfaPattern}
				cfa := linearDNGFixture(order, width, height, 1, dngTestCFAPixels(width, height, cfaPattern, red, green, blue), metadata)
				rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
				rgbMetadata[50717] = dngTestLong(order, white, white, white)
				reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)
				got, err := decodeLinearDNG(cfa)
				if err != nil {
					t.Fatalf("decode CFA DNG: %v", err)
				}
				want, err := decodeLinearDNG(reference)
				if err != nil {
					t.Fatalf("decode LinearRaw reference: %v", err)
				}
				assertLinearDNGImagesEqual(t, got, want)
			})
		}
	}
}

func TestCFADNGBlackAndWhiteLevelsMatchLinearDNGReference(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(orderName(order), func(t *testing.T) {
			const width, height = 8, 8
			const whiteR, whiteG, whiteB = 60000, 62000, 64000
			const blackR, blackG, blackB = 100, 200, 300
			const red, green, blue = 30050, 31100, 32150 // 50% of each color's usable range.
			metadata := dngTestCFAMetadata(order, 0)
			metadata[50713] = dngTestShort(order, 2, 2)
			metadata[50714] = dngTestLong(order, blackR, blackG, blackG, blackB)
			metadata[50717] = dngTestLong(order, whiteR, whiteG, whiteB)
			cfa := linearDNGFixture(order, width, height, 1, dngTestBayerPixels(width, height, red, green, blue), metadata)
			rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
			rgbMetadata[50713] = dngTestShort(order, 1, 1)
			rgbMetadata[50714] = dngTestLong(order, blackR, blackG, blackB)
			rgbMetadata[50717] = dngTestLong(order, whiteR, whiteG, whiteB)
			reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)

			got, err := decodeLinearDNG(cfa)
			if err != nil {
				t.Fatalf("decode CFA DNG with phase-specific black levels: %v", err)
			}
			want, err := decodeLinearDNG(reference)
			if err != nil {
				t.Fatalf("decode LinearRaw reference: %v", err)
			}
			assertLinearDNGImagesEqual(t, got, want)
		})
	}
}

func TestCFADNGLosslessJPEG12BitDevelopMatchesLinearDNG(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(orderName(order), func(t *testing.T) {
			const width, height = 8, 8
			const red, green, blue, white = 1000, 2000, 3000, 4095
			pixels := dngTestBayerPixels(width, height, red, green, blue)
			encoded, tables, _ := dngTestLosslessJPEG(t, width, height, 12, 1, 0, 0, pixels)
			metadata := dngTestCFAMetadata(order, white)
			metadata[347] = dngTestField{typeID: 7, count: uint32(len(tables)), data: tables}
			cfa := linearDNGCompressedFixture(t, order, width, height, 1, 12, 7, 1, pixels, encoded, metadata)
			rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
			rgbMetadata[50717] = dngTestLong(order, white, white, white)
			reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)

			got, err := decodeLinearDNG(cfa)
			if err != nil {
				t.Fatalf("decode 12-bit lossless CFA DNG: %v", err)
			}
			want, err := decodeLinearDNG(reference)
			if err != nil {
				t.Fatalf("decode LinearRaw reference: %v", err)
			}
			assertLinearDNGImagesEqual(t, got, want)

			root := t.TempDir()
			source := filepath.Join(root, "12-bit-bayer.dng")
			if err := os.WriteFile(source, cfa, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", t.TempDir())
			t.Setenv("ProgramFiles", t.TempDir())
			t.Setenv("ProgramFiles(x86)", t.TempDir())
			gotTIFF, config, format, err := readRenderInputWithOptions(Frame{Image: source}, imageInputOptions{RawDevelop: true})
			if err != nil {
				t.Fatalf("develop 12-bit CFA DNG without an external converter: %v", err)
			}
			wantTIFF, err := encodeDevelopedDNGTIFF(want, 1)
			if err != nil {
				t.Fatal(err)
			}
			if format != "tiff" || config.Width != width || config.Height != height || !bytes.Equal(gotTIFF, wantTIFF) {
				t.Fatalf("12-bit CFA raw-develop result format=%s config=%+v matches reference=%t", format, config, bytes.Equal(gotTIFF, wantTIFF))
			}
		})
	}
}

func TestCFADNGDeflatePredictorDevelopMatchesLinearDNG(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(orderName(order), func(t *testing.T) {
			const width, height = 8, 8
			const red, green, blue, white = 10000, 20000, 30000, 65535
			pixels := dngTestBayerPixels(width, height, red, green, blue)
			metadata := dngTestCFAMetadata(order, white)
			data := linearDNGCompressedFixture(t, order, width, height, 1, 16, 8, 2, pixels, nil, metadata)
			rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
			rgbMetadata[50717] = dngTestLong(order, white, white, white)
			reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)

			got, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatalf("decode Deflate CFA DNG with Predictor 2: %v", err)
			}
			want, err := decodeLinearDNG(reference)
			if err != nil {
				t.Fatalf("decode LinearRaw reference: %v", err)
			}
			assertLinearDNGImagesEqual(t, got, want)
		})
	}
}

func TestCFADNGPackedIntegerSamples(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, depth := range []int{9, 10, 11, 12, 13, 14, 15} {
			t.Run(orderName(order)+"/"+strconv.Itoa(depth)+"-bit", func(t *testing.T) {
				const width, height = 8, 8
				maximum := uint16((uint32(1) << uint(depth)) - 1)
				red, green, blue := maximum-20, maximum/3, maximum/5
				pixels := dngTestBayerPixels(width, height, red, green, blue)
				for _, test := range []struct {
					name        string
					compression int
					predictor   int
				}{
					{"uncompressed", 1, 1},
					{"deflate-predictor-2", 8, 2},
				} {
					t.Run(test.name, func(t *testing.T) {
						metadata := dngTestCFAMetadata(order, uint32(maximum))
						data := dngTestPackedCFADNG(t, order, width, height, depth, test.compression, test.predictor, pixels, metadata)
						rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
						rgbMetadata[50717] = dngTestLong(order, uint32(maximum), uint32(maximum), uint32(maximum))
						reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)
						got, err := decodeLinearDNG(data)
						if err != nil {
							t.Fatalf("decode %d-bit CFA DNG: %v", depth, err)
						}
						want, err := decodeLinearDNG(reference)
						if err != nil {
							t.Fatalf("decode LinearRaw reference: %v", err)
						}
						assertLinearDNGImagesEqual(t, got, want)
					})
				}
			})
		}
	}
}

func TestCFADNGMapsCFAPlaneColorOrderToRGB(t *testing.T) {
	const width, height = 8, 8
	const red, green, blue, white = 10000, 20000, 30000, 65535
	order := binary.LittleEndian
	metadata := dngTestCFAMetadata(order, white)
	metadata[50710] = dngTestField{typeID: 1, count: 3, data: []byte{2, 1, 0}}
	metadata[33422] = dngTestField{typeID: 1, count: 4, data: []byte{2, 1, 1, 0}}
	cfa := linearDNGFixture(order, width, height, 1, dngTestBayerPixels(width, height, red, green, blue), metadata)
	rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
	rgbMetadata[50717] = dngTestLong(order, white, white, white)
	reference := linearDNGFixture(order, width, height, 3, dngTestLinearRGBPixels(width, height, red, green, blue), rgbMetadata)

	got, err := decodeLinearDNG(cfa)
	if err != nil {
		t.Fatalf("decode CFA DNG with reordered CFAPlaneColor: %v", err)
	}
	want, err := decodeLinearDNG(reference)
	if err != nil {
		t.Fatalf("decode LinearRaw reference: %v", err)
	}
	assertLinearDNGImagesEqual(t, got, want)
}

func TestCFADNGStage1TrimPreservesBayerPhase(t *testing.T) {
	order := binary.LittleEndian
	const sourceWidth, sourceHeight = 10, 10
	const outputWidth, outputHeight = 6, 6
	const red, green, blue, white = 10000, 20000, 30000, 65535
	metadata := dngTestCFAMetadata(order, white)
	metadata[51008] = testDNGOpcodeList(testDNGTrimBounds(2, 2, 8, 8))
	source := linearDNGFixture(order, sourceWidth, sourceHeight, 1, dngTestBayerPixels(sourceWidth, sourceHeight, red, green, blue), metadata)
	rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
	rgbMetadata[50717] = dngTestLong(order, white, white, white)
	reference := linearDNGFixture(order, outputWidth, outputHeight, 3, dngTestLinearRGBPixels(outputWidth, outputHeight, red, green, blue), rgbMetadata)

	got, err := decodeLinearDNG(source)
	if err != nil {
		t.Fatalf("decode CFA DNG after Stage 1 TrimBounds: %v", err)
	}
	want, err := decodeLinearDNG(reference)
	if err != nil {
		t.Fatalf("decode LinearRaw reference: %v", err)
	}
	if got.Bounds().Dx() != outputWidth || got.Bounds().Dy() != outputHeight {
		t.Fatalf("TrimBounds output is %dx%d, want %dx%d", got.Bounds().Dx(), got.Bounds().Dy(), outputWidth, outputHeight)
	}
	assertLinearDNGImagesEqual(t, got, want)
}

func TestCFADNGPatternOriginIsActiveArea(t *testing.T) {
	order := binary.LittleEndian
	const width, height = 10, 10
	const red, green, blue, white = 10000, 20000, 30000, 65535
	metadata := dngTestCFAMetadata(order, white)
	metadata[50829] = dngTestLong(order, 1, 1, 9, 9)
	cfa := linearDNGFixture(order, width, height, 1, dngTestCFAPixelsAtOrigin(width, height, 1, 1, []byte{0, 1, 1, 2}, red, green, blue), metadata)
	rgbMetadata := dngRGBIdentityCameraMetadata(order, nil)
	rgbMetadata[50717] = dngTestLong(order, white, white, white)
	reference := linearDNGFixture(order, 8, 8, 3, dngTestLinearRGBPixels(8, 8, red, green, blue), rgbMetadata)

	got, err := decodeLinearDNG(cfa)
	if err != nil {
		t.Fatalf("decode CFA DNG with offset ActiveArea: %v", err)
	}
	want, err := decodeLinearDNG(reference)
	if err != nil {
		t.Fatalf("decode LinearRaw reference: %v", err)
	}
	if got.Bounds().Dx() != 8 || got.Bounds().Dy() != 8 {
		t.Fatalf("ActiveArea output is %dx%d, want 8x8", got.Bounds().Dx(), got.Bounds().Dy())
	}
	assertLinearDNGImagesEqual(t, got, want)
}

func TestCFADNGRejectsUnsupportedPattern(t *testing.T) {
	metadata := dngTestCFAMetadata(binary.LittleEndian, 65535)
	metadata[33421] = dngTestShort(binary.LittleEndian, 2, 1)
	data := linearDNGFixture(binary.LittleEndian, 8, 8, 1, dngTestBayerPixels(8, 8, 10000, 20000, 30000), metadata)
	if _, err := decodeLinearDNG(data); err == nil {
		t.Fatal("unsupported non-Bayer CFA pattern was accepted")
	}
}
