package lrtl

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxExternalDNGProfileBytes = 64 << 20
	maxExternalDNGProfileFiles = 100_000
)

type externalDNGCameraProfile struct {
	data    []byte
	entries map[uint16]tiffPreviewEntry
}

type externalDNGProfileLookup struct {
	profile *externalDNGCameraProfile
	err     error
}

var externalDNGProfileCache sync.Map

func cameraProfileDirectories() []string {
	if configured := strings.TrimSpace(os.Getenv("LRTL_CAMERA_PROFILE_DIR")); configured != "" {
		var directories []string
		for _, path := range filepath.SplitList(configured) {
			if path = strings.TrimSpace(path); path != "" {
				directories = append(directories, filepath.Clean(path))
			}
		}
		return uniqueProfileDirectories(directories)
	}

	var directories []string
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("APPDATA"), os.Getenv("ProgramData")} {
			if base != "" {
				directories = append(directories, filepath.Join(base, "Adobe", "CameraRaw", "CameraProfiles"))
			}
		}
	} else if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			directories = append(directories, filepath.Join(home, "Library", "Application Support", "Adobe", "CameraRaw", "CameraProfiles"))
		}
		directories = append(directories, "/Library/Application Support/Adobe/CameraRaw/CameraProfiles")
	}
	return uniqueProfileDirectories(directories)
}

func uniqueProfileDirectories(directories []string) []string {
	seen := make(map[string]bool, len(directories))
	unique := make([]string, 0, len(directories))
	for _, directory := range directories {
		key := filepath.Clean(directory)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if key == "." || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, directory)
	}
	return unique
}

func findExternalDNGCameraProfileCached(name, model string, directories []string) (*externalDNGCameraProfile, error) {
	configured := strings.TrimSpace(os.Getenv("LRTL_CAMERA_PROFILE_DIR")) != ""
	key := strings.Join(directories, "\x00") + "\x00" + model + "\x00" + name + fmt.Sprintf("\x00configured=%t", configured)
	if cached, found := externalDNGProfileCache.Load(key); found {
		result := cached.(externalDNGProfileLookup)
		return result.profile, result.err
	}
	// Adobe's system profile store contains thousands of files with a
	// camera/profile naming convention. Use that convention only there; user
	// and explicitly configured profile directories are scanned by metadata.
	profile, err := findExternalDNGCameraProfileWithDirectoryPolicy(name, model, directories, configured)
	result := externalDNGProfileLookup{profile: profile, err: err}
	actual, _ := externalDNGProfileCache.LoadOrStore(key, result)
	result = actual.(externalDNGProfileLookup)
	return result.profile, result.err
}

func findExternalDNGCameraProfileWithDirectoryPolicy(name, model string, directories []string, configured bool) (*externalDNGCameraProfile, error) {
	name = strings.TrimSpace(name)
	model = strings.TrimSpace(model)
	if name == "" || model == "" {
		return nil, nil
	}
	for _, directory := range directories {
		restrictPathNames := !configured && isSystemCameraProfileDirectory(directory)
		matches, err := findExternalDNGCameraProfilesInDirectory(name, model, directory, restrictPathNames)
		if err != nil {
			return nil, err
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("external DNG camera profile %q for %q is ambiguous", name, model)
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return nil, nil
}

func isSystemCameraProfileDirectory(directory string) bool {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			return false
		}
		relative, err := filepath.Rel(filepath.Clean(base), filepath.Clean(directory))
		return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	case "darwin":
		clean := filepath.Clean(directory)
		return clean == "/Library/Application Support/Adobe/CameraRaw/CameraProfiles" || strings.HasPrefix(clean, "/Library/Application Support/Adobe/CameraRaw/CameraProfiles"+string(filepath.Separator))
	default:
		return false
	}
}

func findExternalDNGCameraProfile(name, model string, directories []string) (*externalDNGCameraProfile, error) {
	return findExternalDNGCameraProfileWithPathFilter(name, model, directories, false)
}

