package lrtl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type sequenceCopyFile struct {
	source, name string
	kind         string
	info         os.FileInfo
}

type sequenceCopyPlan struct {
	source           string
	destination      string
	frames           int
	sidecars, caches int
	files            []sequenceCopyFile
}

func planSequenceCopy(directory, output string, frames []Frame, includeCompanions ...bool) (*sequenceCopyPlan, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames selected")
	}
	if len(includeCompanions) > 1 {
		return nil, fmt.Errorf("at most one companion option is allowed")
	}
	companions := len(includeCompanions) == 1 && includeCompanions[0]
	paths, err := sequenceCopyPaths(frames, companions)
	if err != nil {
		return nil, err
	}
	return planSequenceCopyFiles(directory, output, paths)
}

func planSequenceCopyFiles(directory, output string, paths []sequenceCopyPath) (*sequenceCopyPlan, error) {
	if strings.TrimSpace(output) == "" {
		return nil, fmt.Errorf("a new output directory is required")
	}
	source, err := resolveFuturePath(directory)
	if err != nil {
		return nil, err
	}
	destination, err := resolveFuturePath(output)
	if err != nil {
		return nil, err
	}
	if pathContains(source, destination) {
		return nil, fmt.Errorf("output must be outside the source sequence")
	}
	if _, err := os.Lstat(destination); err == nil {
		return nil, fmt.Errorf("output already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	plan := &sequenceCopyPlan{source: source, destination: destination}
	seen := map[string]sequenceCopyFile{}
	for _, candidate := range paths {
		path := candidate.source
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("copy source must be a regular file: %s", absolute)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		if candidate.kind != "cache" && !sameFilesystemPath(filepath.Dir(resolved), source) || candidate.kind == "cache" && !pathContains(source, resolved) {
			return nil, fmt.Errorf("copy source is outside the sequence: %s", absolute)
		}
		name := candidate.name
		key := strings.ToLower(name)
		if earlier, exists := seen[key]; exists {
			if samePath(earlier.source, absolute) && os.SameFile(earlier.info, info) {
				continue
			} // Shared stem XMP is copied once.
			return nil, fmt.Errorf("case-insensitive output name collision: %s", name)
		}
		entry := sequenceCopyFile{source: absolute, name: name, info: info, kind: candidate.kind}
		seen[key] = entry
		plan.files = append(plan.files, entry)
		switch candidate.kind {
		case "image":
			plan.frames++
		case "sidecar":
			plan.sidecars++
		case "cache":
			plan.caches++
		}
	}
	return plan, nil
}

func copySourceUnchanged(file sequenceCopyFile) error {
	now, err := os.Lstat(file.source)
	if err != nil {
		return err
	}
	if !now.Mode().IsRegular() || !os.SameFile(file.info, now) || now.Size() != file.info.Size() || !now.ModTime().Equal(file.info.ModTime()) || now.Mode() != file.info.Mode() {
		return fmt.Errorf("source changed after copy planning: %s", file.source)
	}
	return nil
}

func executeSequenceCopy(plan *sequenceCopyPlan, copyOne func(string, string) error) error {
	if copyOne == nil {
		copyOne = copyFile
	}
	if _, err := os.Lstat(plan.destination); err == nil {
		return fmt.Errorf("output already exists: %s", plan.destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(plan.destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".lrtl-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, file := range plan.files {
		if err := copySourceUnchanged(file); err != nil {
			return err
		}
		destination := filepath.Join(staging, file.name)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err := copyOne(file.source, destination); err != nil {
			return fmt.Errorf("copy %s: %w", file.name, err)
		}
		if err := copySourceUnchanged(file); err != nil {
			return err
		}
		info, err := os.Stat(destination)
		if err != nil {
			return err
		}
		if info.Size() != file.info.Size() {
			return fmt.Errorf("incomplete copy: %s", file.name)
		}
		if err := os.Chtimes(destination, file.info.ModTime(), file.info.ModTime()); err != nil {
			return err
		}
	}
	// A file copied early may have changed while later files were copied.
	for _, file := range plan.files {
		if err := copySourceUnchanged(file); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(plan.destination); err == nil {
		return fmt.Errorf("output appeared during copy: %s", plan.destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, plan.destination); err != nil {
		return fmt.Errorf("publish copied sequence: %w", err)
	}
	return nil
}

func runCopySequence(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("copy-sequence", stderr)
	filters := registerFrameSelectionFlags(set)
	output := set.String("output", "", "new directory for selected images and active XMP sidecars")
	write := set.Bool("write", false, "copy the planned files")
	companions := set.Bool("include-lrt-companions", false, "also copy paired JPEGs and per-image LRT previews/proxies")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true, "--output": true, "-output": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl copy-sequence DIRECTORY --output NEW_DIRECTORY [--selection FILTER.json] [--filename-contains TEXT] [--ratings LIST] [--invert] [--include-lrt-companions] [--write]"))
	}
	options, err := filters.options(set)
	if err != nil {
		return reportError(stderr, err)
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	selected, err := filterSequenceFrames(frames, set.Arg(0), options)
	if err != nil {
		return reportError(stderr, err)
	}
	plan, err := planSequenceCopy(set.Arg(0), *output, selected, *companions)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Selected: %d/%d frames; files: %d\nOutput: %s\n", len(selected), len(frames), len(plan.files), plan.destination)
	if *companions {
		fmt.Fprintf(stdout, "Including companions: %d images, %d active sidecars, %d cache files\n", plan.frames, plan.sidecars, plan.caches)
	}
	for _, file := range plan.files {
		fmt.Fprintln(stdout, file.name)
	}
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to copy the sequence.")
		return 0
	}
	if err := executeSequenceCopy(plan, nil); err != nil {
		return reportError(stderr, err)
	}
	if *companions {
		fmt.Fprintf(stdout, "Copied %d images, %d sidecars and %d cache files.\n", plan.frames, plan.sidecars, plan.caches)
	} else {
		fmt.Fprintf(stdout, "Copied %d frames and %d sidecars.\n", plan.frames, plan.sidecars)
	}
	return 0
}
