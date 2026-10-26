package lrtl

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"sort"
	"testing"
)

func dngTestRGBTableData(order binary.ByteOrder, method uint32, semantic string, divisions int, pixelType, gamma, primaries, gamut byte, value func(r, g, b int) [3]float64) []byte {
	data := make([]byte, 8)
	order.PutUint32(data, 1)
	order.PutUint32(data[4:], method)
	var nameLength [2]byte
	order.PutUint16(nameLength[:], uint16(len(semantic)))
	data = append(data, nameLength[:]...)
	data = append(data, semantic...)
	data = append(data, byte(divisions), pixelType, gamma, primaries, gamut)
	for r := 0; r < divisions; r++ {
		for g := 0; g < divisions; g++ {
			for b := 0; b < divisions; b++ {
				for _, channel := range value(r, g, b) {
					switch pixelType {
					case 0:
						data = append(data, byte(math.Round(channel*255)))
					case 1:
						var encoded [2]byte
						order.PutUint16(encoded[:], uint16(math.Round(channel*65535)))
						data = append(data, encoded[:]...)
					case 2:
						var encoded [4]byte
						order.PutUint32(encoded[:], math.Float32bits(float32(channel)))
						data = append(data, encoded[:]...)
					}
				}
			}
		}
	}
	return data
}

func dngTestRGBTablesData(order binary.ByteOrder, method uint32, tables ...[]byte) []byte {
	data := make([]byte, 8)
	order.PutUint32(data, uint32(len(tables)))
	order.PutUint32(data[4:], method)
	for _, table := range tables {
		data = append(data, table[8:]...)
	}
	return data
}

func dngTestRGBTableField(data []byte) dngTestField {
	return dngTestField{typeID: 7, count: uint32(len(data)), data: data}
}

func readTestDNGRGBTables(t *testing.T, order binary.ByteOrder, blob []byte) (*dngRGBTables, error) {
	t.Helper()
	file := make([]byte, 8+len(blob))
	order.PutUint32(file, 8)
	copy(file[8:], blob)
	scanner := &tiffPreviewScanner{data: file, order: order}
	fields := &dngFields{
		scanner: scanner,
		entries: map[uint16]tiffPreviewEntry{
			dngRGBTablesTag: {tag: dngRGBTablesTag, typeID: 7, count: uint32(len(blob)), value: file[:4], source: scanner},
		},
	}
	return readDNGRGBTables(fields, 1, 1, 1)
}

func dngTestIdentityRGBTable(divisions int, semantic string, transform func([3]float64) [3]float64) [][3]float64 {
	values := make([][3]float64, 0, divisions*divisions*divisions)
	for r := 0; r < divisions; r++ {
		for g := 0; g < divisions; g++ {
			for b := 0; b < divisions; b++ {
				point := [3]float64{float64(r) / float64(divisions-1), float64(g) / float64(divisions-1), float64(b) / float64(divisions-1)}
				values = append(values, transform(point))
			}
		}
	}
	return values
}

func TestDNGRGBTablesParserPixelTypesAndByteOrders(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, pixelType := range []byte{0, 1, 2} {
			t.Run(order.String()+"/"+string(rune('0'+pixelType)), func(t *testing.T) {
				blob := dngTestRGBTableData(order, 0, "", 2, pixelType, 0, 4, 0, func(r, g, b int) [3]float64 {
					return [3]float64{float64(r), float64(g), float64(b)}
				})
				tables, err := readTestDNGRGBTables(t, order, blob)
				if err != nil {
					t.Fatal(err)
				}
				if tables == nil || len(tables.tables) != 1 || !tables.tables[0].background {
					t.Fatalf("parsed tables=%+v", tables)
				}
				tolerance := 1.0 / 65535
				if pixelType == 0 {
					tolerance = 1.0 / 255
				}
				for index, want := range [][3]float64{{0, 0, 0}, {1, 1, 1}} {
					got := tables.tables[0].values[index*7]
					for channel := range want {
						if math.Abs(got[channel]-want[channel]) > tolerance {
							t.Fatalf("pixel type %d value %d=%v want %v", pixelType, index, got, want)
						}
					}
				}
			})
		}
	}
}

