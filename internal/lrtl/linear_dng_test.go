package lrtl

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"sort"
	"testing"
)

type dngTestField struct {
	typeID uint16
	count  uint32
	data   []byte
}

func dngTestLong(order binary.ByteOrder, values ...uint32) dngTestField {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		order.PutUint32(data[index*4:], value)
	}
	return dngTestField{4, uint32(len(values)), data}
}

func dngTestShort(order binary.ByteOrder, values ...uint16) dngTestField {
	data := make([]byte, len(values)*2)
	for index, value := range values {
		order.PutUint16(data[index*2:], value)
	}
	return dngTestField{3, uint32(len(values)), data}
}

func dngTestDouble(order binary.ByteOrder, values ...float64) dngTestField {
	data := make([]byte, len(values)*8)
	for index, value := range values {
		order.PutUint64(data[index*8:], math.Float64bits(value))
	}
	return dngTestField{12, uint32(len(values)), data}
}

// Build an independent minimal TIFF/DNG container with known sensor samples.
// No proprietary camera data or Adobe executable is needed for these tests.
func linearDNGFixture(order binary.ByteOrder, width, height, channels int, pixels []uint16, extra map[uint16]dngTestField) []byte {
	depths := make([]uint16, channels)
	for index := range depths {
		depths[index] = 16
	}
	fields := map[uint16]dngTestField{
		254: dngTestLong(order, 0), 256: dngTestLong(order, uint32(width)), 257: dngTestLong(order, uint32(height)),
		258: dngTestShort(order, depths...), 259: dngTestShort(order, 1), 262: dngTestShort(order, 34892),
		273: dngTestLong(order, 0), 277: dngTestShort(order, uint16(channels)), 278: dngTestLong(order, uint32(height)),
		279: dngTestLong(order, uint32(len(pixels)*2)), 284: dngTestShort(order, 1),
		50706: {1, 4, []byte{1, 4, 0, 0}},
	}
	for tag, field := range extra {
		if field.typeID == 0 {
			delete(fields, tag)
		} else {
			fields[tag] = field
		}
	}
	sampleDepth := uint16(16)
	if len(fields[258].data) >= 2 {
		sampleDepth = order.Uint16(fields[258].data)
	}
	sampleBytes := int(sampleDepth / 8)
	if _, supplied := extra[279]; !supplied {
		fields[279] = dngTestLong(order, uint32(len(pixels)*sampleBytes))
	}
	tags := make([]int, 0, len(fields))
	for tag := range fields {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	data := make([]byte, 8+2+len(tags)*12+4)
	if order == binary.LittleEndian {
		copy(data, "II")
	} else {
		copy(data, "MM")
	}
	order.PutUint16(data[2:], 42)
	order.PutUint32(data[4:], 8)
	order.PutUint16(data[8:], uint16(len(tags)))
	locations := map[uint16]int{}
	for index, tag := range tags {
		field := fields[uint16(tag)]
		start := 10 + index*12
		order.PutUint16(data[start:], uint16(tag))
		order.PutUint16(data[start+2:], field.typeID)
		order.PutUint32(data[start+4:], field.count)
		location := start + 8
		if len(field.data) > 4 {
			location = len(data)
			order.PutUint32(data[start+8:], uint32(location))
			data = append(data, field.data...)
		} else {
			copy(data[start+8:start+12], field.data)
		}
		locations[uint16(tag)] = location
	}
	if location, ok := locations[273]; ok {
		order.PutUint32(data[location:], uint32(len(data)))
	}
	if location, ok := locations[324]; ok {
		lengths := fields[325]
		start := len(data)
		for index := uint32(0); index < fields[324].count; index++ {
			order.PutUint32(data[location+int(index)*4:], uint32(start))
			start += int(order.Uint32(lengths.data[int(index)*4:]))
		}
	}
	for _, pixel := range pixels {
		if sampleDepth == 8 {
			data = append(data, byte(pixel))
			continue
		}
		var encoded [2]byte
		order.PutUint16(encoded[:], pixel)
		data = append(data, encoded[:]...)
	}
	return data
}

func linearDNGCompressedFixture(t *testing.T, order binary.ByteOrder, width, height, channels, depth, compression, predictor int, pixels []uint16, jpegData []byte, extra map[uint16]dngTestField) []byte {
	t.Helper()
	metadata := make(map[uint16]dngTestField, len(extra)+3)
	for tag, field := range extra {
		metadata[tag] = field
	}
	depths := make([]uint16, channels)
	for index := range depths {
		depths[index] = uint16(depth)
	}
	metadata[258] = dngTestShort(order, depths...)
	metadata[259] = dngTestShort(order, uint16(compression))
	metadata[317] = dngTestShort(order, uint16(predictor))
	fixture := linearDNGFixture(order, width, height, channels, pixels, metadata)
	var encoded []byte
	if compression == 7 || compression == 34892 {
		encoded = jpegData
	} else {
		raw := make([]byte, len(pixels)*(depth/8))
		for index, sample := range pixels {
			value := sample
			pixel := index / channels
			x := pixel % width
			if predictor == 2 && x > 0 {
				value -= pixels[index-channels]
			}
			if depth == 8 {
				raw[index] = byte(value)
			} else {
				order.PutUint16(raw[index*2:], value)
			}
		}
		if compression == 8 {
			var compressed bytes.Buffer
			writer := zlib.NewWriter(&compressed)
			if _, err := writer.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			encoded = compressed.Bytes()
		} else {
			encoded = raw
		}
	}
	blockOffset := len(fixture)
	fixture = append(fixture, encoded...)
	entries := int(order.Uint16(fixture[8:10]))
	for index := 0; index < entries; index++ {
		entry := 10 + index*12
		tag := order.Uint16(fixture[entry:])
		switch tag {
		case 273:
			order.PutUint32(fixture[entry+8:], uint32(blockOffset))
		case 279:
			order.PutUint32(fixture[entry+8:], uint32(len(encoded)))
		}
	}
	return fixture
}

func TestLinearDNGBlackWhiteNormalizationAndGamma(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			data := linearDNGFixture(order, 2, 2, 1, []uint16{1000, 3500, 6000, 11000}, map[uint16]dngTestField{
				50714: dngTestDouble(order, 1000), 50717: dngTestLong(order, 11000),
			})
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			for index, want := range []uint16{0, 35199, 48192, 65535} {
				pixel := img.NRGBA64At(index%2, index/2)
				if pixel.R != want || pixel.G != want || pixel.B != want || pixel.A != 65535 {
					t.Errorf("pixel%d=%v want gray %d", index, pixel, want)
				}
			}
		})
	}
}

