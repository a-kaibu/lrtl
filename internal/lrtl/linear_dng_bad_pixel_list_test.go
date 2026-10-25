package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testDNGBadPixelList(phase uint32, points [][2]int32, rectangles [][4]int32) testDNGOpcode {
	dataSize := uint32(12 + len(points)*8 + len(rectangles)*16)
	data := binary.BigEndian.AppendUint32(nil, dataSize)
	data = binary.BigEndian.AppendUint32(data, phase)
	data = binary.BigEndian.AppendUint32(data, uint32(len(points)))
	data = binary.BigEndian.AppendUint32(data, uint32(len(rectangles)))
	for _, point := range points {
		data = binary.BigEndian.AppendUint32(data, uint32(point[0]))
		data = binary.BigEndian.AppendUint32(data, uint32(point[1]))
	}
	for _, rectangle := range rectangles {
		for _, coordinate := range rectangle {
			data = binary.BigEndian.AppendUint32(data, uint32(coordinate))
		}
	}
	return testDNGOpcode{5, 0x01030000, 0, data}
}

func testDNGBadPixelRamp(width, height int) []float32 {
	pixels := make([]float32, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixels[y*width+x] = float32(1000+20*x+30*y) / 65535
		}
	}
	return pixels
}

func TestDNGBadPixelListUsesSDKBayerPhaseGrouping(t *testing.T) {
	for phase, wantGreen := range []bool{true, false, false, true} {
		if got := dngSDKIsGreen(0, 0, uint32(phase)); got != wantGreen {
			t.Fatalf("phase %d top-left green=%t want %t", phase, got, wantGreen)
		}
	}
}

func TestParseDNGBadPixelList(t *testing.T) {
	opcode, err := parseDNGBadPixelList(testDNGBadPixelList(2,
		[][2]int32{{8, 7}, {3, 4}}, [][4]int32{{1, 2, 4, 6}}).payload)
	if err != nil {
		t.Fatal(err)
	}
	if opcode.bayerPhase != 2 || !reflect.DeepEqual(opcode.points, []image.Point{image.Pt(4, 3), image.Pt(7, 8)}) || !reflect.DeepEqual(opcode.rectangles, []image.Rectangle{image.Rect(2, 1, 6, 4)}) {
		t.Fatalf("parsed list=%+v", opcode)
	}
	for name, payload := range map[string][]byte{
		"truncated header":   []byte{0, 0, 0, 1},
		"invalid phase":      testDNGBadPixelList(4, nil, nil).payload,
		"count mismatch":     append(testDNGBadPixelList(0, nil, nil).payload, 0),
		"empty rectangle":    testDNGBadPixelList(0, nil, [][4]int32{{2, 3, 2, 4}}).payload,
		"inverted rectangle": testDNGBadPixelList(0, nil, [][4]int32{{2, 4, 3, 3}}).payload,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGBadPixelList(payload); err == nil {
				t.Fatal("invalid FixBadPixelsList payload accepted")
			}
		})
	}
}

func TestApplyDNGBadPixelListUsesDirectionalSameCFAInterpolation(t *testing.T) {
	const width, height = 9, 9
	for _, phase := range []uint32{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("phase-%d", phase), func(t *testing.T) {
			source := testDNGBadPixelRamp(width, height)
			const x, y = 4, 4
			source[y*width+x] = 1
			original := append([]float32(nil), source...)
			destination := make([]float32, len(source))
			opcode := dngBadPixelList{bayerPhase: phase, points: []image.Point{image.Pt(x, y)}}
			if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
				t.Fatal(err)
			}
			if got := dngSample16(destination[y*width+x]); got != 1200 {
				t.Fatalf("repaired sample=%d want 1200", got)
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("source buffer was modified")
			}
			if got := dngSample16(destination[0]); got != dngSample16(source[0]) {
				t.Fatalf("unlisted sample changed: %d", got)
			}
		})
	}
}

