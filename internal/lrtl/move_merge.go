package lrtl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errMoveMergeRollbackIncomplete = errors.New("partial merge rollback incomplete")

// Validate every existing component without following a link. A directory index
// is shared by one validation pass so long sequences do not rescan the folder.
func moveMergeTarget(root, name string, index map[string]map[string]string) (string, os.FileInfo, error) {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("invalid merge name: %s", name)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	parent := root
	for i, part := range parts {
		if index != nil {
			names, ok := index[parent]
			if !ok {
				entries, err := os.ReadDir(parent)
				if err != nil && !os.IsNotExist(err) {
					return "", nil, err
				}
				names = map[string]string{}
				for _, entry := range entries {
					key := strings.ToLower(entry.Name())
					if _, exists := names[key]; exists {
						names[key] = "" // Ambiguous portable spelling.
					} else {
						names[key] = entry.Name()
					}
				}
				index[parent] = names
			}
			if actual, exists := names[strings.ToLower(part)]; exists && actual != part {
				return "", nil, fmt.Errorf("case-insensitive merge collision: %s", filepath.Join(parent, part))
			}
		}
		path := filepath.Join(parent, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return filepath.Join(root, clean), nil, nil
		}
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("merge path must not contain a symbolic link: %s", path)
		}
		if i == len(parts)-1 {
			return path, info, nil
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("merge parent is not a directory: %s", path)
		}
		parent = path
	}
	return "", nil, fmt.Errorf("invalid merge name: %s", name)
}

func planMoveMerge(plan *sequenceMovePlan) error {
	plan.reuse = make([]os.FileInfo, len(plan.copy.files))
	plan.reuseHashes = make([]string, len(plan.copy.files))
	index := map[string]map[string]string{}
	for i, file := range plan.copy.files {
		target, info, err := moveMergeTarget(plan.copy.destination, file.name, index)
		if err != nil {
			return err
		}
		if info == nil {
			continue
		}
		if file.kind == "image" || !info.Mode().IsRegular() {
			return fmt.Errorf("merge output already exists: %s", target)
		}
		sourceHash, err := moveDigest(file.source)
		if err != nil {
			return err
		}
		targetHash, err := moveDigest(target)
		if err != nil {
			return err
		}
		if sourceHash != targetHash {
			return fmt.Errorf("merge metadata differs: %s", target)
		}
		os.SameFile(info, info)
		plan.reuse[i], plan.reuseHashes[i] = info, sourceHash
	}
	return nil
}

func moveDestinationUnchanged(plan *sequenceMovePlan) error {
	now, err := os.Lstat(plan.copy.destination)
	if err != nil {
		return err
	}
	if !now.IsDir() || now.Mode()&os.ModeSymlink != 0 || !os.SameFile(plan.destinationRoot, now) {
		return fmt.Errorf("merge destination directory changed")
	}
	return nil
}

func validateMoveMerge(plan *sequenceMovePlan) error {
	if err := moveDestinationUnchanged(plan); err != nil {
		return err
	}
	index := map[string]map[string]string{}
	for i, file := range plan.copy.files {
		target, info, err := moveMergeTarget(plan.copy.destination, file.name, index)
		if err != nil {
			return err
		}
		if plan.reuse[i] == nil {
			if info != nil {
				return fmt.Errorf("merge output appeared after planning: %s", target)
			}
			continue
		}
		if info == nil || !info.Mode().IsRegular() || !os.SameFile(plan.reuse[i], info) {
			return fmt.Errorf("shared merge metadata changed: %s", target)
		}
		digest, err := moveDigest(target)
		if err != nil {
			return err
		}
		if digest != plan.reuseHashes[i] {
			return fmt.Errorf("shared merge metadata changed: %s", target)
		}
	}
	return nil
}

