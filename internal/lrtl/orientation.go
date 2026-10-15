package lrtl

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	"github.com/evanoberholster/imagemeta"
)

func readEXIFOrientation(data []byte) (uint16, error) {
	metadata, err := imagemeta.Decode(bytes.NewReader(data))
	if errors.Is(err, imagemeta.ErrNoExif) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read EXIF orientation: %w", err)
	}
	orientation := uint16(metadata.Orientation)
	if orientation == 0 {
		return 1, nil
	}
	if orientation > 8 {
		return 0, fmt.Errorf("unsupported EXIF orientation value %d", orientation)
	}
	return orientation, nil
}

func orientedDimensions(width, height int, orientation uint16) (int, int) {
	if orientation >= 5 && orientation <= 8 {
		return height, width
	}
	return width, height
}

func applyEXIFOrientation(source image.Image, orientation uint16) image.Image {
	if orientation <= 1 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	outputWidth, outputHeight := orientedDimensions(width, height, orientation)
	result := image.NewNRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var destinationX, destinationY int
			switch orientation {
			case 2: // Mirror horizontal.
				destinationX, destinationY = width-1-x, y
			case 3: // Rotate 180 degrees.
				destinationX, destinationY = width-1-x, height-1-y
			case 4: // Mirror vertical.
				destinationX, destinationY = x, height-1-y
			case 5: // Transpose across the top-left to bottom-right diagonal.
				destinationX, destinationY = y, x
			case 6: // Rotate 90 degrees clockwise.
				destinationX, destinationY = height-1-y, x
			case 7: // Transverse across the top-right to bottom-left diagonal.
				destinationX, destinationY = height-1-y, width-1-x
			case 8: // Rotate 270 degrees clockwise.
				destinationX, destinationY = y, width-1-x
			}
			result.Set(destinationX, destinationY, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return result
}