func TestDNGRGBTableTrilinearGammaAndPrimaries(t *testing.T) {
	values := make([][3]float64, 8)
	for r := 0; r < 2; r++ {
		for g := 0; g < 2; g++ {
			for b := 0; b < 2; b++ {
				index := (r*2+g)*2 + b
				values[index] = [3]float64{float64(r), float64(g), float64(b)}
			}
		}
	}
	// A neutral is inside all five table gamuts, so this exercises the primary
	// conversions without intentional clipping of saturated colors.
	input := [3]float64{.5, .5, .5}
	for gamma := byte(0); gamma <= 4; gamma++ {
		for primaries := byte(0); primaries <= 4; primaries++ {
			toTable, fromTable, err := dngRGBTablePrimaries(primaries)
			if err != nil {
				t.Fatal(err)
			}
			table := dngRGBTable{divisions: 2, gamma: gamma, toTable: toTable, fromTable: fromTable, values: values}
			got := table.apply(input)
			for channel := range input {
				if math.Abs(got[channel]-input[channel]) > 2e-6 {
					t.Fatalf("identity table gamma=%d primaries=%d channel %d: %g want %g", gamma, primaries, channel, got[channel], input[channel])
				}
			}
		}
	}
	// The center of a 2x2x2 table is the average of its eight corners.
	center := dngRGBTable{divisions: 2, values: [][3]float64{{0, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, 1, 1}, {1, 0, 0}, {1, 0, 1}, {1, 1, 0}, {1, 1, 1}}}
	if got := center.lookup([3]float64{.5, .5, .5}); got != [3]float64{.5, .5, .5} {
		t.Fatalf("trilinear center=%v", got)
	}
}

func TestDNGRGBTableGamutExtension(t *testing.T) {
	values := make([][3]float64, 8)
	for index := range values {
		values[index] = [3]float64{.5, .5, .5}
	}
	plain := dngRGBTable{divisions: 2, toTable: dngIdentityMatrix, fromTable: dngIdentityMatrix, values: values}
	if got := plain.apply([3]float64{1.2, -.1, .4}); got != [3]float64{.5, .5, .5} {
		t.Fatalf("without gamut extension=%v", got)
	}
	plain.gamutExtend = true
	if got, want := plain.apply([3]float64{1.2, -.1, .4}), [3]float64{.7, .4, .5}; got != want {
		t.Fatalf("with gamut extension=%v want %v", got, want)
	}
}

func dngRGBIdentityCameraMetadata(order binary.ByteOrder, table []byte) map[uint16]dngTestField {
	colorMatrix, err := dngProPhotoToXYZ.inverse()
	if err != nil {
		panic(err)
	}
	values := make([]float64, 0, 9)
	for _, row := range colorMatrix {
		values = append(values, row[:]...)
	}
	neutral := colorMatrix.vector(dngWhiteD50)
	metadata := map[uint16]dngTestField{
		50721: dngTestDouble(order, values...),
		50728: dngTestDouble(order, neutral[:]...),
		50778: dngTestShort(order, 23),
	}
	if table != nil {
		metadata[dngRGBTablesTag] = dngTestRGBTableField(table)
	}
	return metadata
}

