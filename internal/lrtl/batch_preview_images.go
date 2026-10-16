package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type batchPreviewEntry struct {
	Source    string `json:"source"`
	Directory string `json:"directory"`
	Frames    int    `json:"frames"`
}

type batchPreviewManifest struct {
	Version   int                 `json:"version"`
	Sequences []batchPreviewEntry `json:"sequences"`
}

type batchPreviewResult struct {
	manifest PreviewImageManifest
	err      error
}

func batchPreviewFolder(index int, directory string) string {
	var label strings.Builder
	for _, r := range filepath.Base(directory) {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			r = '_'
		}
		if label.Len()+len(string(r)) > 160 {
			break
		}
		label.WriteRune(r)
	}
	return fmt.Sprintf("sequence_%06d_%s", index+1, strings.Trim(label.String(), " ."))
}

func runBatchPreviewImages(arguments []string, stdout, stderr io.Writer) int {
	batchArgs, imageArgs := arguments, []string(nil)
	for i, arg := range arguments {
		if arg == "--" {
			batchArgs, imageArgs = arguments[:i], arguments[i+1:]
			break
		}
	}
	for _, arg := range imageArgs {
		name := strings.SplitN(strings.TrimLeft(arg, "-"), "=", 2)[0]
		if strings.HasPrefix(arg, "-") && (name == "output" || name == "write-preview-ev" || name == "no-backup" || name == "help" || name == "h") {
			return reportError(stderr, fmt.Errorf("batch-preview-images controls --%s; do not pass it after --", name))
		}
	}
	set := newFlagSet("batch-preview-images", stderr)
	output := set.String("output", "", "new directory for all generated preview sequences")
	workers := set.Int("workers", 1, "number of sequences to generate concurrently (1-64)")
	recursive := set.Bool("recursive", false, "find image sequences at any depth")
	writeEV := set.Bool("write-preview-ev", false, "save source XMP measurements after every preview sequence succeeds")
	noBackup := set.Bool("no-backup", false, "omit source XMP backups with --write-preview-ev")
	if err := set.Parse(flagFirst(batchArgs, map[string]bool{"--output": true, "-output": true, "--workers": true, "-workers": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	if set.NArg() != 1 || strings.TrimSpace(*output) == "" {
		return reportError(stderr, fmt.Errorf("expected one sequence root and --output NEW_DIRECTORY"))
	}
	if *workers < 1 || *workers > 64 {
		return reportError(stderr, fmt.Errorf("--workers must be 1-64"))
	}
	if *noBackup && !*writeEV {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write-preview-ev"))
	}
	root, err := filepath.Abs(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		return reportError(stderr, err)
	}
	if sameFilesystemPath(root, destination) || pathWithin(destination, root) {
		return reportError(stderr, fmt.Errorf("output must not be the sequence root or its parent"))
	}
	if _, err := os.Lstat(destination); err == nil {
		return reportError(stderr, fmt.Errorf("output already exists: %s", destination))
	} else if !os.IsNotExist(err) {
		return reportError(stderr, err)
	}
	directories, err := discoverBatchSequences(root, destination, *recursive)
	if err != nil {
		return reportError(stderr, err)
	}
	if len(directories) == 0 {
		return reportError(stderr, fmt.Errorf("no image sequences found under %s", root))
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return reportError(stderr, err)
	}
	staging, err := os.MkdirTemp(parent, ".lrtl-batch-preview-*")
	if err != nil {
		return reportError(stderr, err)
	}
	defer os.RemoveAll(staging)
	manifest := batchPreviewManifest{Version: 1, Sequences: make([]batchPreviewEntry, len(directories))}
	for i, dir := range directories {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return reportError(stderr, err)
		}
		manifest.Sequences[i] = batchPreviewEntry{Source: filepath.ToSlash(rel), Directory: batchPreviewFolder(i, dir)}
	}
	results := make([]batchPreviewResult, len(directories))
	queue := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < min(*workers, len(directories)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range queue {
				job := filepath.Join(staging, manifest.Sequences[i].Directory)
				args := append([]string(nil), imageArgs...)
				args = append(args, "--output", job, directories[i])
				var jobOut, jobErr bytes.Buffer
				if code := runImageExport(args, &jobOut, &jobErr, true); code != 0 {
					results[i].err = fmt.Errorf("preview generation exited %d: %s", code, strings.TrimSpace(jobErr.String()))
					continue
				}
				data, err := os.ReadFile(filepath.Join(job, "frames.json"))
				if err != nil {
					results[i].err = err
					continue
				}
				results[i].err = json.Unmarshal(data, &results[i].manifest)
			}
		}()
	}
	for i := range directories {
		queue <- i
	}
	close(queue)
	wait.Wait()
	failures, total := 0, 0
	for i, result := range results {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", i+1, len(results), directories[i])
		if result.err != nil {
			failures++
			fmt.Fprintf(stderr, "%s: %s\n", directories[i], strings.ReplaceAll(result.err.Error(), staging, destination))
			continue
		}
		manifest.Sequences[i].Frames = len(result.manifest.Frames)
		total += len(result.manifest.Frames)
		fmt.Fprintf(stdout, "Prepared %d JPEG previews (%dx%d): %s\n", len(result.manifest.Frames), result.manifest.Width, result.manifest.Height, manifest.Sequences[i].Directory)
	}
	if failures > 0 {
		fmt.Fprintf(stderr, "Batch previews: %d sequence(s) failed; no previews published and no source XMP written.\n", failures)
		return 1
	}
	var writes []packetOutput
	if *writeEV {
		seen := map[string]bool{}
		for i, result := range results {
			frames, err := DiscoverSequence(directories[i])
			if err != nil {
				return reportError(stderr, err)
			}
			for _, measurement := range result.manifest.Frames {
				index := measurement.SourceIndex
				if index < 0 || index >= len(frames) || filepath.Base(frames[index].Image) != measurement.Source || math.IsNaN(measurement.ExternalExposureDefault) || math.IsInf(measurement.ExternalExposureDefault, 0) || measurement.ExternalExposureDefault < 0 || measurement.ExternalExposureDefault > 4 {
					return reportError(stderr, fmt.Errorf("invalid preview/source mapping in %s", directories[i]))
				}
				frame := frames[index]
				path := frame.OutputSidecar()
				key := strings.ToLower(filepath.Clean(path))
				if seen[key] {
					return reportError(stderr, fmt.Errorf("multiple preview frames share XMP path: %s", path))
				}
				seen[key] = true
				packet, err := PacketForFrame(frame)
				if err != nil {
					return reportError(stderr, err)
				}
				if err := packet.SetScalar("lrt:ExternalExposureDefault", formatNumber(measurement.ExternalExposureDefault)); err != nil {
					return reportError(stderr, err)
				}
				if _, err := packet.Serialize(); err != nil {
					return reportError(stderr, err)
				}
				writes = append(writes, packetOutput{Path: path, Packet: packet})
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return reportError(stderr, err)
	}
	if err := os.WriteFile(filepath.Join(staging, "sequences.json"), append(data, '\n'), 0644); err != nil {
		return reportError(stderr, err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return reportError(stderr, fmt.Errorf("output appeared during generation: %s", destination))
	} else if !os.IsNotExist(err) {
		return reportError(stderr, err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return reportError(stderr, fmt.Errorf("publish batch previews: %w", err))
	}
	for i, output := range writes {
		backup, err := WritePacket(output.Path, output.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, fmt.Errorf("previews remain at %s; stopped after %d saved XMP files: %w", destination, i, err))
		}
		if backup != "" {
			fmt.Fprintf(stdout, "Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "Saved: %s\n", output.Path)
	}
	fmt.Fprintf(stdout, "Batch previews: %d sequence(s), %d JPEG preview(s), %d XMP file(s) saved: %s\n", len(results), total, len(writes), destination)
	return 0
}
