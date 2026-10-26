package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestAdobeCompressedLinearDNGDirectDecode(t *testing.T) {
	converter, err := resolveDNGConverter("")
	if err != nil {
		t.Skip("Adobe DNG Converter is not installed")
	}
	order := binary.LittleEndian
	pixels := make([]uint16, 16*16*3)
	for index := range pixels {
		pixels[index] = uint16((index*37 + index/7*19) % 256)
	}
	metadata := map[uint16]dngTestField{
		258:   dngTestShort(order, 8, 8, 8),
		50717: dngTestLong(order, 255),
		50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
		50728: dngTestDouble(order, 1, 1, 1),
	}
	sourceData := linearDNGFixture(order, 16, 16, 3, pixels, metadata)
	root := t.TempDir()
	source := filepath.Join(root, "source.dng")
	if err := os.WriteFile(source, sourceData, 0600); err != nil {
		t.Fatal(err)
	}
	losslessOutput := filepath.Join(root, "lossless")
	if err := os.Mkdir(losslessOutput, 0700); err != nil {
		t.Fatal(err)
	}
	if err := runDNGConverter(converter, source, losslessOutput, "lossless.dng", false, "lossless", 0); err != nil {
		t.Fatalf("convert synthetic Linear DNG to Adobe lossless DNG: %v", err)
	}
	losslessData, err := os.ReadFile(filepath.Join(losslessOutput, "lossless.dng"))
	if err != nil {
		t.Fatal(err)
	}
	losslessFields, err := findLinearDNG(losslessData)
	if err != nil {
		t.Fatalf("read Adobe lossless DNG: %v", err)
	}
	losslessCompression := losslessFields.integer(259, 0)
	t.Logf("Adobe DNG Converter emitted lossless DNG compression=%d photometric=%d", losslessCompression, losslessFields.integer(262, 0))
	if losslessCompression != 7 {
		t.Fatalf("expected Adobe lossless output to use JPEG SOF3 compression 7, got %d", losslessCompression)
	}
	losslessImage, err := decodeLinearDNG(losslessData)
	if err != nil {
		t.Fatalf("directly decode Adobe lossless Linear DNG: %v", err)
	}
	inputImage, err := decodeLinearDNG(sourceData)
	if err != nil {
		t.Fatal(err)
	}
	assertLinearDNGImagesEqual(t, losslessImage, inputImage)

	output := filepath.Join(root, "out")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := runDNGConverter(converter, source, output, "lossy.dng", false, "lossy", 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "lossy.dng"))
	if err != nil {
		t.Fatal(err)
	}
	fields, err := findLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	width, height := fields.integer(256, 0), fields.integer(257, 0)
	channels, bits := fields.integer(277, 1), fields.values(258)
	if len(bits) == 0 {
		t.Fatalf("Adobe DNG output has no BitsPerSample: %v", fields.err)
	}
	depth := int(bits[0])
	if fields.err != nil || fields.integer(262, 0) != 34892 || fields.integer(259, 0) != 34892 || width != 16 || height != 16 || channels != 3 || depth != 8 {
		t.Fatalf("unexpected Adobe lossy DNG output: photometric=%d compression=%d dimensions=%dx%d channels=%d depth=%d: %v", fields.integer(262, 0), fields.integer(259, 0), width, height, channels, depth, fields.err)
	}
	offsetTag, lengthTag := uint16(273), uint16(279)
	blockKind := "strip"
	if _, tiled := fields.entries[324]; tiled {
		blockKind = "tile"
		offsetTag, lengthTag = 324, 325
		if fields.integer(322, 0) != width || fields.integer(323, 0) != height {
			t.Fatalf("Adobe DNG tile dimensions do not match this fixture: %dx%d for %dx%d", fields.integer(322, 0), fields.integer(323, 0), width, height)
		}
	}
	offsets, lengths := fields.values(offsetTag), fields.values(lengthTag)
	if len(offsets) != 1 || len(lengths) != 1 {
		t.Fatalf("expected one Adobe JPEG block, got offsets=%v lengths=%v", offsets, lengths)
	}
	t.Logf("Adobe DNG Converter emitted Compression 34892 as one %s: %dx%d, %.0f bytes", blockKind, width, height, lengths[0])
	if fields.err != nil || offsets[0] < 0 || lengths[0] <= 0 || !tiffRange(uint64(offsets[0]), uint64(lengths[0]), uint64(len(data))) {
		t.Fatalf("Adobe JPEG block is outside the output DNG: offset=%v length=%v: %v", offsets[0], lengths[0], fields.err)
	}
	encoded := data[int(offsets[0]):int(offsets[0]+lengths[0])]
	tables, err := dngRawTag(fields, 347)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := joinDNGJPEGTables(encoded, tables)
	if err != nil {
		t.Fatal(err)
	}
	referenceJPEG, err := jpeg.Decode(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("decode Adobe JPEG block with Go standard decoder: %v", err)
	}
	referenceSamples := make([]byte, 0, width*height*channels)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			red, green, blue, _ := referenceJPEG.At(x, y).RGBA()
			referenceSamples = append(referenceSamples, byte(red>>8), byte(green>>8), byte(blue>>8))
		}
	}
	gotSamples, err := decodeDNGJPEGBlock(encoded, tables, width, height, channels, depth, order)
	if err != nil {
		t.Fatalf("directly decode Adobe JPEG block: %v", err)
	}
	if !bytes.Equal(gotSamples, referenceSamples) {
		t.Fatalf("direct JPEG samples differ from Go reference decoder (got %d bytes, want %d)", len(gotSamples), len(referenceSamples))
	}

	// Replace only Compression and the image block range in a copy of Adobe's DNG.
	// This gives the same metadata and samples through the uncompressed path.
	baselineData := make([]byte, len(data), len(data)+len(referenceSamples))
	copy(baselineData, data)
	rawOffset := len(baselineData)
	baselineData = append(baselineData, referenceSamples...)
	baselineFields, err := findLinearDNG(baselineData)
	if err != nil {
		t.Fatal(err)
	}
	setDNGTestScalar(t, baselineFields, 259, 1)
	setDNGTestScalar(t, baselineFields, offsetTag, rawOffset)
	setDNGTestScalar(t, baselineFields, lengthTag, len(referenceSamples))
	wantImage, err := decodeLinearDNG(baselineData)
	if err != nil {
		t.Fatalf("decode uncompressed Adobe-metadata reference: %v", err)
	}
	gotImage, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatalf("decode Adobe-generated lossy DNG directly: %v", err)
	}
	assertLinearDNGImagesEqual(t, gotImage, wantImage)

	t.Setenv("PATH", t.TempDir())
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	assertAdobeDNGDirectRender(t, root, "lossless-generated.dng", losslessData, inputImage, 16, 16, uint16(losslessFields.integer(274, 1)))
	assertAdobeDNGDirectRender(t, root, "lossy-generated.dng", data, wantImage, width, height, uint16(fields.integer(274, 1)))
}

