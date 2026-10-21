package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
)

func nativeGray8WorkingFrame(img *image.Gray) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || uint64(w)*uint64(h) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native grayscale dimensions")
	}
	out := make([]byte, w*h*6)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16(img.GrayAt(b.Min.X+x, b.Min.Y+y).Y)
			pixel := out[(y*w+x)*6:]
			binary.LittleEndian.PutUint16(pixel, v<<2|v>>6)
			binary.LittleEndian.PutUint16(pixel[2:], 512)
			binary.LittleEndian.PutUint16(pixel[4:], 512)
		}
	}
	return out, nil
}

func nativeGray16WorkingFrame(img *image.Gray16) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || uint64(w)*uint64(h) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native grayscale dimensions")
	}
	out := make([]byte, w*h*6)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := int(img.Gray16At(b.Min.X+x, b.Min.Y+y).Y)
			// The measured 8x8 ordered pattern is a permutation of the six coordinate
			// bits. Full-range reduction subtracts the high six bits before dithering.
			d := 18 ^ ((x & 1) * 48) ^ (((x >> 1) & 1) * 12) ^ (((x >> 2) & 1) * 3) ^ ((y & 1) * 32) ^ (((y >> 1) & 1) * 8) ^ (((y >> 2) & 1) * 2)
			sample := uint16((v - (v >> 10) + d) >> 6)
			pixel := out[(y*w+x)*6:]
			binary.LittleEndian.PutUint16(pixel, sample)
			binary.LittleEndian.PutUint16(pixel[2:], 512)
			binary.LittleEndian.PutUint16(pixel[4:], 512)
		}
	}
	return out, nil
}

func nativeScaleGray16WorkingFrame(img *image.Gray16, targetW, targetH int, region image.Rectangle, alignH, alignV int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || uint64(w)*uint64(h) > 64*1024*1024 || targetW <= 0 || targetH <= 0 || region.Empty() || !region.In(image.Rect(0, 0, targetW, targetH)) || uint64(region.Dx())*uint64(region.Dy()) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native grayscale scaling dimensions")
	}
	hx, vy := nativeScaleWeights(w, targetW, 16384, alignH), nativeScaleWeights(h, targetH, 4096, alignV)
	out := make([]byte, region.Dx()*region.Dy()*6)
	rows := map[int][]int64{}
	var reusable [][]int64
	for y := region.Min.Y; y < region.Max.Y; y++ {
		tap := vy[y]
		for key, row := range rows {
			if key < tap.position || key >= tap.position+len(tap.weights) {
				reusable = append(reusable, row)
				delete(rows, key)
			}
		}
		for j := range tap.weights {
			index := tap.position + j
			if _, ok := rows[index]; ok {
				continue
			}
			var row []int64
			if n := len(reusable); n > 0 {
				row = reusable[n-1]
				reusable = reusable[:n-1]
			} else {
				row = make([]int64, region.Dx())
			}
			for x := region.Min.X; x < region.Max.X; x++ {
				sum := int64(0)
				for k, weight := range hx[x].weights {
					sum += int64(img.Gray16At(b.Min.X+hx[x].position+k, b.Min.Y+index).Y) * weight
				}
				row[x-region.Min.X] = min(32767, sum>>15)
			}
			rows[index] = row
		}
		for x := 0; x < region.Dx(); x++ {
			sum := int64(1 << 16)
			for j, weight := range tap.weights {
				sum += rows[tap.position+j][x] * weight
			}
			pixel := out[((y-region.Min.Y)*region.Dx()+x)*6:]
			binary.LittleEndian.PutUint16(pixel, uint16(max(0, min(1023, sum>>17))))
			binary.LittleEndian.PutUint16(pixel[2:], 512)
			binary.LittleEndian.PutUint16(pixel[4:], 512)
		}
	}
	return out, nil
}