func TestLinearDNGCompressedBlocks(t *testing.T) {
	rgbMetadata := func(order binary.ByteOrder) map[uint16]dngTestField {
		return map[uint16]dngTestField{
			50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
			50728: dngTestDouble(order, 1, 1, 1),
		}
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(orderName(order), func(t *testing.T) {
			for _, test := range []struct {
				name      string
				width     int
				height    int
				channels  int
				depth     int
				predictor int
				pixels    []uint16
				metadata  map[uint16]dngTestField
			}{
				{"16-bit-gray-predictor-1", 3, 2, 1, 16, 1, []uint16{100, 8000, 30000, 65535, 24000, 10}, nil},
				{"16-bit-rgb-predictor-2", 2, 2, 3, 16, 2, []uint16{1000, 2000, 3000, 16000, 8000, 4000, 50000, 22000, 12000, 65535, 32768, 8192}, rgbMetadata(order)},
				{"8-bit-rgb-predictor-2", 2, 2, 3, 8, 2, []uint16{0, 32, 255, 64, 128, 16, 200, 220, 240, 255, 1, 100}, rgbMetadata(order)},
			} {
				t.Run(test.name, func(t *testing.T) {
					baselineMetadata := make(map[uint16]dngTestField, len(test.metadata)+1)
					for tag, field := range test.metadata {
						baselineMetadata[tag] = field
					}
					depths := make([]uint16, test.channels)
					for channel := range depths {
						depths[channel] = uint16(test.depth)
					}
					baselineMetadata[258] = dngTestShort(order, depths...)
					baselineData := linearDNGFixture(order, test.width, test.height, test.channels, test.pixels, baselineMetadata)
					compressedData := linearDNGCompressedFixture(t, order, test.width, test.height, test.channels, test.depth, 8, test.predictor, test.pixels, nil, test.metadata)
					baseline, err := decodeLinearDNG(baselineData)
					if err != nil {
						t.Fatalf("decode uncompressed baseline: %v", err)
					}
					compressed, err := decodeLinearDNG(compressedData)
					if err != nil {
						t.Fatalf("decode Deflate + Predictor %d: %v", test.predictor, err)
					}
					assertLinearDNGImagesEqual(t, compressed, baseline)
				})
			}
		})
	}

	t.Run("lossless-jpeg-rgb", func(t *testing.T) {
		order := binary.LittleEndian
		jpegData, err := hex.DecodeString("ffd8ffe000104a46494600010100000100010000ffc30011080002000203011100021100031100ffc4001800000301010000000000000000000000000607080405ffda000c030100020003000400009fd26a7601e1e501415047fdecbfffd9")
		if err != nil {
			t.Fatal(err)
		}
		pixels := []uint16{0, 20, 40, 64, 80, 100, 128, 150, 170, 255, 240, 220}
		metadata := map[uint16]dngTestField{
			258:   dngTestShort(order, 8, 8, 8),
			50717: dngTestLong(order, 255),
			50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
			50728: dngTestDouble(order, 1, 1, 1),
		}
		baselineData := linearDNGFixture(order, 2, 2, 3, pixels, metadata)
		jpegDNG := linearDNGCompressedFixture(t, order, 2, 2, 3, 8, 7, 1, pixels, jpegData, metadata)
		baseline, err := decodeLinearDNG(baselineData)
		if err != nil {
			t.Fatalf("decode uncompressed RGB baseline: %v", err)
		}
		compressed, err := decodeLinearDNG(jpegDNG)
		if err != nil {
			t.Fatalf("decode Lossless JPEG RGB DNG: %v", err)
		}
		assertLinearDNGImagesEqual(t, compressed, baseline)
	})

	t.Run("lossless-jpeg-geometry-differs-from-tiff", func(t *testing.T) {
		order := binary.LittleEndian
		pixels := []uint16{0, 20, 40, 64, 80, 100, 128, 150, 170, 255, 240, 220}
		jpegData, _, _ := dngTestLosslessJPEG(t, 6, 2, 8, 1, 0, 0, pixels)
		metadata := map[uint16]dngTestField{
			258:   dngTestShort(order, 8, 8, 8),
			50717: dngTestLong(order, 255),
			50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
			50728: dngTestDouble(order, 1, 1, 1),
		}
		baseline, err := decodeLinearDNG(linearDNGFixture(order, 2, 2, 3, pixels, metadata))
		if err != nil {
			t.Fatalf("decode uncompressed TIFF geometry baseline: %v", err)
		}
		compressedData := linearDNGCompressedFixture(t, order, 2, 2, 3, 8, 7, 1, pixels, jpegData, metadata)
		compressed, err := decodeLinearDNG(compressedData)
		if err != nil {
			t.Fatalf("decode JPEG geometry differing from TIFF: %v", err)
		}
		assertLinearDNGImagesEqual(t, compressed, baseline)
		if _, err := decodeDNGJPEGBlock(jpegData, nil, 3, 2, 3, 8, order); err == nil {
			t.Fatal("lossless JPEG with a different total sample count was accepted")
		}
	})

	t.Run("lossy-jpeg-rgb", func(t *testing.T) {
		pixels := []uint16{245, 12, 55, 34, 220, 90, 80, 42, 240, 190, 110, 20, 16, 200, 140, 250, 60, 35}
		source := image.NewNRGBA(image.Rect(0, 0, 3, 2))
		for index := range 6 {
			source.SetNRGBA(index%3, index/3, color.NRGBA{R: uint8(pixels[index*3]), G: uint8(pixels[index*3+1]), B: uint8(pixels[index*3+2]), A: 255})
		}
		var jpegBytes bytes.Buffer
		if err := jpeg.Encode(&jpegBytes, source, &jpeg.Options{Quality: 92}); err != nil {
			t.Fatal(err)
		}
		decodedJPEG, err := jpeg.Decode(bytes.NewReader(jpegBytes.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		jpegPixels := make([]uint16, 0, 18)
		wantRaw := make([]byte, 0, 18)
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				red, green, blue, _ := decodedJPEG.At(x, y).RGBA()
				for _, component := range []uint16{uint16(red >> 8), uint16(green >> 8), uint16(blue >> 8)} {
					jpegPixels = append(jpegPixels, component)
					wantRaw = append(wantRaw, byte(component))
				}
			}
		}
		metadataFor := func(order binary.ByteOrder) map[uint16]dngTestField {
			return map[uint16]dngTestField{
				50717: dngTestLong(order, 255),
				50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
				50728: dngTestDouble(order, 1, 1, 1),
			}
		}
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			metadata := metadataFor(order)
			baselineMetadata := make(map[uint16]dngTestField, len(metadata)+1)
			for tag, field := range metadata {
				baselineMetadata[tag] = field
			}
			baselineMetadata[258] = dngTestShort(order, 8, 8, 8)
			baseline, err := decodeLinearDNG(linearDNGFixture(order, 3, 2, 3, jpegPixels, baselineMetadata))
			if err != nil {
				t.Fatalf("decode lossy JPEG sample reference (%s): %v", orderName(order), err)
			}
			compressedData := linearDNGCompressedFixture(t, order, 3, 2, 3, 8, 34892, 1, jpegPixels, jpegBytes.Bytes(), metadata)
			compressed, err := decodeLinearDNG(compressedData)
			if err != nil {
				t.Fatalf("decode lossy JPEG Linear DNG (%s): %v", orderName(order), err)
			}
			assertLinearDNGImagesEqual(t, compressed, baseline)
			raw, err := decodeDNGJPEGBlock(jpegBytes.Bytes(), nil, 3, 2, 3, 8, order)
			if err != nil {
				t.Fatalf("decode lossy JPEG samples (%s): %v", orderName(order), err)
			}
			if !bytes.Equal(raw, wantRaw) {
				t.Fatalf("lossy JPEG samples (%s)=%v, want %v", orderName(order), raw, wantRaw)
			}
		}
		if _, err := decodeDNGJPEGBlock(jpegBytes.Bytes(), nil, 2, 2, 3, 8, binary.LittleEndian); err == nil {
			t.Fatal("lossy JPEG with mismatched dimensions was accepted")
		}
		if _, err := decodeDNGJPEGBlock(jpegBytes.Bytes(), nil, 3, 2, 3, 16, binary.LittleEndian); err == nil {
			t.Fatal("8-bit lossy JPEG was accepted as a 16-bit image")
		}
	})

	t.Run("jpeg-rejects-tiff-predictor", func(t *testing.T) {
		order := binary.LittleEndian
		jpegData := []byte{0xff, 0xd8, 0xff, 0xd9}
		for _, compression := range []int{7, 34892} {
			data := linearDNGCompressedFixture(t, order, 1, 1, 1, 8, compression, 2, []uint16{1}, jpegData, nil)
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatalf("JPEG compression %d combined with TIFF Predictor 2 was accepted", compression)
			}
		}
	})
}