func TestLinearDNGRGBTablesTransformRawPixels(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		swap := dngTestRGBTableData(order, 0, "", 2, 2, 0, 4, 0, func(r, g, b int) [3]float64 {
			return [3]float64{float64(g), float64(r), float64(b)}
		})
		pixels := []uint16{16384, 32768, 49152}
		metadata := dngRGBIdentityCameraMetadata(order, swap)
		img, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, pixels, metadata))
		if err != nil {
			t.Fatal(err)
		}
		proPhoto := [3]float64{float64(pixels[1]) / 65535, float64(pixels[0]) / 65535, float64(pixels[2]) / 65535}
		srgb := dngXYZD50ToSRGB.multiply(dngProPhotoToXYZ).vector(proPhoto)
		want := [3]uint16{encodeDNGSRGB(srgb[0]), encodeDNGSRGB(srgb[1]), encodeDNGSRGB(srgb[2])}
		got := img.NRGBA64At(0, 0)
		for channel, pair := range [][2]uint16{{got.R, want[0]}, {got.G, want[1]}, {got.B, want[2]}} {
			if delta := math.Abs(float64(pair[0]) - float64(pair[1])); delta > 8 {
				t.Fatalf("order %T channel %d pixel=%v want RGB=%v", order, channel, got, want)
			}
		}
	}
}

func TestLinearDNGRGBTablesCameraProfileTakesPrecedence(t *testing.T) {
	mainOrder, profileOrder := binary.LittleEndian, binary.BigEndian
	rootTable := dngTestRGBTableData(mainOrder, 0, "", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{1, 0, 0} })
	profileTable := dngTestRGBTableData(profileOrder, 0, "", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{0, 0, 1} })
	metadata := dngRGBIdentityCameraMetadata(mainOrder, rootTable)
	metadata[50933] = dngTestLong(mainOrder, 0)
	data := linearDNGFixture(mainOrder, 1, 1, 3, []uint16{16384, 32768, 49152}, metadata)
	data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Selected", map[uint16]dngTestField{
		dngRGBTablesTag: dngTestRGBTableField(profileTable),
	})

	rootImage, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	profileImage, err := decodeLinearDNGWithProfile(data, "Selected")
	if err != nil {
		t.Fatal(err)
	}
	rootPixel, profilePixel := rootImage.NRGBA64At(0, 0), profileImage.NRGBA64At(0, 0)
	if rootPixel.R <= rootPixel.B || profilePixel.B <= profilePixel.R {
		t.Fatalf("RGBTables profile precedence failed: root=%v selected-profile=%v", rootPixel, profilePixel)
	}
}

func TestDNGRGBTablesMaskedWeightedAndSequential(t *testing.T) {
	mask := &dngSemanticMask{width: 3, height: 1, fullWidth: 3, fullHeight: 1, values: []float32{1, .5, 0}}
	constant := func(value [3]float64) dngRGBTable {
		values := make([][3]float64, 8)
		for index := range values {
			values[index] = value
		}
		return dngRGBTable{divisions: 2, toTable: dngIdentityMatrix, fromTable: dngIdentityMatrix, values: values}
	}
	red, blue := constant([3]float64{1, 0, 0}), constant([3]float64{0, 0, 1})
	red.semanticName, red.masks = "subject", []*dngSemanticMask{mask}
	blue.background = true
	input := [3]float64{.2, .4, .6}
	weighted := dngRGBTables{composite: 0, tables: []dngRGBTable{red, blue}}
	for x, want := range [][3]float64{{1, 0, 0}, {.5, 0, .5}, {0, 0, 1}} {
		got := weighted.apply(input, x, 0, 3, 1)
		for channel := range want {
			if math.Abs(got[channel]-want[channel]) > 1e-7 {
				t.Fatalf("weighted x=%d result=%v want=%v", x, got, want)
			}
		}
	}
	green := constant([3]float64{0, 1, 0})
	green.semanticName, green.masks = "subject", []*dngSemanticMask{mask}
	sequential := dngRGBTables{composite: 1, tables: []dngRGBTable{red, green}}
	got := sequential.apply(input, 1, 0, 3, 1)
	want := [3]float64{.3, .6, .15}
	for channel := range want {
		if math.Abs(got[channel]-want[channel]) > 1e-7 {
			t.Fatalf("sequential result=%v want=%v", got, want)
		}
	}
	withoutBackground := dngRGBTables{composite: 0, tables: []dngRGBTable{red}}
	got = withoutBackground.apply(input, 1, 0, 3, 1)
	want = [3]float64{.6, .2, .3}
	for channel := range want {
		if math.Abs(got[channel]-want[channel]) > 1e-7 {
			t.Fatalf("weighted identity background result=%v want=%v", got, want)
		}
	}
}

