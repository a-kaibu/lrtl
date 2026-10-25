package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"strconv"
	"testing"
)

func parseTestStage1Warp(opcode testDNGOpcode, channels int) (*dngWarp, error) {
	switch opcode.id {
	case 1:
		return parseDNGWarpRectilinear(opcode.payload, channels)
	case 2:
		return parseDNGWarpFisheye(opcode.payload, channels)
	case 14:
		return parseDNGWarpRectilinear2(opcode.payload, channels)
	default:
		return nil, fmt.Errorf("test opcode %d is not a warp", opcode.id)
	}
}

func referenceStage1WarpSample(pixels []uint16, width, height, channels, plane int, x, y float64) float64 {
	x = min(float64(width-1), max(0, x))
	y = min(float64(height-1), max(0, y))
	left, top := int(math.Floor(x)), int(math.Floor(y))
	value := 0.0
	for row := top - 1; row <= top+2; row++ {
		sy := min(height-1, max(0, row))
		for col := left - 1; col <= left+2; col++ {
			sx := min(width-1, max(0, col))
			weight := referenceDNGCubicWeight(x-float64(col)) * referenceDNGCubicWeight(y-float64(row))
			sample := float32(pixels[(sy*width+sx)*channels+plane]) / 65535
			value += float64(sample) * weight
		}
	}
	return float64(float32(min(1, max(0, value)))) * 65535
}

func referenceStage1RectilinearSource(width, height int, verticalScale, centerX, centerY float64, coefficients [6]float64, x, y int) (float64, float64) {
	squareHeight := math.Round(float64(height) * verticalScale)
	cx, cy := float64(width)*centerX, float64(height)*centerY
	squareCenterY := squareHeight * centerY
	radius := math.Hypot(max(math.Abs(cx), math.Abs(float64(width)-cx)), max(math.Abs(squareCenterY), math.Abs(squareHeight-squareCenterY)))
	dx, dy := float64(x)-cx, float64(y)-cy
	u, v := dx/radius, dy*verticalScale/radius
	r2 := min(1, u*u+v*v)
	radial := coefficients[0] + coefficients[1]*r2 + coefficients[2]*r2*r2 + coefficients[3]*r2*r2*r2
	tangentialX := 2*coefficients[4]*u*v + coefficients[5]*(r2+2*u*u)
	tangentialY := 2*coefficients[5]*u*v + coefficients[4]*(r2+2*v*v)
	return cx + dx*radial + radius*tangentialX, cy + dy*radial + radius*tangentialY/verticalScale
}

func TestApplyDNGStage1WarpRawAgainstIndependentReference(t *testing.T) {
	const width, height, channels, verticalScale = 9, 7, 2, 1.6
	pixels := make([]uint16, width*height*channels)
	for index := range pixels {
		pixels[index] = uint16((index*7919 + index*index*37 + 1234) % 65536)
	}

	type warpCase struct {
		name      string
		opcode    testDNGOpcode
		sourceFor func(plane, x, y int) (float64, float64)
	}
	const centerX, centerY = .42, .58
	rectilinearCoefficients := [][6]float64{
		{.91, .15, -.03, .007, .014, -.009},
		{1.08, -.16, .08, -.02, -.017, .011},
	}
	fisheyeCoefficients := [][4]float64{
		{.91, .12, -.035, .008},
		{1.05, -.08, .02, -.003},
	}
	rectilinear2Sets := []testDNGRectilinear2Parameters{
		{radial: [15]float64{.92, .16, -.04, .008}, tangential: [2]float64{.012, -.008}, minValidRadius: .04, maxValidRadius: .95, centerX: centerX, centerY: centerY},
		{radial: [15]float64{1.07, -.12, .045, -.006}, tangential: [2]float64{-.009, .013}, minValidRadius: .1, maxValidRadius: .82, centerX: .61, centerY: .39},
	}
	warpCases := []warpCase{
		{
			name:   "WarpRectilinear",
			opcode: testDNGWarp(centerX, centerY, rectilinearCoefficients...),
			sourceFor: func(plane, x, y int) (float64, float64) {
				return referenceStage1RectilinearSource(width, height, verticalScale, centerX, centerY, rectilinearCoefficients[plane], x, y)
			},
		},
		{
			name:   "WarpFisheye",
			opcode: testDNGWarpFisheye(centerX, centerY, fisheyeCoefficients...),
			sourceFor: func(plane, x, y int) (float64, float64) {
				return referenceFisheyeSource(width, height, centerX, centerY, verticalScale, fisheyeCoefficients[plane], x, y)
			},
		},
		{
			name:   "WarpRectilinear2",
			opcode: testDNGWarpRectilinear2(false, rectilinear2Sets...),
			sourceFor: func(plane, x, y int) (float64, float64) {
				return referenceDNGWarpRectilinear2Source(width, height, verticalScale, rectilinear2Sets[plane], false, x, y)
			},
		},
	}

	for _, test := range warpCases {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseTestStage1Warp(test.opcode, channels)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]uint16, len(pixels))
			if err := applyDNGWarpRaw(pixels, got, width, height, channels, verticalScale, *parsed); err != nil {
				t.Fatal(err)
			}
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					for plane := 0; plane < channels; plane++ {
						sx, sy := test.sourceFor(plane, x, y)
						want := uint16(math.Round(referenceStage1WarpSample(pixels, width, height, channels, plane, sx, sy)))
						index := (y*width+x)*channels + plane
						if delta := math.Abs(float64(got[index]) - float64(want)); delta > 1 {
							t.Fatalf("sample (%d,%d) plane %d=%d want %d (delta %g)", x, y, plane, got[index], want, delta)
						}
					}
				}
			}
		})
	}
}