func assertLinearDNGImagesEqual(t *testing.T, got, want *image.NRGBA64) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds %v, want %v", got.Bounds(), want.Bounds())
	}
	for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
		for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
			if actual, expected := got.NRGBA64At(x, y), want.NRGBA64At(x, y); actual != expected {
				t.Errorf("pixel (%d,%d)=%v, want %v", x, y, actual, expected)
			}
		}
	}
}

func TestLinearDNGDefaultCrop(t *testing.T) {
	order := binary.LittleEndian
	data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, map[uint16]dngTestField{
		50719: dngTestDouble(order, 1, 0), 50720: dngTestDouble(order, 1, 2),
	})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 2 || img.NRGBA64At(0, 1).R != 65535 {
		t.Fatalf("incorrect default crop: %v", img.Bounds())
	}
}

func TestLinearDNGTiledPixels(t *testing.T) {
	order := binary.LittleEndian
	// Four 2x2 padded tiles form a 3x3 image. Padding has deliberately
	// different values so a row/column-stride bug changes visible pixels.
	pixels := []uint16{0, 0, 0, 0, 65535, 999, 65535, 999, 0, 0, 999, 999, 65535, 999, 999, 999}
	data := linearDNGFixture(order, 3, 3, 1, pixels, map[uint16]dngTestField{
		273: {}, 279: {}, 322: dngTestLong(order, 2), 323: dngTestLong(order, 2),
		324: dngTestLong(order, 0, 0, 0, 0), 325: dngTestLong(order, 8, 8, 8, 8),
	})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			want := uint16(0)
			if x == 2 {
				want = 65535
			}
			if got := img.NRGBA64At(x, y).R; got != want {
				t.Errorf("(%d,%d)=%d want %d", x, y, got, want)
			}
		}
	}
}