func appendDNGSemanticMaskIFD(t testing.TB, data []byte, order binary.ByteOrder, name string, width, height int, bits, compression, sampleFormat, predictor, tileWidth, tileHeight uint16, samples []byte, subArea []uint32, jpegTables ...[]byte) []byte {
	t.Helper()
	rootEntries := int(order.Uint16(data[8:10]))
	rootNext := 10 + rootEntries*12
	if order.Uint32(data[rootNext:]) != 0 {
		t.Fatal("test DNG root already has a following IFD")
	}
	maskOffset := len(data)
	order.PutUint32(data[rootNext:], uint32(maskOffset))
	fields := map[uint16]dngTestField{
		254: dngTestLong(order, 65540), 256: dngTestLong(order, uint32(width)), 257: dngTestLong(order, uint32(height)),
		258: dngTestShort(order, bits), 259: dngTestShort(order, compression), 262: dngTestShort(order, 52527),
		277: dngTestShort(order, 1), 284: dngTestShort(order, 1), 339: dngTestShort(order, sampleFormat),
		52526: {typeID: 2, count: uint32(len(name) + 1), data: append([]byte(name), 0)},
	}
	if len(jpegTables) != 0 && len(jpegTables[0]) != 0 {
		fields[347] = dngTestField{typeID: 7, count: uint32(len(jpegTables[0])), data: jpegTables[0]}
	}
	offsetTag := uint16(273)
	if tileWidth != 0 && tileHeight != 0 {
		offsetTag = 324
		fields[322], fields[323] = dngTestLong(order, uint32(tileWidth)), dngTestLong(order, uint32(tileHeight))
		fields[324], fields[325] = dngTestLong(order, 0), dngTestLong(order, uint32(len(samples)))
	} else {
		fields[273], fields[278], fields[279] = dngTestLong(order, 0), dngTestLong(order, uint32(height)), dngTestLong(order, uint32(len(samples)))
	}
	if predictor != 1 {
		fields[317] = dngTestShort(order, predictor)
	}
	if len(subArea) != 0 {
		fields[52536] = dngTestLong(order, subArea...)
	}
	tags := make([]int, 0, len(fields))
	for tag := range fields {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	ifd := make([]byte, 2+12*len(tags)+4)
	order.PutUint16(ifd, uint16(len(tags)))
	for index, tag := range tags {
		field := fields[uint16(tag)]
		entryStart := 2 + index*12
		order.PutUint16(ifd[entryStart:], uint16(tag))
		order.PutUint16(ifd[entryStart+2:], field.typeID)
		order.PutUint32(ifd[entryStart+4:], field.count)
		if len(field.data) <= 4 {
			copy(ifd[entryStart+8:entryStart+12], field.data)
		}
	}
	data = append(data, ifd...)
	for index, tag := range tags {
		field := fields[uint16(tag)]
		if len(field.data) <= 4 {
			continue
		}
		valueOffset := len(data)
		data = append(data, field.data...)
		entryStart := maskOffset + 2 + index*12 + 8
		order.PutUint32(data[entryStart:], uint32(valueOffset))
	}
	sampleOffset := len(data)
	data = append(data, samples...)
	for index, tag := range tags {
		if uint16(tag) == offsetTag {
			entryStart := maskOffset + 2 + index*12 + 8
			order.PutUint32(data[entryStart:], uint32(sampleOffset))
		}
	}
	return data
}

func TestLinearDNGRGBTablesSemanticMaskAndSubArea(t *testing.T) {
	order := binary.LittleEndian
	red := dngTestRGBTableData(order, 0, "subject", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{1, 0, 0} })
	background := dngTestRGBTableData(order, 0, "", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{0, 0, 1} })
	// RGBTables allows one background and one masked transform in the same tag.
	metadata := dngRGBIdentityCameraMetadata(order, dngTestRGBTablesData(order, 0, red, background))
	metadata[50829] = dngTestLong(order, 0, 1, 1, 3) // ActiveArea starts one sensor column in.
	data := linearDNGFixture(order, 3, 1, 3, []uint16{8192, 16384, 24576, 16384, 32768, 49152, 24576, 32768, 40960}, metadata)
	data = appendDNGSemanticMaskIFD(t, data, order, "subject", 1, 1, 8, 1, 1, 1, 0, 0, []byte{255}, []uint32{0, 0, 2, 1})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatalf("mask changed image dimensions: %v", img.Bounds())
	}
	// The one-pixel mask covers the left half of its 2x1 full area. Its right
	// half is uncovered and therefore uses the background table.
	left, right := img.NRGBA64At(0, 0), img.NRGBA64At(1, 0)
	if !(left.R > left.B && right.B > right.R) {
		t.Fatalf("semantic/background tables were not spatially applied: left=%v right=%v", left, right)
	}
}