func TestApplyDNGStage1WarpIdentityPreservesEveryUint16Sample(t *testing.T) {
	const width, height = 256, 256
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = uint16(index)
	}
	destination := make([]uint16, len(pixels))
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{1}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGWarpRaw(pixels, destination, width, height, 1, 1, *warp); err != nil {
		t.Fatal(err)
	}
	for index := range pixels {
		if destination[index] != pixels[index] {
			t.Fatalf("sample %d=%d want %d", index, destination[index], pixels[index])
		}
	}
}

func TestApplyDNGStage1WarpRejectsInvalidInputs(t *testing.T) {
	pixels := []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9}
	destination := make([]uint16, len(pixels))
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{1}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, verticalScale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if err := applyDNGWarpRaw(pixels, destination, 3, 3, 1, verticalScale, *warp); err == nil {
			t.Fatalf("invalid pixel aspect %v accepted", verticalScale)
		}
	}
	if err := applyDNGWarpRaw(pixels, destination[:len(destination)-1], 3, 3, 1, 1, *warp); err == nil {
		t.Fatal("short destination accepted")
	}
	largeWarp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{math.MaxFloat64, math.MaxFloat64}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGWarpRaw(pixels, destination, 3, 3, 1, 1, *largeWarp); err == nil {
		t.Fatal("non-finite mapped coordinate accepted")
	}
}

func FuzzDNGStage1WarpRawSamples(f *testing.F) {
	f.Add([]byte{128, 180, 96, 155, 119, 137, 105, 211})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) < 8 || len(input) > 64 {
			return
		}
		const width, height = 8, 6
		coefficients := [6]float64{
			.8 + float64(input[0])/255*.4,
			(float64(input[1])/255 - .5) * .4,
			(float64(input[2])/255 - .5) * .12,
			(float64(input[3])/255 - .5) * .04,
			(float64(input[4])/255 - .5) * .08,
			(float64(input[5])/255 - .5) * .08,
		}
		centerX := .2 + float64(input[6])/255*.6
		centerY := .2 + float64(input[7])/255*.6
		verticalScale := .5 + float64(input[0])/255*1.5
		pixels := make([]uint16, width*height)
		for index := range pixels {
			pixels[index] = uint16(input[index%len(input)]) * 257
		}
		warp, err := parseDNGWarpRectilinear(testDNGWarp(centerX, centerY, coefficients).payload, 1)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]uint16, len(pixels))
		if err := applyDNGWarpRaw(pixels, got, width, height, 1, verticalScale, *warp); err != nil {
			t.Fatal(err)
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				sx, sy := referenceStage1RectilinearSource(width, height, verticalScale, centerX, centerY, coefficients, x, y)
				want := uint16(math.Round(referenceStage1WarpSample(pixels, width, height, 1, 0, sx, sy)))
				if delta := math.Abs(float64(got[y*width+x]) - float64(want)); delta > 1 {
					t.Fatalf("sample (%d,%d)=%d want %d (delta %g)", x, y, got[y*width+x], want, delta)
				}
			}
		}
	})
}