func TestLinearDNGRejectsInvalidMetadata(t *testing.T) {
	order := binary.LittleEndian
	for name, extra := range map[string]map[uint16]dngTestField{
		"compressed": {259: dngTestShort(order, 7)}, "mosaic": {262: dngTestShort(order, 32803)},
		"huge_dimensions": {256: dngTestLong(order, 0xffffffff)}, "zero_white": {50717: dngTestLong(order, 0)},
		"nan_black": {50714: dngTestDouble(order, math.NaN())}, "short_strip": {279: dngTestLong(order, 1)},
		"bad_crop":        {50719: dngTestDouble(order, 4, 0), 50720: dngTestDouble(order, 1, 1)},
		"missing_version": {50706: {}}, "cycle": {330: dngTestLong(order, 8), 262: dngTestShort(order, 2)},
		"reversed_active_area": {50829: dngTestLong(order, 2, 0, 0, 2)},
		"huge_crop":            {50719: dngTestDouble(order, 1e100, 0), 50720: dngTestDouble(order, 1, 1)},
		"bad_scale":            {50718: dngTestDouble(order, 9, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 1, 2, 3}, extra)
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 1, 2, 3}, nil)
	for _, length := range []int{0, 7, 15, len(data) - 1} {
		if _, err := decodeLinearDNG(data[:length]); err == nil {
			t.Fatalf("truncated file of %d bytes accepted", length)
		}
	}
}

