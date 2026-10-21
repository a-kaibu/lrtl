package lrtl

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

type nativeSubtitleOptions struct{ every int }

func nativeTimestampFPS(label string) float32 {
	if len(label) > 4 {
		label = label[:4]
	}
	switch label {
	case "23.9":
		return 23.976025
	case "29.9":
		return 29.97003
	case "47.9":
		return 47.95205
	case "59.9":
		return 59.94006
	}
	v, err := strconv.ParseFloat(label, 32)
	if err != nil {
		return 25
	}
	return float32(v)
}

type nativeTimestampClock struct{ step *big.Rat }

func newNativeTimestampClock(label string, speed float64) (nativeTimestampClock, error) {
	fps := nativeTimestampFPS(strings.TrimSpace(label))
	if fps <= 0 || math.IsNaN(float64(fps)) || math.IsInf(float64(fps), 0) {
		return nativeTimestampClock{}, fmt.Errorf("native timestamp fps must be positive and finite")
	}
	if _, err := validatePlaybackSpeed(speed); err != nil {
		return nativeTimestampClock{}, err
	}
	seconds := (1 / float64(fps)) / float64(float32(speed))
	step, ok := new(big.Rat).SetString(strconv.FormatFloat(seconds, 'g', -1, 64))
	if !ok {
		return nativeTimestampClock{}, fmt.Errorf("invalid native timestamp clock")
	}
	return nativeTimestampClock{step}, nil
}

func (clock nativeTimestampClock) timecode(frame int) (string, error) {
	if frame < 0 || int64(frame) > math.MaxInt32 {
		return "", fmt.Errorf("native timestamp frame exceeds signed 32-bit range")
	}
	if frame == 0 {
		return "00:00:00,000", nil
	}
	value := new(big.Rat).Mul(clock.step, new(big.Rat).SetInt64(int64(frame)))
	// Truncate the decimal value to six significant digits before splitting
	// hours/minutes/seconds. The native clock loses precision on long clips.
	exponent := len(value.Num().String()) - len(value.Denom().String())
	power := func(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
	threshold := new(big.Rat)
	if exponent >= 0 {
		threshold.SetInt(power(exponent))
	} else {
		threshold.SetFrac(big.NewInt(1), power(-exponent))
	}
	if value.Cmp(threshold) < 0 {
		exponent--
	}
	places := 5 - exponent
	if places >= 0 {
		value.Mul(value, new(big.Rat).SetInt(power(places)))
	} else {
		value.Quo(value, new(big.Rat).SetInt(power(-places)))
	}
	integer := new(big.Int).Quo(value.Num(), value.Denom())
	milliseconds := new(big.Int).Set(integer)
	if places <= 3 {
		milliseconds.Mul(milliseconds, power(3-places))
	} else {
		milliseconds.Quo(milliseconds, power(places-3))
	}
	if !milliseconds.IsInt64() {
		return "", fmt.Errorf("native subtitle duration exceeds supported range")
	}
	ms := milliseconds.Int64()
	seconds := ms / 1000
	// Native seconds extraction uses Java intValue(), including its wrap at
	// very large offsets. Hours and minutes are divided before that cast.
	return fmt.Sprintf("%02d:%02d:%02d,%03d", int32(seconds/3600), int32(seconds/60)%60, int32(seconds)%60, ms%1000), nil
}

func nativeTimestampCueFrames(count int, label string, speed float64, every int) ([]int, error) {
	if count <= 0 || int64(count) > math.MaxInt32 {
		return nil, fmt.Errorf("native subtitles require a nonempty 32-bit sequence")
	}
	if every == 0 || every < -1 {
		return nil, fmt.Errorf("native subtitle interval must be -1 or a positive frame count")
	}
	if _, err := newNativeTimestampClock(label, speed); err != nil {
		return nil, err
	}
	interval := float32(every)
	if every < 0 {
		interval = nativeTimestampFPS(label)
	}
	indices := make([]int, 0)
	for frame := 0; frame < count; frame++ {
		remainder := float32(math.Mod(float64(float32(frame)), float64(interval)))
		if int32(float32(remainder*float32(speed))) == 0 {
			indices = append(indices, frame)
		}
	}
	return append(indices, count), nil
}
