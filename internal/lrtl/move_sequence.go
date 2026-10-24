package lrtl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type sequenceMovePlan struct {
	copy            *sequenceCopyPlan
	keep            []bool
	root            os.FileInfo
	destinationRoot os.FileInfo
	reuse           []os.FileInfo
	reuseHashes     []string
}

func movePathKey(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func planSequenceMove(directory, output string, all, selected []Frame, companions bool, mergeOption ...bool) (*sequenceMovePlan, error) {
	if len(mergeOption) > 1 {
		return nil, fmt.Errorf("at most one merge option is allowed")
	}
	merge := len(mergeOption) == 1 && mergeOption[0]
	if merge {
		if info, err := os.Lstat(filepath.Clean(output)); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("merge destination must be a directory, not a symbolic link")
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no frames selected")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	root, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("move source must be a directory, not a symbolic link")
	}
	paths, err := sequenceCopyPaths(selected, companions)
	if err != nil {
		return nil, err
	}
	// Moving also carries inactive candidate sidecars, so settings are not left
	// orphaned when the active sidecar uses the other naming convention.
	for _, entry := range append([]sequenceCopyPath(nil), paths...) {
		if entry.kind != "image" {
			continue
		}
		for _, path := range sidecarCandidates(entry.source) {
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			paths = append(paths, sequenceCopyPath{path, filepath.Base(path), "sidecar"})
		}
	}
	copy, err := planSequenceCopyFilesInto(directory, output, paths, merge)
	if err != nil {
		return nil, err
	}
	moved := map[string]bool{}
	for _, file := range copy.files {
		if file.kind == "image" {
			moved[movePathKey(file.source)] = true
		}
	}
	needed := map[string]bool{}
	for _, frame := range all {
		if moved[movePathKey(frame.Image)] {
			continue
		}
		for _, path := range append(sidecarCandidates(frame.Image), frame.Sidecar) {
			if path == "" {
				continue
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			needed[movePathKey(absolute)] = true
		}
		_, _, cache := nativeSequenceCompanionNames(frame.Image)
		for _, name := range cache {
			absolute, err := filepath.Abs(filepath.Join(filepath.Dir(frame.Image), name))
			if err != nil {
				return nil, err
			}
			needed[movePathKey(absolute)] = true
		}
	}
	plan := &sequenceMovePlan{copy: copy, root: root, keep: make([]bool, len(copy.files))}
	if !os.SameFile(root, root) {
		return nil, fmt.Errorf("cannot capture source directory identity")
	}
	for i, file := range copy.files {
		plan.keep[i] = file.kind != "image" && needed[movePathKey(file.source)]
		if err := copySourceUnchanged(file); err != nil {
			return nil, err
		}
	}
	if merge {
		if info, err := os.Lstat(copy.destination); err == nil {
			plan.destinationRoot = info
			os.SameFile(info, info)
			if err := planMoveMerge(plan); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return plan, nil
}

type sequenceMoveOps struct {
	copy, move func(string, string) error
	remove     func(string) error
}

type sequenceMoveJournal struct {
	Version             int                       `json:"version"`
	Source              string                    `json:"source"`
	Destination         string                    `json:"destination"`
	Staging             string                    `json:"destination_staging"`
	ExistingDestination bool                      `json:"existing_destination"`
	Files               []sequenceMoveJournalFile `json:"files"`
}
type sequenceMoveJournalFile struct {
	Original, Held, Name, SHA256 string
	KeepOriginal                 bool
	ReuseDestination             bool
}

func moveDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeMoveJournal(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(value)
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

// Only known held files and the journal are removed. Unexpected files keep the
// private holding directory nonempty rather than being recursively discarded.
func removeEmptyMoveDirectories(root string) error {
	var directories []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Remove(directories[i]); err != nil {
			return err
		}
	}
	return nil
}

func moveRootUnchanged(plan *sequenceMovePlan) error {
	now, err := os.Lstat(plan.copy.source)
	if err != nil {
		return err
	}
	if !now.IsDir() || !os.SameFile(plan.root, now) {
		return fmt.Errorf("source directory changed after move planning")
	}
	return nil
}

func executeSequenceMove(plan *sequenceMovePlan, ops sequenceMoveOps) error {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return fmt.Errorf("move-sequence is currently supported on Windows and Linux")
	}
	if ops.copy == nil {
		ops.copy = copyFile
	}
	if ops.move == nil {
		ops.move = renameDirectoryNoReplace
	}
	if ops.remove == nil {
		ops.remove = removeMovedOriginal
	}
	if err := moveRootUnchanged(plan); err != nil {
		return err
	}
	if plan.destinationRoot != nil {
		if err := validateMoveMerge(plan); err != nil {
			return err
		}
	} else if _, err := os.Lstat(plan.copy.destination); err == nil {
		return fmt.Errorf("output already exists: %s", plan.copy.destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(plan.copy.destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".lrtl-move-copy-*")
	if err != nil {
		return err
	}
	retainStage := false
	stageInfo, err := os.Stat(stage)
	if err != nil {
		return err
	}
	if !os.SameFile(stageInfo, stageInfo) {
		return fmt.Errorf("cannot capture destination staging identity")
	}
	defer func() {
		if !retainStage {
			if now, err := os.Lstat(stage); err == nil && now.IsDir() && os.SameFile(stageInfo, now) {
				os.RemoveAll(stage)
			}
		}
	}()
	journal := sequenceMoveJournal{Version: 1, Source: plan.copy.source, Destination: plan.copy.destination, Staging: stage, ExistingDestination: plan.destinationRoot != nil}
	for i, file := range plan.copy.files {
		if err := copySourceUnchanged(file); err != nil {
			return err
		}
		digest, err := moveDigest(file.source)
		if err != nil {
			return err
		}
		target := filepath.Join(stage, file.name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := ops.copy(file.source, target); err != nil {
			return err
		}
		if err := copySourceUnchanged(file); err != nil {
			return err
		}
		after, err := moveDigest(file.source)
		if err != nil {
			return err
		}
		copied, err := moveDigest(target)
		if err != nil {
			return err
		}
		if after != digest || copied != digest {
			return fmt.Errorf("move copy verification failed: %s", file.source)
		}
		if err := os.Chtimes(target, file.info.ModTime(), file.info.ModTime()); err != nil {
			return err
		}
		if err := os.Chmod(target, file.info.Mode().Perm()); err != nil {
			return err
		}
		reuse := plan.destinationRoot != nil && plan.reuse[i] != nil
		journal.Files = append(journal.Files, sequenceMoveJournalFile{Original: file.source, Name: file.name, SHA256: digest, KeepOriginal: plan.keep[i], ReuseDestination: reuse})
	}
	if err := moveRootUnchanged(plan); err != nil {
		return err
	}
	holding, err := os.MkdirTemp(plan.copy.source, ".lrtl-move-*")
	if err != nil {
		return err
	}
	for i := range journal.Files {
		journal.Files[i].Held = filepath.Join(holding, "files", journal.Files[i].Name)
	}
	journalPath := filepath.Join(holding, "move-journal.json")
	if err := writeMoveJournal(journalPath, journal); err != nil {
		os.Remove(journalPath)
		os.Remove(holding)
		return err
	}
	var held []int
	rollback := func(failure error) error {
		var failures []error
		for n := len(held) - 1; n >= 0; n-- {
			i := held[n]
			file := journal.Files[i]
			if err := moveRootUnchanged(plan); err != nil {
				failures = append(failures, err)
				continue
			}
			resolved, err := filepath.EvalSymlinks(filepath.Dir(file.Original))
			if err != nil || !pathContains(plan.copy.source, resolved) {
				failures = append(failures, fmt.Errorf("original parent changed: %s", file.Original))
				continue
			}
			if err := ops.move(file.Held, file.Original); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) > 0 {
			retainStage = true
			return errors.Join(failure, errors.Join(failures...), fmt.Errorf("rollback incomplete; originals retained at %s; verified copies at %s", holding, stage))
		}
		if retainStage {
			return errors.Join(failure, fmt.Errorf("restored originals; recovery journal retained at %s", holding))
		}
		if err := os.Remove(journalPath); err != nil {
			return errors.Join(failure, fmt.Errorf("restored originals; journal retained at %s: %w", holding, err))
		}
		if err := removeEmptyMoveDirectories(holding); err != nil {
			return errors.Join(failure, err)
		}
		return failure
	}
	for i, file := range plan.copy.files {
		if err := copySourceUnchanged(file); err != nil {
			return rollback(err)
		}
		if !plan.keep[i] {
			if err := os.MkdirAll(filepath.Dir(journal.Files[i].Held), 0755); err != nil {
				return rollback(err)
			}
			if err := ops.move(file.source, journal.Files[i].Held); err != nil {
				return rollback(err)
			}
			held = append(held, i)
		}
	}
	for i, file := range journal.Files {
		path := file.Held
		if plan.keep[i] {
			path = file.Original
		}
		info, err := os.Lstat(path)
		if err != nil {
			return rollback(err)
		}
		if !info.Mode().IsRegular() || !os.SameFile(plan.copy.files[i].info, info) {
			return rollback(fmt.Errorf("source identity changed before publication: %s", file.Original))
		}
		digest, err := moveDigest(path)
		if err != nil {
			return rollback(err)
		}
		if digest != file.SHA256 {
			return rollback(fmt.Errorf("source changed before move publication: %s", file.Original))
		}
	}
	if err := moveRootUnchanged(plan); err != nil {
		return rollback(err)
	}
	if plan.destinationRoot != nil {
		if err := publishMoveMerge(plan, stage, ops); err != nil {
			// Retain verified copies even if undoing a partial merge failed.
			if errors.Is(err, errMoveMergeRollbackIncomplete) {
				retainStage = true
				err = errors.Join(err, fmt.Errorf("merge copies retained at %s", stage))
			}
			return rollback(err)
		}
	} else if err := ops.move(stage, plan.copy.destination); err != nil {
		return rollback(err)
	}
	// Publication is the commit point. Never roll back a complete destination
	// because removing redundant source backups later failed.
	committedError := func(err error) error {
		return fmt.Errorf("move published at %s; original backup retained at %s; cleanup failed: %w", plan.copy.destination, holding, err)
	}
	if err := writeMoveJournal(filepath.Join(holding, "published.json"), map[string]string{"destination": plan.copy.destination}); err != nil {
		return committedError(err)
	}
	for _, i := range held {
		file := journal.Files[i]
		originalDigest, err := moveDigest(file.Held)
		if err != nil {
			return committedError(err)
		}
		publishedDigest, err := moveDigest(filepath.Join(plan.copy.destination, file.Name))
		if err != nil {
			return committedError(err)
		}
		if originalDigest != file.SHA256 || publishedDigest != file.SHA256 {
			return committedError(fmt.Errorf("file changed after publication: %s", file.Name))
		}
		if err := ops.remove(file.Held); err != nil {
			return committedError(err)
		}
	}
	if err := os.Remove(filepath.Join(holding, "published.json")); err != nil {
		return committedError(err)
	}
	if err := os.Remove(journalPath); err != nil {
		return committedError(err)
	}
	if err := removeEmptyMoveDirectories(holding); err != nil {
		return committedError(err)
	}
	return nil
}

func runMoveSequence(arguments []string, stdout, stderr io.Writer) int {
	return runTransferSequence(arguments, stdout, stderr, false)
}

func runRemoveSequence(arguments []string, stdout, stderr io.Writer) int {
	return runTransferSequence(arguments, stdout, stderr, true)
}

func runTransferSequence(arguments []string, stdout, stderr io.Writer, remove bool) int {
	command := "move-sequence"
	if remove {
		command = "remove-sequence"
	}
	set := newFlagSet(command, stderr)
	filters := registerFrameSelectionFlags(set)
	outputValue := ""
	output := &outputValue
	mergeValue := remove
	merge := &mergeValue
	if !remove {
		output = set.String("output", "", "destination directory outside the source")
		merge = set.Bool("merge", false, "append to an existing directory without replacing files")
	}
	companions := set.Bool("include-lrt-companions", true, "include paired JPEGs and per-image LRT caches")
	write := set.Bool("write", false, "move the planned files")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--output": true, "-output": true, "--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || !remove && *output == "" {
		outputUsage := "--output DIRECTORY [--merge] "
		if remove {
			outputUsage = ""
		}
		return reportError(stderr, fmt.Errorf("Usage: lrtl %s DIRECTORY %s[--selection FILTER.json] [--filename-contains TEXT] [--ratings LIST] [--invert] [--include-lrt-companions=false] [--write]", command, outputUsage))
	}
	if remove {
		source, err := filepath.EvalSymlinks(set.Arg(0))
		if err != nil {
			return reportError(stderr, err)
		}
		source, err = filepath.Abs(source)
		if err != nil {
			return reportError(stderr, err)
		}
		if filepath.Dir(source) == source {
			return reportError(stderr, fmt.Errorf("cannot remove a filesystem root sequence"))
		}
		*output = source + "_REMOVED"
	}
	options, err := filters.options(set)
	if err != nil {
		return reportError(stderr, err)
	}
	all, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	selected, err := filterSequenceFrames(all, set.Arg(0), options)
	if err != nil {
		return reportError(stderr, err)
	}
	plan, err := planSequenceMove(set.Arg(0), *output, all, selected, *companions, *merge)
	if err != nil {
		return reportError(stderr, err)
	}
	kept := 0
	for _, keep := range plan.keep {
		if keep {
			kept++
		}
	}
	fmt.Fprintf(stdout, "Selected: %d/%d; moving images: %d; shared metadata retained: %d\nOutput: %s\n", len(selected), len(all), plan.copy.frames, kept, plan.copy.destination)
	for i, file := range plan.copy.files {
		action := "move"
		if plan.keep[i] {
			action = "copy (shared)"
		}
		if plan.destinationRoot != nil && plan.reuse[i] != nil {
			action = "reuse existing (move original)"
			if plan.keep[i] {
				action = "reuse existing (keep shared original)"
			}
		}
		fmt.Fprintf(stdout, "%s: %s\n", action, file.name)
	}
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to move the sequence files.")
		return 0
	}
	if err := executeSequenceMove(plan, sequenceMoveOps{}); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Moved %d images; %d shared metadata files kept in source.\n", plan.copy.frames, kept)
	return 0
}