func TestLinearDNGCameraWhiteBalanceAndColorMatrix(t *testing.T) {
	order := binary.LittleEndian
	for _, forward := range []bool{true, false} {
		t.Run(fmt.Sprint("forward=", forward), func(t *testing.T) {
			metadata := map[uint16]dngTestField{339: dngTestShort(order, 1, 1, 1)}
			var pixels []uint16
			if forward {
				// Camera neutral [.5,1,.25], exposed to one quarter white,
				// should become a display-neutral quarter-linear gray.
				metadata[50728] = dngTestDouble(order, .5, 1, .25)
				metadata[50964] = dngTestDouble(order, .96422, 0, 0, 0, 1, 0, 0, 0, .82521)
				pixels = []uint16{8192, 16384, 4096}
			} else {
				// Identity ColorMatrix puts samples directly in XYZ. A D50
				// neutral uses the fallback matrix/adaptation path.
				metadata[50721] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
				metadata[50728] = dngTestDouble(order, .96422, 1, .82521)
				pixels = []uint16{15798, 16384, 13520}
			}
			data := linearDNGFixture(order, 1, 1, 3, pixels, metadata)
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			pixel := img.NRGBA64At(0, 0)
			for _, value := range []uint16{pixel.R, pixel.G, pixel.B} {
				if math.Abs(float64(value)-35199) > 50 {
					t.Fatalf("white balance/color transform did not preserve gray: %v", pixel)
				}
			}
		})
	}
}

