package lrtl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sequenceCopyPath struct{ source, name, kind string }

func nativeSequenceCompanionNames(image string) (string, bool, []string) {
	name := filepath.Base(image)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	ext := strings.ToLower(filepath.Ext(name))
	return stem, ext != ".jpg" && ext != ".jpeg", []string{
		filepath.Join(".lrt", "previews", stem+".lrtpreview"),
		filepath.Join(".lrt", "visual", stem+".lrtpreview"),
		filepath.Join(".lrt", "proxy", stem+".proxy"),
	}
}

func existingSequenceCompanion(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("sequence companion must be a regular file: %s", path)
	}
	return path, nil
}

type companionDirectoryIndex map[string]map[string][]os.DirEntry

func (index companionDirectoryIndex) actualJPEG(path string) (string, error) {
	path, err := existingSequenceCompanion(path)
	if err != nil || path == "" {
		return path, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// Windows may have resolved a differently cased .jpg spelling. Preserve the
	// actual leaf name. Index the source directory once rather than scanning a
	// potentially long sequence for every selected frame or cache file.
	parent := filepath.Dir(path)
	entries, ok := index[parent]
	if !ok {
		list, err := os.ReadDir(parent)
		if err != nil {
			return "", err
		}
		entries = map[string][]os.DirEntry{}
		for _, entry := range list {
			key := strings.ToLower(entry.Name())
			entries[key] = append(entries[key], entry)
		}
		index[parent] = entries
	}
	for _, entry := range entries[strings.ToLower(filepath.Base(path))] {
		entryInfo, err := entry.Info()
		if err != nil {
			return "", err
		}
		if os.SameFile(info, entryInfo) {
			return filepath.Join(filepath.Dir(path), entry.Name()), nil
		}
	}
	return path, nil // A symlink leaf is rejected by the common source validation.
}

func sequenceCopyPaths(frames []Frame, companions bool) ([]sequenceCopyPath, error) {
	images := append([]Frame(nil), frames...)
	index := companionDirectoryIndex{}
	if companions {
		for _, frame := range frames {
			stem, paired, _ := nativeSequenceCompanionNames(frame.Image)
			if !paired {
				continue
			}
			path, err := index.actualJPEG(filepath.Join(filepath.Dir(frame.Image), stem+".jpg"))
			if err != nil {
				return nil, err
			}
			if path == "" {
				continue
			}
			sidecar, err := findSidecar(path)
			if err != nil {
				return nil, err
			}
			images = append(images, Frame{Image: path, Sidecar: sidecar})
		}
	}
	var paths []sequenceCopyPath
	for _, frame := range images {
		paths = append(paths, sequenceCopyPath{frame.Image, filepath.Base(frame.Image), "image"})
		if frame.Sidecar != "" {
			paths = append(paths, sequenceCopyPath{frame.Sidecar, filepath.Base(frame.Sidecar), "sidecar"})
		}
		if !companions {
			continue
		}
		_, _, cache := nativeSequenceCompanionNames(frame.Image)
		for _, name := range cache {
			path, err := existingSequenceCompanion(filepath.Join(filepath.Dir(frame.Image), name))
			if err != nil {
				return nil, err
			}
			if path == "" {
				continue
			}
			paths = append(paths, sequenceCopyPath{path, name, "cache"})
		}
	}
	return paths, nil
}