func TestDNGSemanticMaskBilinearAndInvalidSubArea(t *testing.T) {
	mask := &dngSemanticMask{width: 2, height: 2, fullWidth: 4, fullHeight: 4, left: 1, top: 1, values: []float32{0, 1, 1, 0}}
	for _, point := range []struct {
		x, y int
		want float64
	}{{0, 0, 0}, {1, 1, 0}, {2, 1, 1}, {1, 2, 1}, {2, 2, 0}, {1, 0, 0}} {
		if got := mask.weight(point.x, point.y, 4, 4); math.Abs(got-point.want) > 1e-7 {
			t.Fatalf("mask weight (%d,%d)=%g want %g", point.x, point.y, got, point.want)
		}
	}
	decoded, err := dngTestSemanticMask(t, binary.LittleEndian, 1, 1, 8, 1, 1, 1, 0, 0, []byte{255}, []uint32{0, 2, 2, 1})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.left != 0 || decoded.top != 0 || decoded.fullWidth != 1 || decoded.fullHeight != 1 {
		t.Fatalf("invalid MaskSubArea was not ignored: %+v", decoded)
	}
}

func dngTestSemanticMask(t *testing.T, order binary.ByteOrder, width, height int, bits, compression, sampleFormat, predictor, tileWidth, tileHeight uint16, samples []byte, subArea []uint32, jpegTables ...[]byte) (*dngSemanticMask, error) {
	t.Helper()
	fixture := linearDNGFixture(order, max(1, width), max(1, height), 3, make([]uint16, max(1, width*height*3)), nil)
	fixture = appendDNGSemanticMaskIFD(t, fixture, order, "test", width, height, bits, compression, sampleFormat, predictor, tileWidth, tileHeight, samples, subArea, jpegTables...)
	fields, err := findLinearDNG(fixture)
	if err != nil {
		return nil, err
	}
	if len(fields.semanticIFDs) != 1 {
		return nil, fmt.Errorf("found %d semantic IFDs, want 1", len(fields.semanticIFDs))
	}
	maskFields := &dngFields{scanner: fields.scanner, entries: fields.semanticIFDs[0]}
	return readDNGSemanticMask(maskFields, width, height, 1)
}