func FuzzLinearDNG(f *testing.F) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{50730: dngTestDouble(order, -.5), 51109: dngTestSRational(order, 3, 2)}))
		f.Add(linearDNGFixture(order, 4, 2, 1, []uint16{8192, 8192, 8192, 8192, 8192, 8192, 8192, 8192}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(testDNGVignetteRadial(.5, .5, [5]float64{.5, -.1, .02})),
		}))
		stage1Area := testDNGArea(image.Rect(0, 0, 2, 2), 0, 1, 1, 1)
		f.Add(linearDNGFixture(order, 2, 2, 1, []uint16{4096, 8192, 16384, 32768}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(
				testDNGGainMap(testDNGGainMapPayload(2, 2, 1, 1, 1, 1.5)),
				testDNGAxisOpcode(10, stage1Area, 512, -512),
				testDNGAxisOpcode(11, stage1Area, -256, 256),
				testDNGAxisOpcode(12, stage1Area, 1.25, .75),
				testDNGAxisOpcode(13, stage1Area, .8, 1.2),
			),
		}))
		stage1Pixels := []uint16{900, 900, 900, 900, 65535, 900, 900, 900, 900}
		f.Add(linearDNGFixture(order, 3, 3, 1, stage1Pixels, map[uint16]dngTestField{
			51008: testDNGOpcodeList(testDNGBadPixelConstant(65535, 0)),
		}))
		f.Add(linearDNGFixture(order, 3, 3, 1, stage1Pixels, map[uint16]dngTestField{
			51008: testDNGOpcodeList(testDNGBadPixelList(0, [][2]int32{{1, 1}}, nil)),
		}))
		f.Add(linearDNGFixture(order, 4, 1, 1, []uint16{0, 1, 2, 3}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(testDNGMapTable(testDNGArea(image.Rectangle{}, 0, 1, 1, 1), 3, 2, 1, 0)),
		}))
		f.Add(linearDNGFixture(order, 4, 1, 1, []uint16{0, 1, 2, 3}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(testDNGPolynomial(testDNGArea(image.Rectangle{}, 0, 1, 1, 1), 0, 0, 16000)),
		}))
	}
	f.Add(profileMapDNGFixture(binary.LittleEndian))
	f.Add(profileMapDNGFixture(binary.BigEndian))
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, tripleIlluminantDNGMetadata(order)))
	}
	maskOrder := binary.LittleEndian
	maskRed := dngTestRGBTableData(maskOrder, 0, "subject", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{1, 0, 0} })
	maskBackground := dngTestRGBTableData(maskOrder, 0, "", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{0, 0, 1} })
	maskMetadata := dngRGBIdentityCameraMetadata(maskOrder, dngTestRGBTablesData(maskOrder, 0, maskRed, maskBackground))
	maskFixture := linearDNGFixture(maskOrder, 2, 1, 3, []uint16{8192, 16384, 24576, 24576, 32768, 40960}, maskMetadata)
	maskFixture = appendDNGSemanticMaskIFD(f, maskFixture, maskOrder, "subject", 2, 1, 8, 1, 1, 1, 0, 0, []byte{255, 0}, nil)
	f.Add(maskFixture)
	f.Add(profileToneDNGFixture(binary.LittleEndian, 0))
	f.Add(profileToneDNGFixture(binary.BigEndian, 1))
	profileIFD := linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{
		50933: dngTestLong(binary.LittleEndian, 0), 50934: dngTestASCII("Fuzz Profile"),
		50936: dngTestASCII("Default"), 52544: testProfileGainMap2(binary.LittleEndian, 1),
	})
	profileIFD = appendDNGCameraProfile(f, profileIFD, binary.LittleEndian, binary.BigEndian, "Fuzz Profile", map[uint16]dngTestField{
		52544: testProfileGainMap2(binary.BigEndian, 2),
	})
	f.Add(profileIFD)
	f.Add(warpDNGFixture(binary.LittleEndian))
	f.Add(warpDNGFixture(binary.BigEndian))
	f.Add(mixedDNGPointFixture(binary.LittleEndian, 2, 2))
	f.Add(mixedDNGPointFixture(binary.BigEndian, 2, 2))
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 2, 2, 1, []uint16{1024, 2048, 4096, 8192}, map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGGainMap(testDNGGainMapPayload(2, 2, 2, 2, 1, 1, 2, 3, 4)))}))
		f.Add(linearDNGFixture(order, 5, 5, 1, []uint16{
			10000, 10000, 10000, 10000, 10000,
			10000, 1000, 10000, 2000, 10000,
			10000, 10000, 65535, 10000, 10000,
			10000, 3000, 10000, 4000, 10000,
			10000, 10000, 10000, 10000, 10000,
		}, map[uint16]dngTestField{50717: dngTestLong(order, 65535), 51009: testDNGOpcodeList(testDNGBadPixelConstant(65535, 0))}))
		listPixels := make([]uint16, 7*7)
		for i := range listPixels {
			listPixels[i] = 10000
		}
		listPixels[2*7+2], listPixels[3*7+2], listPixels[3*7+3] = 65535, 65535, 65535
		f.Add(linearDNGFixture(order, 7, 7, 1, listPixels, map[uint16]dngTestField{
			50717: dngTestLong(order, 65535),
			51022: testDNGOpcodeList(testDNGBadPixelList(0, [][2]int32{{2, 2}}, [][4]int32{{3, 2, 4, 4}})),
		}))
	}
	f.Add(blackPatternDNGFixture(f, binary.LittleEndian))
	f.Add(blackPatternDNGFixture(f, binary.BigEndian))
	f.Add([]byte("II"))
	f.Add(linearDNGFixture(binary.LittleEndian, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, nil))
	f.Add(linearDNGFixture(binary.BigEndian, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, nil))
	f.Add(linearDNGFixture(binary.LittleEndian, 1, 1, 3, []uint16{8192, 16384, 4096}, map[uint16]dngTestField{
		50728: dngTestDouble(binary.LittleEndian, .5, 1, .25), 50964: dngTestDouble(binary.LittleEndian, .96422, 0, 0, 0, 1, 0, 0, 0, .82521),
	}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		_, _ = decodeLinearDNG(data)
	})
}