// Existing destinations are published per file. On failure only files created
// by this operation are moved back; existing metadata is never overwritten.
func publishMoveMerge(plan *sequenceMovePlan, stage string, ops sequenceMoveOps) error {
	if err := validateMoveMerge(plan); err != nil {
		return err
	}
	type publishedFile struct {
		from, to, digest string
		info             os.FileInfo
	}
	type createdDirectory struct {
		path string
		info os.FileInfo
	}
	var published []publishedFile
	var directories []createdDirectory
	rollback := func(failure error) error {
		var failures []error
		for i := len(published) - 1; i >= 0; i-- {
			file := published[i]
			if err := moveDestinationUnchanged(plan); err != nil {
				failures = append(failures, err)
				continue
			}
			rel, _ := filepath.Rel(plan.copy.destination, file.to)
			_, now, err := moveMergeTarget(plan.copy.destination, rel, nil)
			if err != nil || now == nil || !now.Mode().IsRegular() || !os.SameFile(file.info, now) {
				failures = append(failures, fmt.Errorf("cannot undo changed merge file: %s", file.to))
				continue
			}
			digest, err := moveDigest(file.to)
			if err != nil || digest != file.digest {
				failures = append(failures, fmt.Errorf("cannot undo edited merge file: %s", file.to))
				continue
			}
			if err := ops.move(file.to, file.from); err != nil {
				failures = append(failures, err)
			}
		}
		for i := len(directories) - 1; i >= 0; i-- {
			dir := directories[i]
			if now, err := os.Lstat(dir.path); err == nil && now.IsDir() && os.SameFile(dir.info, now) {
				if err := os.Remove(dir.path); err != nil {
					failures = append(failures, err)
				}
			}
		}
		if len(failures) > 0 {
			return errors.Join(failure, errMoveMergeRollbackIncomplete, errors.Join(failures...))
		}
		return failure
	}
	for i, file := range plan.copy.files {
		if plan.reuse[i] != nil {
			staged, err := moveDigest(filepath.Join(stage, file.name))
			if err != nil || staged != plan.reuseHashes[i] {
				return rollback(fmt.Errorf("shared merge metadata differs from staged copy: %s", file.name))
			}
			continue
		}
		if err := moveDestinationUnchanged(plan); err != nil {
			return rollback(err)
		}
		target, info, err := moveMergeTarget(plan.copy.destination, file.name, nil)
		if err != nil {
			return rollback(err)
		}
		if info != nil {
			return rollback(fmt.Errorf("merge output already exists: %s", target))
		}
		parent := plan.copy.destination
		parts := strings.Split(filepath.Dir(file.name), string(filepath.Separator))
		for _, part := range parts {
			if part == "." {
				continue
			}
			parent = filepath.Join(parent, part)
			if err := os.Mkdir(parent, 0755); err == nil {
				info, err := os.Lstat(parent)
				if err != nil {
					return rollback(err)
				}
				os.SameFile(info, info)
				directories = append(directories, createdDirectory{parent, info})
			} else if !os.IsExist(err) {
				return rollback(err)
			}
			info, err := os.Lstat(parent)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return rollback(fmt.Errorf("merge parent changed: %s", parent))
			}
		}
		from := filepath.Join(stage, file.name)
		stagedInfo, err := os.Lstat(from)
		if err != nil {
			return rollback(err)
		}
		os.SameFile(stagedInfo, stagedInfo)
		digest, err := moveDigest(from)
		if err != nil {
			return rollback(err)
		}
		if err := ops.move(from, target); err != nil {
			return rollback(err)
		}
		published = append(published, publishedFile{from, target, digest, stagedInfo})
	}
	if err := moveDestinationUnchanged(plan); err != nil {
		return rollback(err)
	}
	for i, file := range plan.copy.files {
		if plan.reuse[i] == nil {
			continue
		}
		target, info, err := moveMergeTarget(plan.copy.destination, file.name, nil)
		if err != nil || info == nil || !info.Mode().IsRegular() || !os.SameFile(plan.reuse[i], info) {
			return rollback(fmt.Errorf("shared merge metadata changed during publication: %s", file.name))
		}
		digest, err := moveDigest(target)
		if err != nil || digest != plan.reuseHashes[i] {
			return rollback(fmt.Errorf("shared merge metadata changed during publication: %s", file.name))
		}
	}
	return nil
}
