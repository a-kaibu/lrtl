package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

type videoFade struct{ start, duration float64 }

func displayedVideoDuration(frames int, rate frameRate, label string) float32 {
	fps, err := strconv.ParseFloat(strings.TrimSpace(label), 32)
	if err != nil || fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		fps = float64(rate.numerator) / float64(rate.denominator)
	}
	return float32(frames) / float32(fps)
}

func planVideoFade(frames int, rate frameRate, label string, seconds float64) (videoFade, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return videoFade{}, fmt.Errorf("fade-out seconds must be a finite nonnegative number")
	}
	if seconds == 0 {
		return videoFade{}, nil
	}
	// Native duration uses Float frame count / Float displayed FPS; rational
	// rate strings are a lrtl extension and use the exact ratio as the input.
	total := displayedVideoDuration(frames, rate, label)
	duration := total
	if seconds < float64(total) {
		duration = float32(seconds)
	}
	if duration <= 0 {
		return videoFade{}, fmt.Errorf("fade-out duration is too small")
	}
	start := float32(total - duration)
	text := strconv.FormatFloat(float64(start), 'f', 3, 64)
	rounded, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return videoFade{}, err
	}
	return videoFade{start: rounded, duration: float64(duration)}, nil
}

func (fade videoFade) gain(index int, rate frameRate) float64 {
	if fade.duration == 0 {
		return 1
	}
	time := float64(index) * float64(rate.denominator) / float64(rate.numerator)
	return math.Max(0, math.Min(1, 1-(time-fade.start)/fade.duration))
}

func fadeRGB48(data []byte, gain float64) {
	if gain >= 1 {
		return
	}
	for i := 0; i+1 < len(data); i += 2 {
		value := binary.LittleEndian.Uint16(data[i:])
		binary.LittleEndian.PutUint16(data[i:], uint16(math.Round(float64(value)*gain)))
	}
}

func fadeVideoImage(img image.Image, gain float64) image.Image {
	if gain >= 1 {
		return img
	}
	out := image.NewNRGBA64(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			p.R = uint16(math.Round(float64(p.R) * gain))
			p.G = uint16(math.Round(float64(p.G) * gain))
			p.B = uint16(math.Round(float64(p.B) * gain))
			out.SetNRGBA64(x, y, p)
		}
	}
	return out
}
