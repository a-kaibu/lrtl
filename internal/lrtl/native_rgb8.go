package lrtl

import (
	"fmt"
	"image"
)

func nativeImageWorkingFrame(img image.Image, format string) ([]byte, error) {
	if source, ok := img.(*image.YCbCr); ok && format == "jpeg" {
		return nativeJPEGWorkingFrame(source)
	}
	if source, ok := img.(*image.Gray); ok && (format == "jpeg" || format == "png" || format == "tiff") {
		return nativeGray8WorkingFrame(source)
	}
	if source, ok := img.(*image.Gray16); ok && (format == "png" || format == "tiff") {
		return nativeGray16WorkingFrame(source)
	}
	rgb, err := imageRGB48(img)
	if err != nil {
		return nil, err
	}
	var planes [3][]uint16
	rgb8 := false
	// JPEG and grayscale/palette codecs have their own native input paths.
	// Only truecolor, opaque PNG/TIFF samples select the RGB24 quantizer.
	if format == "png" || format == "tiff" {
		switch source := img.(type) {
		case *image.RGBA:
			rgb8 = source.Opaque()
		case *image.NRGBA:
			rgb8 = source.Opaque()
		}
	}
	if rgb8 {
		packed := make([]byte, len(rgb)/2)
		for i := range packed {
			packed[i] = rgb[i*2+1]
		}
		planes, err = nativeRGB8Planes(packed, img.Bounds().Dx(), img.Bounds().Dy())
	} else {
		planes, err = nativeSDRPlanes(rgb, img.Bounds().Dx(), img.Bounds().Dy())
	}
	if err != nil {
		return nil, err
	}
	outputFrameColor(true).apply(rgb, planes, planes)
	return rgb, nil
}

// RGB24 uses a 14-bit intermediate before horizontal scaling to 15 bits.
// Expanding channels to RGB48 first changes this quantization boundary.
func nativeRGB8Planes(rgb []byte, width, height int) ([3][]uint16, error) {
	var planes [3][]uint16
	if width <= 0 || height <= 0 || len(rgb)%3 != 0 || len(rgb)/3/width != height || len(rgb)/3%width != 0 {
		return planes, fmt.Errorf("invalid native RGB24 dimensions")
	}
	matrix := [3][3]int64{{8414, 16519, 3208}, {-4865, -9528, 14392}, {14392, -12061, -2332}}
	for p := range planes {
		planes[p] = make([]uint16, len(rgb)/3)
		upper, bias := int64(940), int64(32<<14)+256
		if p > 0 {
			upper = 960
			bias = int64(256<<14) + 256
		}
		coefficient := (1023*16384 + upper - 65) / (upper - 64)
		offset := (int64(1023) << 19) - (upper<<5)*coefficient + 8192
		for i := range planes[p] {
			value := bias
			for c, weight := range matrix[p] {
				value += weight * int64(rgb[i*3+c])
			}
			value = ((value>>9)*2*coefficient + offset) >> 14
			planes[p][i] = uint16(min(1023, max(0, (min(32767, value)+16)>>5)))
		}
	}
	return planes, nil
}