func findExternalDNGCameraProfileWithPathFilter(name, model string, directories []string, restrictPathNames bool) (*externalDNGCameraProfile, error) {
	name = strings.TrimSpace(name)
	model = strings.TrimSpace(model)
	if name == "" || model == "" {
		return nil, nil
	}

	for _, directory := range directories {
		matches, err := findExternalDNGCameraProfilesInDirectory(name, model, directory, restrictPathNames)
		if err != nil {
			return nil, err
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("external DNG camera profile %q for %q is ambiguous", name, model)
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return nil, nil
}

func findExternalDNGCameraProfilesInDirectory(name, model, directory string, restrictPathNames bool) ([]*externalDNGCameraProfile, error) {
	var matches []*externalDNGCameraProfile
	filesSeen := 0
	var walkErr error
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Profile directories are optional. Skip unreadable or missing entries,
			// while still checking the rest of the configured search path.
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".dcp") {
			return nil
		}
		filesSeen++
		if filesSeen > maxExternalDNGProfileFiles {
			walkErr = fmt.Errorf("external DNG camera profile directory %q has too many files", directory)
			return fs.SkipAll
		}
		if restrictPathNames && !externalDNGProfilePathLooksSelected(path, name, model) {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil || !entryInfo.Mode().IsRegular() {
			return nil
		}

		file, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return nil
		}
		profileName, cameraModel, metadataErr := readExternalDNGCameraProfileMetadata(file, info.Size())
		file.Close()
		if metadataErr != nil {
			if externalDNGProfilePathLooksSelected(path, name, model) {
				walkErr = fmt.Errorf("read external DNG camera profile %q: %w", path, metadataErr)
				return fs.SkipAll
			}
			return nil
		}
		if profileName != name || !strings.EqualFold(strings.TrimSpace(cameraModel), model) {
			return nil
		}
		data, readErr := readExternalDNGCameraProfileFile(path)
		if readErr != nil {
			walkErr = fmt.Errorf("read external DNG camera profile %q: %w", path, readErr)
			return fs.SkipAll
		}
		scanner := &tiffPreviewScanner{data: data}
		parsedName, parsedModel, entries, parseErr := readDNGCameraProfile(scanner, 0)
		if parseErr != nil {
			walkErr = fmt.Errorf("read external DNG camera profile %q: %w", path, parseErr)
			return fs.SkipAll
		}
		if parsedName != name || !strings.EqualFold(strings.TrimSpace(parsedModel), model) {
			return nil
		}
		matches = append(matches, &externalDNGCameraProfile{data: data, entries: entries})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return matches, nil
}

func readExternalDNGCameraProfileFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < 10 || info.Size() > maxExternalDNGProfileBytes {
		return nil, fmt.Errorf("invalid profile file size")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxExternalDNGProfileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxExternalDNGProfileBytes {
		return nil, fmt.Errorf("profile file exceeds %d bytes", maxExternalDNGProfileBytes)
	}
	return data, nil
}

func externalDNGProfilePathLooksSelected(path, name, model string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(path, strings.ToLower(name)) && strings.Contains(path, strings.ToLower(model))
}

func readExternalDNGCameraProfileMetadata(file *os.File, size int64) (string, string, error) {
	if size < 10 || size > maxExternalDNGProfileBytes {
		return "", "", fmt.Errorf("invalid profile file size")
	}
	var header [8]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return "", "", err
	}
	var order binary.ByteOrder
	switch string(header[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return "", "", fmt.Errorf("invalid byte-order mark")
	}
	if order.Uint16(header[2:4]) != 0x4352 {
		return "", "", fmt.Errorf("invalid DNG camera profile magic")
	}
	ifdOffset := uint64(order.Uint32(header[4:8]))
	if ifdOffset < 8 || !tiffRange(ifdOffset, 2, uint64(size)) {
		return "", "", fmt.Errorf("invalid profile IFD offset")
	}
	var countBytes [2]byte
	if _, err := file.ReadAt(countBytes[:], int64(ifdOffset)); err != nil {
		return "", "", err
	}
	count := uint64(order.Uint16(countBytes[:]))
	if count > maxPreviewIFDEntries || !tiffRange(ifdOffset+2, count*12+4, uint64(size)) {
		return "", "", fmt.Errorf("invalid profile IFD entries")
	}
	entries := make([]byte, count*12)
	if len(entries) > 0 {
		if _, err := file.ReadAt(entries, int64(ifdOffset+2)); err != nil {
			return "", "", err
		}
	}
	var name, model string
	for index := uint64(0); index < count; index++ {
		encoded := entries[index*12 : index*12+12]
		tag := order.Uint16(encoded[:2])
		if tag != 50936 && tag != 50708 {
			continue
		}
		value, err := readExternalDNGProfileString(file, size, order, encoded)
		if err != nil {
			return "", "", fmt.Errorf("invalid profile string tag %d: %w", tag, err)
		}
		if tag == 50936 {
			name = value
		} else {
			model = value
		}
	}
	return name, model, nil
}

func readExternalDNGProfileString(file *os.File, size int64, order binary.ByteOrder, entry []byte) (string, error) {
	typeID := order.Uint16(entry[2:4])
	count := uint64(order.Uint32(entry[4:8]))
	if (typeID != 1 && typeID != 2) || count < 1 || count > 1<<20 || count > uint64(size) {
		return "", fmt.Errorf("unsupported string type or size")
	}
	data := make([]byte, count)
	if count <= 4 {
		copy(data, entry[8:8+count])
	} else {
		offset := uint64(order.Uint32(entry[8:12]))
		if !tiffRange(offset, count, uint64(size)) || offset > math.MaxInt64 {
			return "", fmt.Errorf("string data is outside the profile")
		}
		if _, err := file.ReadAt(data, int64(offset)); err != nil {
			return "", err
		}
	}
	if data[len(data)-1] != 0 || strings.IndexByte(string(data[:len(data)-1]), 0) >= 0 || !utf8.Valid(data[:len(data)-1]) {
		return "", fmt.Errorf("string is not valid NUL-terminated UTF-8")
	}
	return string(data[:len(data)-1]), nil
}