func TestDNGColorMatrixInversionAndWhiteAdaptation(t *testing.T) {
	matrix := dngMatrix{{1, 2, 3}, {0, 1, 4}, {5, 6, 0}}
	inverse, err := matrix.inverse()
	if err != nil {
		t.Fatal(err)
	}
	product := matrix.multiply(inverse)
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			if math.Abs(product[row][column]-dngIdentityMatrix[row][column]) > 1e-12 {
				t.Fatal("matrix inverse is incorrect")
			}
		}
	}
	if _, err := (dngMatrix{}).inverse(); err == nil {
		t.Fatal("singular matrix accepted")
	}
	d65 := [3]float64{0.95047, 1, 1.08883}
	adaptation, err := dngChromaticAdaptation(d65, dngWhiteD50)
	if err != nil {
		t.Fatal(err)
	}
	result := adaptation.vector(d65)
	for channel := range result {
		if math.Abs(result[channel]-dngWhiteD50[channel]) > 1e-6 {
			t.Fatalf("white adaptation=%v", result)
		}
	}
}

func TestDNGWhiteTemperatureReferenceWhites(t *testing.T) {
	for _, test := range []struct {
		white [3]float64
		want  float64
	}{
		{[3]float64{.95047, 1, 1.08883}, 6500},
		{dngWhiteD50, 5000},
		{[3]float64{}, 6500},
		{[3]float64{.4, .1858, .4142}, 6500},
	} {
		if got := dngWhiteTemperature(test.white); math.Abs(got-test.want) > 50 {
			t.Errorf("CCT(%v)=%g want approximately %g", test.white, got, test.want)
		}
	}
	for code, want := range map[int]float64{17: 2856, 21: 6500, 23: 5000, 0: 0} {
		if got := dngIlluminantTemperature(code); got != want {
			t.Errorf("illuminant %d=%g want %g", code, got, want)
		}
	}
}

func TestDNGDualIlluminantSelectsDaylightMatrix(t *testing.T) {
	order := binary.LittleEndian
	metadata := map[uint16]dngTestField{
		50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
		50722: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
		50728: dngTestDouble(order, .95047, 1, 1.08883),
		50778: dngTestShort(order, 17), 50779: dngTestShort(order, 21),
		50964: dngTestDouble(order, .5, .46422, 0, 0, 1, 0, 0, 0, .82521),
		50965: dngTestDouble(order, .96422, 0, 0, 0, 1, 0, 0, 0, .82521),
	}
	// A D65 scene should use the second calibration, retaining this red
	// sample's stronger X component. The tungsten matrix would produce a
	// red sRGB code near 28000 instead of the expected approximately 38400.
	data := linearDNGFixture(order, 1, 1, 3, []uint16{6229, 0, 0}, metadata)
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	pixel := img.NRGBA64At(0, 0)
	if pixel.R < 38000 || pixel.R > 39000 || pixel.G != 0 || pixel.B < 4900 || pixel.B > 5300 {
		t.Fatalf("wrong dual-illuminant color transform: %v", pixel)
	}
}

func TestAdobeLinearDNGRealSample(t *testing.T) {
	path := os.Getenv("LRTL_LINEAR_DNG_SAMPLE")
	if path == "" {
		t.Skip("set LRTL_LINEAR_DNG_SAMPLE to a real Converter -l -u -p0 -dng1.4 output")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("developed real sample: %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	if output := os.Getenv("LRTL_LINEAR_DNG_PREVIEW"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 90}); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
