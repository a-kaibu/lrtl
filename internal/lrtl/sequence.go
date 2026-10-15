package lrtl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var imageExtensions = map[string]bool{
	".raw": true, ".rwl": true, ".gpr": true, ".x3f": true,
	".3fr": true, ".ari": true, ".arw": true, ".cr2": true, ".cr3": true,
	".crw": true, ".dcr": true, ".dcs": true, ".dng": true, ".erf": true,
	".fff": true, ".iiq": true, ".jpeg": true, ".jpg": true, ".kdc": true,
	".mef": true, ".mos": true, ".mrw": true, ".nef": true, ".nrw": true,
	".orf": true, ".pef": true, ".png": true, ".raf": true, ".rw2": true,
	".sr2": true, ".srf": true, ".srw": true, ".tif": true, ".tiff": true,
}

var naturalParts = regexp.MustCompile(`(\d+)`)

type Frame struct {
	Index   int
	Image   string
	Sidecar string
}

func (frame Frame) OutputSidecar() string {
	if frame.Sidecar != "" {
		return frame.Sidecar
	}
	return frame.Image + ".xmp"
}

func naturalLess(left, right string) bool {
	left = strings.ToLower(filepath.Base(left))
	right = strings.ToLower(filepath.Base(right))
	leftParts := naturalParts.Split(left, -1)
	rightParts := naturalParts.Split(right, -1)
	leftMatches := naturalParts.FindAllString(left, -1)
	rightMatches := naturalParts.FindAllString(right, -1)
	leftTokens := mergeNaturalTokens(leftParts, leftMatches)
	rightTokens := mergeNaturalTokens(rightParts, rightMatches)
	limit := len(leftTokens)
	if len(rightTokens) < limit {
		limit = len(rightTokens)
	}
	for index := 0; index < limit; index++ {
		leftToken, rightToken := leftTokens[index], rightTokens[index]
		if leftToken.number != rightToken.number {
			return leftToken.number
		}
		if leftToken.number {
			leftNumber := strings.TrimLeft(leftToken.value, "0")
			rightNumber := strings.TrimLeft(rightToken.value, "0")
			if leftNumber == "" {
				leftNumber = "0"
			}
			if rightNumber == "" {
				rightNumber = "0"
			}
			if len(leftNumber) != len(rightNumber) {
				return len(leftNumber) < len(rightNumber)
			}
			if leftNumber != rightNumber {
				return leftNumber < rightNumber
			}
		} else if leftToken.value != rightToken.value {
			return leftToken.value < rightToken.value
		}
	}
	if len(leftTokens) != len(rightTokens) {
		return len(leftTokens) < len(rightTokens)
	}
	return left < right
}

type naturalToken struct {
	value  string
	number bool
}

func mergeNaturalTokens(parts, numbers []string) []naturalToken {
	tokens := make([]naturalToken, 0, len(parts)+len(numbers))
	for index, part := range parts {
		tokens = append(tokens, naturalToken{value: part})
		if index < len(numbers) {
			tokens = append(tokens, naturalToken{value: numbers[index], number: true})
		}
	}
	return tokens
}

func sidecarCandidates(image string) []string {
	return []string{image + ".xmp", strings.TrimSuffix(image, filepath.Ext(image)) + ".xmp"}
}

func findSidecar(image string) (string, error) {
	var best string
	var bestModTime int64
	for index, candidate := range sidecarCandidates(image) {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		modTime := info.ModTime().UnixNano()
		if best == "" || modTime > bestModTime || (modTime == bestModTime && index == 0) {
			best = candidate
			bestModTime = modTime
		}
	}
	return best, nil
}

func DiscoverSequence(directory string) ([]Frame, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	images := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !imageExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		images = append(images, filepath.Join(directory, entry.Name()))
	}
	sort.SliceStable(images, func(i, j int) bool { return naturalLess(images[i], images[j]) })

	frames := make([]Frame, 0, len(images))
	for index, image := range images {
		sidecar, err := findSidecar(image)
		if err != nil {
			return nil, err
		}
		frames = append(frames, Frame{Index: index, Image: image, Sidecar: sidecar})
	}
	return frames, nil
}

func loadPacket(frame Frame) (*XMPPacket, error) {
	packet, found, err := loadFramePacket(frame)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return packet, nil
}

func loadFramePacket(frame Frame) (*XMPPacket, bool, error) {
	sidecar := frame.Sidecar
	if sidecar == "" {
		candidate := frame.OutputSidecar()
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			sidecar = candidate
		} else if err != nil && !os.IsNotExist(err) {
			return nil, false, err
		}
	}
	if sidecar != "" {
		data, err := os.ReadFile(sidecar)
		if err == nil {
			packet, parseErr := parseXMP(data)
			if parseErr != nil {
				return nil, false, fmt.Errorf("%s: %w", sidecar, parseErr)
			}
			return packet, true, nil
		}
		if !os.IsNotExist(err) {
			return nil, false, err
		}
	}
	data, found, err := readEmbeddedImageXMP(frame.Image)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	packet, err := parseXMP(data)
	if err != nil {
		return nil, false, err
	}
	return packet, true, nil
}
