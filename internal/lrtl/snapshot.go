package lrtl

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const xmpSnapshotFormat = "lrtl-xmp-snapshot"

type xmpSnapshotManifest struct {
	Format    string             `json:"format"`
	Version   int                `json:"version"`
	Sequence  string             `json:"sequence"`
	CreatedAt time.Time          `json:"createdAt"`
	Sidecars  []xmpSnapshotEntry `json:"sidecars"`
}

type xmpSnapshotEntry struct {
	Image   string `json:"image"`
	Sidecar string `json:"sidecar"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type loadedXMPSnapshot struct {
	manifest xmpSnapshotManifest
	data     map[string][]byte
}

type listedXMPSnapshot struct {
	path     string
	manifest xmpSnapshotManifest
}

func snapshotDirectory(sequenceDirectory string) string {
	return filepath.Join(sequenceDirectory, ".lrtl", "snapshots")
}

func ensureSnapshotDirectory(sequenceDirectory string) (string, error) {
	metadataDirectory := filepath.Join(sequenceDirectory, ".lrtl")
	directory := snapshotDirectory(sequenceDirectory)
	for _, path := range []string{metadataDirectory, directory} {
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("snapshot directory is not a real directory: %s", path)
		}
	}
	return directory, nil
}

func validSnapshotName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return false
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func snapshotName(name string) (string, error) {
	if name == "" {
		name = "snapshot-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	if strings.EqualFold(filepath.Ext(name), ".zip") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if !validSnapshotName(name) {
		return "", fmt.Errorf("snapshot name may contain only letters, numbers, dot, dash, and underscore")
	}
	return name, nil
}

func createXMPSnapshot(sequenceDirectory, name string) (string, int, error) {
	info, err := os.Stat(sequenceDirectory)
	if err != nil {
		return "", 0, err
	}
	if !info.IsDir() {
		return "", 0, fmt.Errorf("not a directory: %s", sequenceDirectory)
	}
	name, err = snapshotName(name)
	if err != nil {
		return "", 0, err
	}
	absoluteDirectory, err := filepath.Abs(sequenceDirectory)
	if err != nil {
		return "", 0, err
	}
	frames, err := DiscoverSequence(sequenceDirectory)
	if err != nil {
		return "", 0, err
	}
	manifest := xmpSnapshotManifest{
		Format:    xmpSnapshotFormat,
		Version:   1,
		Sequence:  absoluteDirectory,
		CreatedAt: time.Now().UTC(),
		Sidecars:  make([]xmpSnapshotEntry, 0, len(frames)),
	}
	contents := make(map[string][]byte)
	seen := make(map[string]bool)
	var totalSize int64
	for _, frame := range frames {
		if frame.Sidecar == "" {
			continue
		}
		sidecarInfo, err := os.Lstat(frame.Sidecar)
		if err != nil {
			return "", 0, err
		}
		if sidecarInfo.Mode()&os.ModeSymlink != 0 || !sidecarInfo.Mode().IsRegular() {
			return "", 0, fmt.Errorf("sidecar is not a regular file: %s", frame.Sidecar)
		}
		if sidecarInfo.Size() > 128*1024*1024 {
			return "", 0, fmt.Errorf("XMP sidecar exceeds the 128 MiB snapshot limit: %s", frame.Sidecar)
		}
		sidecarName := filepath.Base(frame.Sidecar)
		if !strings.EqualFold(filepath.Ext(sidecarName), ".xmp") {
			return "", 0, fmt.Errorf("unexpected sidecar extension: %s", sidecarName)
		}
		key := strings.ToLower(sidecarName)
		if seen[key] {
			continue
		}
		data, err := os.ReadFile(frame.Sidecar)
		if err != nil {
			return "", 0, err
		}
		if len(data) > 128*1024*1024 {
			return "", 0, fmt.Errorf("XMP sidecar exceeds the 128 MiB snapshot limit: %s", frame.Sidecar)
		}
		totalSize += int64(len(data))
		if totalSize > 512*1024*1024 {
			return "", 0, fmt.Errorf("sequence XMP sidecars exceed the 512 MiB snapshot limit")
		}
		digest := sha256.Sum256(data)
		manifest.Sidecars = append(manifest.Sidecars, xmpSnapshotEntry{
			Image:   filepath.Base(frame.Image),
			Sidecar: sidecarName,
			Size:    int64(len(data)),
			SHA256:  hex.EncodeToString(digest[:]),
		})
		contents[sidecarName] = data
		seen[key] = true
	}
	if len(manifest.Sidecars) == 0 {
		return "", 0, fmt.Errorf("no XMP sidecars found in sequence: %s", sequenceDirectory)
	}
	sort.Slice(manifest.Sidecars, func(i, j int) bool {
		return naturalLess(manifest.Sidecars[i].Image, manifest.Sidecars[j].Image)
	})

	outputDirectory, err := ensureSnapshotDirectory(sequenceDirectory)
	if err != nil {
		return "", 0, err
	}
	outputPath := filepath.Join(outputDirectory, name+".zip")
	if _, err := os.Lstat(outputPath); err == nil {
		return "", 0, fmt.Errorf("snapshot already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return "", 0, err
	}
	temporary, err := os.CreateTemp(outputDirectory, ".snapshot-*.tmp")
	if err != nil {
		return "", 0, err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	zipWriter := zip.NewWriter(temporary)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		var entry io.Writer
		entry, err = zipWriter.Create("manifest.json")
		if err == nil {
			_, err = entry.Write(manifestData)
		}
	}
	for _, sidecar := range manifest.Sidecars {
		if err != nil {
			break
		}
		var entry io.Writer
		entry, err = zipWriter.Create(filepath.ToSlash(filepath.Join("sidecars", sidecar.Sidecar)))
		if err == nil {
			_, err = entry.Write(contents[sidecar.Sidecar])
		}
	}
	closeZipErr := zipWriter.Close()
	if err == nil {
		err = closeZipErr
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeFileErr := temporary.Close()
	if err == nil {
		err = closeFileErr
	}
	if err != nil {
		return "", 0, err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return "", 0, err
	}
	committed = true
	return outputPath, len(manifest.Sidecars), nil
}

func validateSnapshotEntry(entry xmpSnapshotEntry) error {
	if entry.Image == "" || filepath.Base(entry.Image) != entry.Image || strings.ContainsAny(entry.Image, `/\:`) {
		return fmt.Errorf("invalid image path in snapshot: %q", entry.Image)
	}
	if !imageExtensions[strings.ToLower(filepath.Ext(entry.Image))] {
		return fmt.Errorf("invalid image extension in snapshot: %q", entry.Image)
	}
	if entry.Sidecar == "" || filepath.Base(entry.Sidecar) != entry.Sidecar || strings.ContainsAny(entry.Sidecar, `/\:`) || !strings.EqualFold(filepath.Ext(entry.Sidecar), ".xmp") {
		return fmt.Errorf("invalid sidecar path in snapshot: %q", entry.Sidecar)
	}
	if entry.Size < 0 || entry.Size > 128*1024*1024 {
		return fmt.Errorf("invalid sidecar size in snapshot for %s", entry.Sidecar)
	}
	if len(entry.SHA256) != sha256.Size*2 {
		return fmt.Errorf("invalid sidecar hash in snapshot for %s", entry.Sidecar)
	}
	if _, err := hex.DecodeString(entry.SHA256); err != nil {
		return fmt.Errorf("invalid sidecar hash in snapshot for %s", entry.Sidecar)
	}
	return nil
}

func sameSequencePath(expected, actual string) bool {
	expected = filepath.Clean(expected)
	actual = filepath.Clean(actual)
	if filepath.Separator == '\\' {
		return strings.EqualFold(expected, actual)
	}
	return expected == actual
}

func readXMPSnapshot(path string) (*loadedXMPSnapshot, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	var manifestData []byte
	manifestFiles := 0
	for _, file := range archive.File {
		if file.Name != "manifest.json" {
			continue
		}
		manifestFiles++
		if file.UncompressedSize64 > 8*1024*1024 {
			return nil, fmt.Errorf("snapshot manifest is too large")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		manifestData, err = io.ReadAll(io.LimitReader(reader, 8*1024*1024+1))
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(manifestData) > 8*1024*1024 {
			return nil, fmt.Errorf("snapshot manifest is too large")
		}
	}
	if manifestFiles != 1 {
		return nil, fmt.Errorf("snapshot must contain exactly one manifest.json")
	}
	var manifest xmpSnapshotManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("invalid snapshot manifest: %w", err)
	}
	if manifest.Format != xmpSnapshotFormat || manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported XMP snapshot format or version")
	}
	if !filepath.IsAbs(manifest.Sequence) || manifest.CreatedAt.IsZero() {
		return nil, fmt.Errorf("snapshot sequence path or creation time is invalid")
	}
	if len(manifest.Sidecars) == 0 || len(manifest.Sidecars) > 100000 {
		return nil, fmt.Errorf("snapshot sidecar count is invalid")
	}
	archiveFiles := make(map[string][]*zip.File)
	for _, file := range archive.File {
		archiveFiles[file.Name] = append(archiveFiles[file.Name], file)
	}
	loaded := &loadedXMPSnapshot{manifest: manifest, data: make(map[string][]byte, len(manifest.Sidecars))}
	seenSidecars := make(map[string]bool)
	var totalSize int64
	for _, entry := range manifest.Sidecars {
		if err := validateSnapshotEntry(entry); err != nil {
			return nil, err
		}
		totalSize += entry.Size
		if totalSize > 512*1024*1024 {
			return nil, fmt.Errorf("snapshot sidecar data exceeds the 512 MiB limit")
		}
		key := strings.ToLower(entry.Sidecar)
		if seenSidecars[key] {
			return nil, fmt.Errorf("duplicate sidecar in snapshot: %s", entry.Sidecar)
		}
		seenSidecars[key] = true
		archiveName := filepath.ToSlash(filepath.Join("sidecars", entry.Sidecar))
		files := archiveFiles[archiveName]
		if len(files) != 1 || files[0].UncompressedSize64 != uint64(entry.Size) {
			return nil, fmt.Errorf("snapshot sidecar is missing or has an invalid size: %s", entry.Sidecar)
		}
		reader, err := files[0].Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(reader, entry.Size+1))
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(data)) != entry.Size {
			return nil, fmt.Errorf("snapshot sidecar has an invalid length: %s", entry.Sidecar)
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), entry.SHA256) {
			return nil, fmt.Errorf("snapshot sidecar checksum failed: %s", entry.Sidecar)
		}
		loaded.data[entry.Sidecar] = data
	}
	return loaded, nil
}

func listXMPSnapshots(sequenceDirectory string) ([]listedXMPSnapshot, error) {
	sequenceInfo, err := os.Stat(sequenceDirectory)
	if err != nil {
		return nil, err
	}
	if !sequenceInfo.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", sequenceDirectory)
	}
	metadataDirectory := filepath.Join(sequenceDirectory, ".lrtl")
	metadataInfo, err := os.Lstat(metadataDirectory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metadataInfo.Mode()&os.ModeSymlink != 0 || !metadataInfo.IsDir() {
		return nil, fmt.Errorf("snapshot metadata path is not a real directory: %s", metadataDirectory)
	}
	directory := snapshotDirectory(sequenceDirectory)
	directoryInfo, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return nil, fmt.Errorf("snapshot path is not a real directory: %s", directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	listed := make([]listedXMPSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		loaded, err := readXMPSnapshot(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		listed = append(listed, listedXMPSnapshot{path: path, manifest: loaded.manifest})
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].manifest.CreatedAt.Before(listed[j].manifest.CreatedAt) })
	return listed, nil
}

func writeRestoredSidecar(path string, data []byte, mode os.FileMode) (string, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		cleanup()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return "", err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		cleanup()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return "", err
	}
	return temporaryPath, nil
}

func restoreXMPSnapshot(sequenceDirectory, archivePath string, write, backup, allowMoved bool, stdout io.Writer) error {
	info, err := os.Stat(sequenceDirectory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", sequenceDirectory)
	}
	loaded, err := readXMPSnapshot(archivePath)
	if err != nil {
		return err
	}
	absoluteDirectory, err := filepath.Abs(sequenceDirectory)
	if err != nil {
		return err
	}
	if !allowMoved && !sameSequencePath(loaded.manifest.Sequence, absoluteDirectory) {
		return fmt.Errorf("snapshot belongs to a different sequence path; pass --allow-moved to restore here")
	}
	type restoreTarget struct {
		path string
		data []byte
		mode os.FileMode
		temp string
		old  bool
	}
	targets := make([]restoreTarget, 0, len(loaded.manifest.Sidecars))
	for _, entry := range loaded.manifest.Sidecars {
		imagePath := filepath.Join(sequenceDirectory, entry.Image)
		imageInfo, err := os.Stat(imagePath)
		if err != nil {
			return fmt.Errorf("snapshot image is missing: %s", imagePath)
		}
		if !imageInfo.Mode().IsRegular() {
			return fmt.Errorf("snapshot image is not a regular file: %s", imagePath)
		}
		targetPath := filepath.Join(sequenceDirectory, entry.Sidecar)
		mode := os.FileMode(0644)
		old := false
		targetInfo, err := os.Lstat(targetPath)
		if err == nil {
			if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
				return fmt.Errorf("target sidecar is not a regular file: %s", targetPath)
			}
			mode, old = targetInfo.Mode().Perm(), true
		} else if !os.IsNotExist(err) {
			return err
		}
		targets = append(targets, restoreTarget{path: targetPath, data: loaded.data[entry.Sidecar], mode: mode, old: old})
		fmt.Fprintf(stdout, "%s: restore %d bytes\n", entry.Sidecar, entry.Size)
	}
	if !write {
		fmt.Fprintf(stdout, "Dry run. Would restore %d XMP sidecars. Pass --write to apply.\n", len(targets))
		return nil
	}
	cleanupTemps := func() {
		for _, target := range targets {
			if target.temp != "" {
				_ = os.Remove(target.temp)
			}
		}
	}
	for index := range targets {
		target := &targets[index]
		target.temp, err = writeRestoredSidecar(target.path, target.data, target.mode)
		if err != nil {
			cleanupTemps()
			return err
		}
	}
	if backup {
		stamp := time.Now().UTC().Format("20060102-150405.000000000")
		for index := range targets {
			target := &targets[index]
			if !target.old {
				continue
			}
			backupPath := fmt.Sprintf("%s.lrtl-snapshot-%s-%04d.bak", target.path, stamp, index+1)
			if err := copyFile(target.path, backupPath); err != nil {
				cleanupTemps()
				return err
			}
			fmt.Fprintf(stdout, "Backup: %s\n", backupPath)
		}
	}
	for index := range targets {
		target := &targets[index]
		if err := os.Rename(target.temp, target.path); err != nil {
			cleanupTemps()
			return fmt.Errorf("restore stopped at %s: %w", target.path, err)
		}
		target.temp = ""
		fmt.Fprintf(stdout, "Saved: %s\n", target.path)
	}
	return nil
}
