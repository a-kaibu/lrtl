package lrtl

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var lrtFilenameTokens = regexp.MustCompile(`[0-9]+|[^0-9]+`)

func javaOrderKey(value string) string {
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, value)
}

func javaStringUnits(value string) int {
	n := 0
	for _, r := range value {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

type lrtFilenameToken struct {
	key     string
	numeric bool
	number  int64
	err     error
}

type lrtFilenameKey struct {
	tokens []lrtFilenameToken
	length int
}

func lrtFilenameKeyFor(name string) lrtFilenameKey {
	name = strings.NewReplacer("_", " ", "@", "!").Replace(name)
	key := lrtFilenameKey{length: javaStringUnits(name)}
	for _, text := range lrtFilenameTokens.FindAllString(name, -1) {
		token := lrtFilenameToken{key: javaOrderKey(text), numeric: true}
		var digits strings.Builder
		for _, r := range text {
			// The native digit predicate sees UTF-16 code units, so supplementary
			// decimal digits do not qualify even though Java can parse them elsewhere.
			if r > 0xffff || !unicode.IsDigit(r) {
				token.numeric = false
				break
			}
			for _, block := range unicode.Digit.R16 {
				if uint16(r) >= block.Lo && uint16(r) <= block.Hi && (uint16(r)-block.Lo)%block.Stride == 0 {
					digits.WriteByte(byte((uint16(r)-block.Lo)/block.Stride%10) + '0')
					break
				}
			}
		}
		if token.numeric {
			token.number, token.err = strconv.ParseInt(digits.String(), 10, 64)
		}
		key.tokens = append(key.tokens, token)
	}
	return key
}

func compareLRTFilenameKeys(left, right lrtFilenameKey) (int, error) {
	for i := 0; i < min(len(left.tokens), len(right.tokens)); i++ {
		a, b := left.tokens[i], right.tokens[i]
		order := 0
		if a.numeric && b.numeric {
			if a.err != nil || b.err != nil {
				return 0, fmt.Errorf("LRT filename numeric token exceeds signed 64-bit range")
			}
			order = cmp.Compare(a.number, b.number)
		} else {
			order = strings.Compare(a.key, b.key)
		}
		if order != 0 {
			return order, nil
		}
	}
	return cmp.Compare(left.length, right.length), nil
}

// orderSequenceFrames preserves each source Index and never changes the input
// slice. Call after filtering, before temporal processing or export manifests.
func orderSequenceFrames(frames []Frame, mode string) ([]Frame, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "name"
	}
	if mode != "name" && mode != "capture-time" && mode != "lrt-name" && mode != "lrt-ascii" && mode != "lrt-date" && mode != "lrt-settings" {
		return nil, fmt.Errorf("--sort must be name, capture-time, lrt-name, lrt-ascii, lrt-date or lrt-settings: %q", mode)
	}
	if mode == "lrt-settings" {
		var err error
		mode, err = readLRTSequenceSortMode(frames)
		if err != nil {
			return nil, err
		}
	}
	result := append([]Frame(nil), frames...)
	if mode == "name" {
		sort.SliceStable(result, func(i, j int) bool { return naturalLess(result[i].Image, result[j].Image) })
		return result, nil
	}
	if mode == "capture-time" {
		times, err := readFrameCaptureTimes(frames)
		if err != nil {
			return nil, err
		}
		indices := make([]int, len(frames))
		for i := range indices {
			indices[i] = i
		}
		sort.SliceStable(indices, func(i, j int) bool { return times[indices[i]].Before(times[indices[j]]) })
		for i, index := range indices {
			result[i] = frames[index]
		}
		return result, nil
	}
	for _, frame := range frames {
		if !utf8.ValidString(filepath.Base(frame.Image)) {
			return nil, fmt.Errorf("LRT sort requires UTF-8 filenames: %q", frame.Image)
		}
	}
	if mode == "lrt-date" {
		indices, err := readLRTReferenceDateOrder(frames)
		if err != nil {
			return nil, err
		}
		for i, index := range indices {
			result[i] = frames[index]
		}
		return result, nil
	}
	indices := make([]int, len(frames))
	for i := range indices {
		indices[i] = i
	}
	keys := make([]string, len(frames))
	natural := make([]lrtFilenameKey, len(frames))
	for i, frame := range frames {
		name := filepath.Base(frame.Image)
		if mode == "lrt-ascii" {
			keys[i] = javaOrderKey(name)
		} else {
			natural[i] = lrtFilenameKeyFor(name)
		}
	}
	var comparisonError error
	sort.SliceStable(indices, func(i, j int) bool {
		if comparisonError != nil {
			return false
		}
		if mode == "lrt-ascii" {
			return keys[indices[i]] < keys[indices[j]]
		}
		order, err := compareLRTFilenameKeys(natural[indices[i]], natural[indices[j]])
		if err != nil {
			comparisonError = err
			return false
		}
		return order < 0
	})
	if comparisonError != nil {
		return nil, comparisonError
	}
	for i, index := range indices {
		result[i] = frames[index]
	}
	return result, nil
}
