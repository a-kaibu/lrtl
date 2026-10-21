package lrtl

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
)

// Numerical reference for the isolated native fade kernel. Production runs
// fade after final encoder color conversion in FFmpeg.
// Its time options are parsed at microsecond precision and rescaled to frames.
func nativeFadeFactor(fade videoFade, index int, rate frameRate) int64 {
	if fade.duration <= 0 {
		return 65535
	}
	rescale := func(text string) (int64, int64) {
		whole, fraction, _ := strings.Cut(text, ".")
		seconds, _ := strconv.ParseInt(whole, 10, 64)
		// Native duration parsing discards decimal digits after six places;
		// rounding here can move the ramp by an entire frame at half ties.
		micros, _ := strconv.ParseInt((fraction + "000000")[:6], 10, 64)
		micros += seconds * 1e6
		return int64(math.Round(float64(micros) * float64(rate.numerator) / (1e6 * float64(rate.denominator)))), micros
	}
	// The native command prints duration as a Java float, rather than its
	// expanded float64 value.
	text := strconv.FormatFloat(fade.duration, 'f', -1, 32)
	start, _ := rescale(strconv.FormatFloat(fade.start, 'f', 3, 64))
	length, micros := rescale(text)
	if int64(index) <= start {
		return 65535
	}
	if micros == 0 {
		// A positive option truncated to zero microseconds uses the native
		// default frame-based ramp: 25 frames and a prequantized step.
		if int64(index)-start > 25 {
			return 0
		}
		return 65535 - (int64(index)-start)*(65536/25)
	}
	if length == 0 {
		return 0
	}
	return max(0, 65535-min(65535, (int64(index)-start)*65535/length))
}

func fadeNativePlanes(data []byte, depth int, factor int64) {
	if factor >= 65535 {
		return
	}
	stride := 1
	if depth == 10 {
		stride = 2
	}
	planeBytes := len(data) / 3
	for p := 0; p < 3; p++ {
		// Native yuv444p/10le uses the format's studio black even when the
		// frame is tagged full-range. Chroma converges to the neutral code.
		black := int64(16 << (depth - 8))
		if p > 0 {
			black = 1 << (depth - 1)
		}
		for i := p * planeBytes; i < (p+1)*planeBytes; i += stride {
			value := int64(data[i])
			if stride == 2 {
				value = int64(binary.LittleEndian.Uint16(data[i:]))
			}
			bias := (black << 16) + 32768
			if depth == 8 && p > 0 {
				// The measured 8-bit chroma constant is nine below the
				// nominal half-up bias; preserve its tie behavior.
				bias -= 9
			}
			value = ((value-black)*factor + bias) >> 16
			if stride == 2 {
				binary.LittleEndian.PutUint16(data[i:], uint16(value))
			} else {
				data[i] = byte(value)
			}
		}
	}
}