func TestApplyDNGBadPixelListExcludesOtherBadPointsAndRectangles(t *testing.T) {
	const width, height = 9, 9
	source := testDNGBadPixelRamp(width, height)
	for _, point := range []image.Point{image.Pt(4, 4), image.Pt(6, 4)} {
		source[point.Y*width+point.X] = 1
	}
	destination := make([]float32, len(source))
	opcode := dngBadPixelList{bayerPhase: 0, points: []image.Point{image.Pt(4, 4), image.Pt(6, 4)}}
	if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
		t.Fatal(err)
	}
	for point, want := range map[image.Point]uint16{image.Pt(4, 4): 1200, image.Pt(6, 4): 1240} {
		if got := dngSample16(destination[point.Y*width+point.X]); got != want {
			t.Fatalf("point %v repaired to %d, want %d", point, got, want)
		}
	}

	source = testDNGBadPixelRamp(width, height)
	source[4*width+4] = 1
	source[3*width+3] = float32(1000+20*5+30*5) / 65535
	opcode = dngBadPixelList{bayerPhase: 0, points: []image.Point{image.Pt(4, 4)}, rectangles: []image.Rectangle{image.Rect(3, 3, 4, 4)}}
	if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
		t.Fatal(err)
	}
	if got := dngSample16(destination[4*width+4]); got != 1200 {
		t.Fatalf("point adjacent to listed rectangle repaired to %d, want 1200", got)
	}

	source = testDNGBadPixelRamp(width, height)
	for y := 4; y < 5; y++ {
		for x := 3; x < 6; x++ {
			source[y*width+x] = 1
		}
	}
	opcode = dngBadPixelList{bayerPhase: 0, rectangles: []image.Rectangle{image.Rect(3, 4, 6, 5)}}
	if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
		t.Fatal(err)
	}
	for x := 3; x < 6; x++ {
		want := uint16(1000 + 20*x + 30*4)
		if got := dngSample16(destination[4*width+x]); got != want {
			t.Fatalf("rectangle sample (%d,4)=%d want %d", x, got, want)
		}
	}

	source = testDNGBadPixelRamp(width, height)
	for y := 3; y < 6; y++ {
		source[y*width+4] = 1
	}
	opcode = dngBadPixelList{bayerPhase: 0, rectangles: []image.Rectangle{image.Rect(4, 3, 5, 6)}}
	if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
		t.Fatal(err)
	}
	for y := 3; y < 6; y++ {
		want := uint16(1000 + 20*4 + 30*y)
		if got := dngSample16(destination[y*width+4]); got != want {
			t.Fatalf("column rectangle sample (4,%d)=%d want %d", y, got, want)
		}
	}
}

func TestApplyDNGBadPixelListLeavesPixelsWithoutOpposingSamples(t *testing.T) {
	const width, height = 3, 3
	source := make([]float32, width*height)
	for i := range source {
		source[i] = testDNGNormalizedSample(1000)
	}
	source[0] = testDNGNormalizedSample(65535)
	destination := make([]float32, len(source))
	opcode := dngBadPixelList{bayerPhase: 0, points: []image.Point{image.Pt(0, 0)}}
	if err := applyDNGBadPixelList(source, destination, width, height, image.Rect(0, 0, width, height), opcode); err != nil {
		t.Fatal(err)
	}
	if got := dngSample16(destination[0]); got != 65535 {
		t.Fatalf("unrepairable edge value changed to %d", got)
	}
}

func TestApplyDNGBadPixelListRetainsCoordinatesAfterTrimAndIgnoresOutOfBounds(t *testing.T) {
	const width, height = 5, 5
	const originX, originY = 2, 3
	currentBounds := image.Rect(originX, originY, originX+width, originY+height)
	source := testDNGBadPixelRamp(width, height)
	source[2*width+2] = 1
	destination := make([]float32, len(source))
	opcode := dngBadPixelList{bayerPhase: 0, points: []image.Point{image.Pt(originX+2, originY+2), image.Pt(-1, -1)}}
	if err := applyDNGBadPixelList(source, destination, width, height, currentBounds, opcode); err != nil {
		t.Fatal(err)
	}
	if got := dngSample16(destination[2*width+2]); got != 1100 {
		t.Fatalf("trimmed point=%d want 1100", got)
	}
}

