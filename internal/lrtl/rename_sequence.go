package lrtl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type sequenceRenamePlan struct {
	source, destination string
	info, parentInfo    os.FileInfo
	noop, caseOnly      bool
}

func validateSequenceFolderName(name string) error {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || len(name) > 255 || name != strings.TrimSpace(name) || strings.HasSuffix(name, ".") {
		return fmt.Errorf("new name must be a portable folder name of 1–255 UTF-8 bytes, without leading/trailing whitespace or trailing dots")
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`/\<>:"|?*`, r) {
			return fmt.Errorf("new name contains a forbidden character: %q", r)
		}
	}
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return fmt.Errorf("reserved folder name: %s", name)
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(base, prefix) {
			suffix := []rune(strings.TrimPrefix(base, prefix))
			if len(suffix) == 1 && (suffix[0] >= '1' && suffix[0] <= '9' || strings.ContainsRune("¹²³", suffix[0])) {
				return fmt.Errorf("reserved folder name: %s", name)
			}
		}
	}
	return nil
}

func planSequenceRename(directory, name string) (*sequenceRenamePlan, error) {
	if err := validateSequenceFolderName(name); err != nil {
		return nil, err
	}
	source, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("rename source must be a directory, not a symbolic link: %s", source)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(source)
	if parent == source {
		return nil, fmt.Errorf("cannot rename a filesystem root")
	}
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return nil, err
	}
	// Preserve the stored leaf spelling when Windows accepted a differently
	// cased input path. No-op and case-only behavior use the actual entry name.
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), filepath.Base(source)) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if os.SameFile(info, entryInfo) {
			source = filepath.Join(parent, entry.Name())
			break
		}
	}
	plan := &sequenceRenamePlan{source: source, destination: filepath.Join(parent, name), info: info, parentInfo: parentInfo, noop: filepath.Base(source) == name, caseOnly: strings.EqualFold(filepath.Base(source), name)}
	if err := validateSequenceRenamePlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func validateSequenceRenamePlan(plan *sequenceRenamePlan) error {
	parent, err := os.Stat(filepath.Dir(plan.source))
	if err != nil {
		return err
	}
	if !parent.IsDir() || !os.SameFile(parent, plan.parentInfo) {
		return fmt.Errorf("source parent changed after rename planning")
	}
	now, err := os.Lstat(plan.source)
	if err != nil {
		return err
	}
	if !now.IsDir() || now.Mode()&os.ModeSymlink != 0 || !os.SameFile(now, plan.info) {
		return fmt.Errorf("source directory changed after rename planning")
	}
	entries, err := os.ReadDir(filepath.Dir(plan.source))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), filepath.Base(plan.destination)) {
			continue
		}
		// Only the actual source name may occupy the requested case-folded name.
		if entry.Name() == filepath.Base(plan.source) {
			continue
		}
		return fmt.Errorf("destination already exists (case-insensitive): %s", filepath.Join(filepath.Dir(plan.source), entry.Name()))
	}
	return nil
}

// The usual path is one OS directory rename with no replacement. A case-only
// change uses a private sibling staging directory so it also works on Windows.
// A failed second move is rolled back, or the error names the retained data.
func executeSequenceRename(plan *sequenceRenamePlan, move func(string, string) error) error {
	if move == nil {
		move = renameDirectoryNoReplace
	}
	if err := validateSequenceRenamePlan(plan); err != nil {
		return err
	}
	if plan.noop {
		return nil
	}
	if !plan.caseOnly {
		return move(plan.source, plan.destination)
	}
	holding, err := os.MkdirTemp(filepath.Dir(plan.source), ".lrtl-rename-*")
	if err != nil {
		return err
	}
	staged := filepath.Join(holding, "sequence")
	defer os.Remove(holding) // Empty directories only; retained data is never removed.
	if err := move(plan.source, staged); err != nil {
		return err
	}
	if err := move(staged, plan.destination); err != nil {
		if rollback := move(staged, plan.source); rollback != nil {
			return errors.Join(err, fmt.Errorf("rollback failed: %w; sequence retained at %s", rollback, staged))
		}
		return err
	}
	return nil
}

func runRenameSequence(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("rename-sequence", stderr)
	name := set.String("name", "", "new name for the sequence directory, in the same parent")
	write := set.Bool("write", false, "rename the directory")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--name": true, "-name": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *name == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl rename-sequence DIRECTORY --name NEW_NAME [--write]"))
	}
	plan, err := planSequenceRename(set.Arg(0), *name)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Source: %s\nDestination: %s\n", plan.source, plan.destination)
	if plan.noop {
		fmt.Fprintln(stdout, "No change: directory already has that name.")
		return 0
	}
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to rename the sequence directory.")
		return 0
	}
	if err := executeSequenceRename(plan, nil); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintln(stdout, "Renamed sequence directory.")
	return 0
}