func TestDNGStage1WarpAfterTrimUsesTrimmedFrameAndDefaultScale(t *testing.T) {
	const width, height = 9, 7
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = uint16((index*7919 + index*index*37 + 1234) % 65536)
	}
	originalPixels := append([]uint16(nil), pixels...)
	trim := image.Rect(1, 1, 8, 6)
	opcode, err := parseTestStage1Warp(testDNGWarp(.43, .61, [6]float64{.94, .12, -.03, .004, .01, -.007}), 1)
	if err != nil {
		t.Fatal(err)
	}
	verticalScale := 1.75
	got, gotWidth, gotHeight, activeArea, _, _, err := applyDNGStage1Opcodes([]dngImageOpcode{{trim: &trim}, {warp: opcode}}, pixels, width, height, 1, image.Rect(0, 0, width, height), verticalScale)
	if err != nil {
		t.Fatal(err)
	}
	if gotWidth != trim.Dx() || gotHeight != trim.Dy() || activeArea != image.Rect(0, 0, trim.Dx(), trim.Dy()) {
		t.Fatalf("dimensions=%dx%d active=%v", gotWidth, gotHeight, activeArea)
	}
	trimmed := make([]uint16, trim.Dx()*trim.Dy())
	for y := 0; y < trim.Dy(); y++ {
		copy(trimmed[y*trim.Dx():(y+1)*trim.Dx()], originalPixels[(trim.Min.Y+y)*width+trim.Min.X:(trim.Min.Y+y)*width+trim.Max.X])
	}
	coefficients := [6]float64{.94, .12, -.03, .004, .01, -.007}
	for y := 0; y < gotHeight; y++ {
		for x := 0; x < gotWidth; x++ {
			sx, sy := referenceStage1RectilinearSource(gotWidth, gotHeight, verticalScale, .43, .61, coefficients, x, y)
			want := uint16(math.Round(referenceStage1WarpSample(trimmed, gotWidth, gotHeight, 1, 0, sx, sy)))
			index := y*gotWidth + x
			if delta := math.Abs(float64(got[index]) - float64(want)); delta > 1 {
				t.Fatalf("trimmed sample (%d,%d)=%d want %d (delta %g)", x, y, got[index], want, delta)
			}
		}
	}
}

func TestLinearDNGStage1WarpOpcodesDecodeForBothByteOrders(t *testing.T) {
	const width, height = 8, 6
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = uint16(5000 + (index*7919+index*index*83)%56000)
	}
	opcodes := []testDNGOpcode{
		testDNGWarp(.43, .58, [6]float64{.91, .12, -.025, .004, .01, -.008}),
		testDNGWarpFisheye(.43, .58, [4]float64{.93, .08, -.02, .003}),
		testDNGWarpRectilinear2(false, testDNGRectilinear2Parameters{radial: [15]float64{.92, .12, -.025, .004}, tangential: [2]float64{.01, -.007}, minValidRadius: 0, maxValidRadius: 1, centerX: .43, centerY: .58}),
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, opcode := range opcodes {
			t.Run(order.String()+"/opcode-"+strconv.Itoa(int(opcode.id)), func(t *testing.T) {
				metadata := map[uint16]dngTestField{
					50718: dngTestDouble(order, 1, 1.5),
					51008: testDNGOpcodeList(opcode),
				}
				data := linearDNGFixture(order, width, height, 1, pixels, metadata)
				withoutWarp, err := decodeLinearDNG(linearDNGFixture(order, width, height, 1, pixels, nil))
				if err != nil {
					t.Fatal(err)
				}
				withWarp, err := decodeLinearDNG(data)
				if err != nil {
					t.Fatal(err)
				}
				if withWarp.Bounds() != image.Rect(0, 0, width, height+height/2) {
					t.Fatalf("bounds=%v want %dx%d", withWarp.Bounds(), width, height+height/2)
				}
				changed := false
				for y := 0; y < height && !changed; y++ {
					for x := 0; x < width; x++ {
						if withWarp.NRGBA64At(x, y) != withoutWarp.NRGBA64At(x, y) {
							changed = true
							break
						}
					}
				}
				if !changed {
					t.Fatal("Stage 1 warp had no effect on the decoded image")
				}
			})
		}
	}
}
