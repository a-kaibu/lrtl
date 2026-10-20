package lrtl

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

type frameRate struct{ numerator, denominator uint32 }

var frameRateSyntax = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+|/[0-9]+)?$`)

func parseFrameRate(text string) (frameRate, error) {
	text = strings.TrimSpace(text)
	if len(text) > 64 || !frameRateSyntax.MatchString(text) {
		return frameRate{}, fmt.Errorf("fps must be a number or fraction between 1 and 240: %q", text)
	}
	numerator, denominator := new(big.Int), big.NewInt(1)
	if parts := strings.SplitN(text, "/", 2); len(parts) == 2 {
		numerator.SetString(parts[0], 10)
		denominator.SetString(parts[1], 10)
	} else if parts := strings.SplitN(text, ".", 2); len(parts) == 2 {
		numerator.SetString(parts[0]+parts[1], 10)
		denominator.Exp(big.NewInt(10), big.NewInt(int64(len(parts[1]))), nil)
	} else {
		numerator.SetString(text, 10)
	}
	if denominator.Sign() == 0 {
		return frameRate{}, fmt.Errorf("fps denominator must be positive")
	}
	ratio := new(big.Rat).SetFrac(numerator, denominator)
	// Decimal NTSC labels denote /1001 rates. Explicit fractions stay exact.
	if !strings.Contains(text, "/") {
		switch ratio.RatString() {
		case "2997/125":
			ratio.SetFrac64(24000, 1001)
		case "2997/100":
			ratio.SetFrac64(30000, 1001)
		case "5994/125":
			ratio.SetFrac64(48000, 1001)
		case "2997/50":
			ratio.SetFrac64(60000, 1001)
		}
	}
	if ratio.Cmp(big.NewRat(1, 1)) < 0 || ratio.Cmp(big.NewRat(240, 1)) > 0 || ratio.Num().BitLen() > 32 || ratio.Denom().BitLen() > 32 {
		return frameRate{}, fmt.Errorf("fps must be between 1 and 240 with a 32-bit numerator and denominator: %q", text)
	}
	return frameRate{uint32(ratio.Num().Uint64()), uint32(ratio.Denom().Uint64())}, nil
}

func (rate frameRate) String() string {
	if rate.denominator == 1 {
		return strconv.FormatUint(uint64(rate.numerator), 10)
	}
	return fmt.Sprintf("%d/%d", rate.numerator, rate.denominator)
}

func (options RenderOptions) frameRate() (frameRate, error) {
	if options.FrameRate != "" {
		if options.NativeFPS {
			return parseNativeFrameRate(options.FrameRate)
		}
		return parseFrameRate(options.FrameRate)
	}
	return parseFrameRate(strconv.Itoa(options.FPS))
}

// Native stored fps labels are parsed as Java floating-point literals. Keep
// this separate from the CLI's exact decimal/fraction interface.
func nativeFrameRateLabel(text string) string {
	text = strings.TrimFunc(text, func(r rune) bool { return r <= 32 })
	if len(text) > 0 && !strings.Contains(text, "/") {
		switch text[len(text)-1] {
		case 'f', 'F', 'd', 'D':
			text = text[:len(text)-1]
		}
	}
	return text
}

func parseNativeFrameRate(text string) (frameRate, error) {
	if len(text) > 64 {
		return frameRate{}, fmt.Errorf("native fps label exceeds 64 characters")
	}
	label := nativeFrameRateLabel(text)
	if strings.Contains(label, "_") {
		return frameRate{}, fmt.Errorf("invalid native fps label: %q", text)
	}
	// Fraction labels are an lrtl extension. The native rate helper passes
	// them through, although its other Float.parseFloat consumers reject them.
	if strings.Contains(label, "/") {
		return parseFrameRate(label)
	}
	value, err := strconv.ParseFloat(label, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 241 {
		return frameRate{}, fmt.Errorf("invalid native fps label: %q", text)
	}
	nearest := math.Floor(value + .5)
	translated := strconv.FormatFloat(nearest, 'f', 0, 64)
	if math.Abs(value-nearest) >= .001 {
		translated = strconv.FormatFloat(nearest*1000, 'f', 0, 64) + "/1001"
	}
	return parseFrameRate(translated)
}

func (options RenderOptions) frameRateLabel() string {
	if options.FrameRate != "" {
		if options.NativeFPS {
			return nativeFrameRateLabel(options.FrameRate)
		}
		return options.FrameRate
	}
	return strconv.Itoa(options.FPS)
}

func (rate frameRate) seconds(frames int) float64 {
	return float64(frames) * float64(rate.denominator) / float64(rate.numerator)
}

func (rate frameRate) offsetMilliseconds(frame int64) (int64, error) {
	if frame < 0 || rate.numerator == 0 || rate.denominator == 0 {
		return 0, fmt.Errorf("invalid frame offset or frame rate")
	}
	value := new(big.Int).SetInt64(frame)
	value.Mul(value, new(big.Int).SetUint64(uint64(rate.denominator)*1000))
	value.Add(value, new(big.Int).SetUint64(uint64(rate.numerator)/2))
	value.Quo(value, new(big.Int).SetUint64(uint64(rate.numerator)))
	if !value.IsInt64() {
		return 0, fmt.Errorf("subtitle frame offset exceeds supported duration")
	}
	return value.Int64(), nil
}