func assertAdobeDNGDirectRender(t *testing.T, root, name string, dng []byte, wantImage *image.NRGBA64, width, height int, orientation uint16) {
	t.Helper()
	generatedSource := filepath.Join(root, name)
	if err := os.WriteFile(generatedSource, dng, 0600); err != nil {
		t.Fatal(err)
	}
	gotTIFF, config, format, err := readRenderInputWithOptions(Frame{Image: generatedSource}, imageInputOptions{RawDevelop: true})
	if err != nil {
		t.Fatalf("read Adobe-generated %s with external converter unavailable: %v", name, err)
	}
	wantTIFF, err := encodeDevelopedDNGTIFF(wantImage, orientation)
	if err != nil {
		t.Fatal(err)
	}
	if format != "tiff" || config.Width != width || config.Height != height || !bytes.Equal(gotTIFF, wantTIFF) {
		t.Fatalf("Adobe %s raw-develop result format=%s config=%+v matches reference=%t", name, format, config, bytes.Equal(gotTIFF, wantTIFF))
	}
	stem := filepath.Join(root, name[:len(name)-len(filepath.Ext(name))])
	referenceTIFF := stem + ".reference.tif"
	if err := os.WriteFile(referenceTIFF, wantTIFF, 0600); err != nil {
		t.Fatal(err)
	}
	rawAVI, referenceAVI := stem+".avi", stem+".reference.avi"
	if err := RenderSequence([]Frame{{Image: generatedSource}}, rawAVI, RenderOptions{FPS: 24, Quality: 95, RawDevelop: true}); err != nil {
		t.Fatalf("render Adobe-generated %s without external converter: %v", name, err)
	}
	if err := RenderSequence([]Frame{{Image: referenceTIFF}}, referenceAVI, RenderOptions{FPS: 24, Quality: 95}); err != nil {
		t.Fatalf("render reference TIFF for Adobe-generated %s: %v", name, err)
	}
	rawVideo, err := os.ReadFile(rawAVI)
	if err != nil {
		t.Fatal(err)
	}
	referenceVideo, err := os.ReadFile(referenceAVI)
	if err != nil {
		t.Fatal(err)
	}
	rawMovie, referenceMovie := bytes.Index(rawVideo, []byte("movi")), bytes.Index(referenceVideo, []byte("movi"))
	if rawMovie < 0 || referenceMovie < 0 || !bytes.Equal(rawVideo[rawMovie:], referenceVideo[referenceMovie:]) {
		t.Fatalf("Adobe-generated %s AVI differs from its uncompressed metadata-preserving reference", name)
	}
}

func setDNGTestScalar(t *testing.T, fields *dngFields, tag uint16, value int) {
	t.Helper()
	entry, ok := fields.entries[tag]
	if !ok || entry.count != 1 {
		t.Fatalf("missing scalar TIFF tag %d", tag)
	}
	switch entry.typeID {
	case 3:
		if value < 0 || value > 0xffff {
			t.Fatalf("TIFF tag %d value %d exceeds SHORT", tag, value)
		}
		fields.scanner.order.PutUint16(entry.value, uint16(value))
	case 4:
		if value < 0 || uint64(value) > 0xffffffff {
			t.Fatalf("TIFF tag %d value %d exceeds LONG", tag, value)
		}
		fields.scanner.order.PutUint32(entry.value, uint32(value))
	default:
		t.Fatalf("TIFF tag %d has unsupported scalar type %d", tag, entry.typeID)
	}
}