func TestDNGSemanticMaskSampleDecoding(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		orderName := "little"
		if order == binary.BigEndian {
			orderName = "big"
		}
		t.Run(orderName, func(t *testing.T) {
			var floatSamples [8]byte
			order.PutUint32(floatSamples[:4], math.Float32bits(.25))
			order.PutUint32(floatSamples[4:], math.Float32bits(.75))
			var wordSamples [4]byte
			order.PutUint16(wordSamples[:2], 0)
			order.PutUint16(wordSamples[2:], 65535)
			var compressed bytes.Buffer
			writer := zlib.NewWriter(&compressed)
			if _, err := writer.Write([]byte{0, 255}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			gray := image.NewGray(image.Rect(0, 0, 2, 1))
			gray.SetGray(0, 0, color.Gray{Y: 0})
			gray.SetGray(1, 0, color.Gray{Y: 255})
			var jpegSamples bytes.Buffer
			if err := jpeg.Encode(&jpegSamples, gray, &jpeg.Options{Quality: 100}); err != nil {
				t.Fatal(err)
			}
			losslessJPEG, _, _ := dngTestLosslessJPEG(t, 2, 1, 8, 1, 0, 0, []uint16{0, 255})
			cases := []struct {
				name                                                        string
				bits, compression, format, predictor, tileWidth, tileHeight uint16
				samples                                                     []byte
				want                                                        []float32
				tolerance                                                   float64
			}{
				{name: "8-bit", bits: 8, compression: 1, format: 1, predictor: 1, samples: []byte{0, 255}, want: []float32{0, 1}},
				{name: "8-bit predictor", bits: 8, compression: 1, format: 1, predictor: 2, samples: []byte{100, 100}, want: []float32{100.0 / 255, 200.0 / 255}},
				{name: "16-bit", bits: 16, compression: 1, format: 1, predictor: 1, samples: wordSamples[:], want: []float32{0, 1}},
				{name: "32-bit float", bits: 32, compression: 1, format: 3, predictor: 1, samples: floatSamples[:], want: []float32{.25, .75}},
				{name: "deflate", bits: 8, compression: 8, format: 1, predictor: 1, samples: compressed.Bytes(), want: []float32{0, 1}},
				{name: "padded tile", bits: 8, compression: 1, format: 1, predictor: 1, tileWidth: 2, tileHeight: 2, samples: []byte{0, 255, 17, 23}, want: []float32{0, 1}},
				{name: "lossy JPEG", bits: 8, compression: 34892, format: 1, predictor: 1, samples: jpegSamples.Bytes(), want: []float32{0, 1}, tolerance: .03},
				{name: "lossless JPEG", bits: 8, compression: 7, format: 1, predictor: 1, samples: losslessJPEG, want: []float32{0, 1}},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					mask, err := dngTestSemanticMask(t, order, 2, 1, test.bits, test.compression, test.format, test.predictor, test.tileWidth, test.tileHeight, test.samples, nil)
					if err != nil {
						t.Fatal(err)
					}
					for i := range test.want {
						if math.Abs(float64(mask.values[i]-test.want[i])) > max(1e-7, test.tolerance) {
							t.Fatalf("mask sample %d=%g want %g", i, mask.values[i], test.want[i])
						}
					}
				})
			}
		})
	}
}

func TestDNGRGBTablesMalformedRejected(t *testing.T) {
	order := binary.LittleEndian
	valid := dngTestRGBTableData(order, 0, "", 2, 2, 0, 4, 0, func(r, g, b int) [3]float64 {
		return [3]float64{float64(r), float64(g), float64(b)}
	})
	for name, mutate := range map[string]func([]byte) []byte{
		"bad composite":  func(data []byte) []byte { order.PutUint32(data[4:], 2); return data },
		"bad divisions":  func(data []byte) []byte { data[10] = 1; return data },
		"bad pixel type": func(data []byte) []byte { data[11] = 3; return data },
		"bad gamut":      func(data []byte) []byte { data[14] = 2; return data },
		"truncated":      func(data []byte) []byte { return data[:len(data)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			blob := append([]byte(nil), valid...)
			blob = mutate(blob)
			if _, err := readTestDNGRGBTables(t, order, blob); err == nil {
				t.Fatal("malformed RGBTables accepted")
			}
		})
	}
}

func FuzzDNGRGBTables(f *testing.F) {
	order := binary.LittleEndian
	seed := dngTestRGBTableData(order, 0, "", 2, 2, 0, 4, 0, func(r, g, b int) [3]float64 {
		return [3]float64{float64(r), float64(g), float64(b)}
	})
	f.Add(seed)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, blob []byte) {
		if len(blob) > maxDNGRGBTableBytes {
			return
		}
		_, _ = readTestDNGRGBTables(t, order, blob)
	})
}