func TestDNGImageOpcodeBadPixelListFollowsRecordedOrder(t *testing.T) {
	const width, height = 9, 9
	const centerX, centerY = 4, 4
	var invalid []image.Point
	directions := [4][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	for _, direction := range directions {
		for distance := 1; distance <= 4; distance++ {
			dx, dy := direction[0]*distance, direction[1]*distance
			left, right := image.Pt(centerX-dx, centerY-dy), image.Pt(centerX+dx, centerY+dy)
			if !dngSameBayerColor(centerX, centerY, left.X, left.Y, 0) || !dngSameBayerColor(centerX, centerY, right.X, right.Y, 0) {
				continue
			}
			if left != image.Pt(2, 4) && left != image.Pt(6, 4) {
				invalid = append(invalid, left)
			}
			if right != image.Pt(2, 4) && right != image.Pt(6, 4) {
				invalid = append(invalid, right)
			}
		}
	}
	invalid = append(invalid, image.Pt(centerX, centerY))
	points := make([]image.Point, len(invalid))
	copy(points, invalid)
	source := make([]float32, width*height)
	for i := range source {
		source[i] = testDNGNormalizedSample(12000)
	}
	source[4*width+2], source[4*width+6] = testDNGNormalizedSample(10000), testDNGNormalizedSample(30000)
	source[centerY*width+centerX] = 1
	encodedPoints := make([]image.Point, len(points))
	copy(encodedPoints, points)
	list := dngBadPixelList{bayerPhase: 0, points: encodedPoints}
	polynomial := &dngPointOpcode{dngOpcodeArea: dngOpcodeArea{planes: 1, rowPitch: 1, colPitch: 1}, id: 8, coefficients: []float64{0, 0, 1}}

	listThenMap, _, err := applyDNGImageOpcodesWithBounds([]dngImageOpcode{{badPixelList: &list}, {point: polynomial}}, append([]float32(nil), source...), width, height, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	mapThenList, _, err := applyDNGImageOpcodesWithBounds([]dngImageOpcode{{point: polynomial}, {badPixelList: &list}}, append([]float32(nil), source...), width, height, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	center := centerY*width + centerX
	mean := float64(20000) / 65535
	wantListThenMap := float32(mean * mean)
	leftSquared := uint16(math.Round(math.Pow(float64(10000)/65535, 2) * 65535))
	rightSquared := uint16(math.Round(math.Pow(float64(30000)/65535, 2) * 65535))
	wantMapThenList := float32(float64((uint32(leftSquared)+uint32(rightSquared)+1)/2) / 65535)
	if math.Abs(float64(listThenMap[center]-wantListThenMap)) > 1e-7 {
		t.Fatalf("List→Map center=%.9g want %.9g", listThenMap[center], wantListThenMap)
	}
	if math.Abs(float64(mapThenList[center]-wantMapThenList)) > 1e-7 {
		t.Fatalf("Map→List center=%.9g want %.9g", mapThenList[center], wantMapThenList)
	}
	if listThenMap[center] == mapThenList[center] {
		t.Fatal("bad-pixel interpolation ignored the encoded opcode order")
	}
}

func TestLinearDNGFixBadPixelsListAcrossByteOrders(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		const width, height = 9, 9
		pixels := make([]uint16, width*height)
		for i := range pixels {
			pixels[i] = 10000
		}
		pixels[4*width+4] = 65535
		metadata := map[uint16]dngTestField{50717: dngTestLong(order, 65535), 51009: testDNGOpcodeList(testDNGBadPixelList(0, [][2]int32{{4, 4}}, nil))}
		img, err := decodeLinearDNG(linearDNGFixture(order, width, height, 1, pixels, metadata))
		if err != nil {
			t.Fatal(err)
		}
		want := testDNGLinearToSRGB16(10000)
		pixel := img.NRGBA64At(4, 4)
		if pixel.R != want || pixel.G != want || pixel.B != want {
			t.Fatalf("%s center=%v want gray %d", order, pixel, want)
		}
	}
	if _, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, 3, 3, 3,
		make([]uint16, 3*3*3), map[uint16]dngTestField{51009: testDNGOpcodeList(testDNGBadPixelList(0, nil, nil))})); err == nil {
		t.Fatal("multi-channel FixBadPixelsList accepted")
	}
}

func TestRAWBadPixelListCLIThroughTIFFAndAVI(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	const width, height = 16, 16
	pixels := make([]uint16, width*height)
	for i := range pixels {
		pixels[i] = 10000
	}
	pixels[8*width+8] = 65535
	fixture := linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{
		50717: dngTestLong(binary.LittleEndian, 65535),
		51022: testDNGOpcodeList(testDNGBadPixelList(0, [][2]int32{{8, 8}}, nil)),
	})
	fixturePath := filepath.Join(fixtureDir, "badpixel-list.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	sequence := t.TempDir()
	for frame := 1; frame <= 2; frame++ {
		if err := os.WriteFile(filepath.Join(sequence, fmt.Sprintf("frame%d.nef", frame)), fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, sequence)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", sequence, "--output", output, "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) != 0 {
		t.Fatalf("RAW export failed: %s", stderr.String())
	}
	want := testDNGLinearToSRGB16(10000)
	for frame := 1; frame <= 2; frame++ {
		img := readExportedTIFF(t, filepath.Join(output, fmt.Sprintf("frame%06d.tif", frame)))
		r, g, b, _ := img.At(8, 8).RGBA()
		if uint16(r) != want || g != r || b != r {
			t.Fatalf("frame %d TIFF center=(%d,%d,%d), want %d", frame, r, g, b, want)
		}
	}
	video := filepath.Join(t.TempDir(), "bad-pixel-list.avi")
	if Main([]string{"render", output, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatalf("AVI render failed: %s", stderr.String())
	}
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 || frames == 2 {
			t.Fatal("invalid AVI JPEG frame data")
		}
		decoded, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		r, _, _, _ := decoded.At(8, 8).RGBA()
		if math.Abs(float64(r>>8)-float64(want)/257) > 2 {
			t.Fatalf("AVI frame %d center=%d want about %d", frames, r>>8, want/257)
		}
		frames++
		data = data[end+2:]
	}
	if frames != 2 {
		t.Fatalf("AVI has %d frames, want 2", frames)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, sequence)) {
		t.Fatal("RAW source files were changed")
	}
}
