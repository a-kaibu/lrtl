package lrtl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type batchRenderResult struct {
	directory string
	stdout    []byte
	stderr    []byte
	code      int
}

func runBatchRender(arguments []string, stdout, stderr io.Writer) int {
	batchArguments := arguments
	var renderArguments []string
	for index, argument := range arguments {
		if argument == "--" {
			batchArguments = arguments[:index]
			renderArguments = arguments[index+1:]
			break
		}
	}
	for _, argument := range renderArguments {
		if argument == "--output" || argument == "-output" || strings.HasPrefix(argument, "--output=") || strings.HasPrefix(argument, "-output=") {
			return reportError(stderr, fmt.Errorf("batch-render controls each output path; do not pass --output after --"))
		}
	}

	set := newFlagSet("batch-render", stderr)
	outputDirectory := set.String("output", "", "destination directory for rendered sequences")
	format := set.String("format", "mp4", "output file format: avi, mp4, or mov")
	workers := set.Int("workers", 1, "number of sequences to render concurrently (1-64)")
	recursive := set.Bool("recursive", false, "find sequence directories at any depth")
	if err := set.Parse(flagFirst(batchArguments, map[string]bool{
		"--output": true, "-output": true,
		"--format": true, "-format": true,
		"--workers": true, "-workers": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one root directory containing image sequences"))
	}
	if strings.TrimSpace(*outputDirectory) == "" {
		return reportError(stderr, fmt.Errorf("--output is required"))
	}
	if *workers < 1 || *workers > 64 {
		return reportError(stderr, fmt.Errorf("--workers must be between 1 and 64: %d", *workers))
	}
	formatName := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(*format), "."))
	if formatName != "avi" && formatName != "mp4" && formatName != "mov" {
		return reportError(stderr, fmt.Errorf("unsupported batch output format %q (use avi, mp4, or mov)", *format))
	}

	root, err := filepath.Abs(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	outputRoot, err := filepath.Abs(*outputDirectory)
	if err != nil {
		return reportError(stderr, err)
	}
	if sameFilesystemPath(root, outputRoot) {
		return reportError(stderr, fmt.Errorf("--output must not be the sequence root directory"))
	}
	if pathWithin(outputRoot, root) {
		return reportError(stderr, fmt.Errorf("--output must not be a parent of the sequence root"))
	}
	sequences, err := discoverBatchSequences(root, outputRoot, *recursive)
	if err != nil {
		return reportError(stderr, err)
	}
	if len(sequences) == 0 {
		return reportError(stderr, fmt.Errorf("no image sequence directories found under %s", root))
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return reportError(stderr, err)
	}

	jobs := make([]string, len(sequences))
	for index, directory := range sequences {
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			return reportError(stderr, err)
		}
		if relative == "." {
			relative = filepath.Base(root)
		}
		jobs[index] = filepath.Join(outputRoot, relative+"."+formatName)
		if err := os.MkdirAll(filepath.Dir(jobs[index]), 0o755); err != nil {
			return reportError(stderr, err)
		}
	}

	results := make([]batchRenderResult, len(sequences))
	queue := make(chan int)
	var wait sync.WaitGroup
	workerCount := min(*workers, len(sequences))
	wait.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer wait.Done()
			for index := range queue {
				jobArguments := append([]string(nil), renderArguments...)
				jobArguments = append(jobArguments, "--output", jobs[index], sequences[index])
				var jobStdout, jobStderr bytes.Buffer
				code := runRender(jobArguments, &jobStdout, &jobStderr)
				results[index] = batchRenderResult{
					directory: sequences[index],
					stdout:    append([]byte(nil), jobStdout.Bytes()...),
					stderr:    append([]byte(nil), jobStderr.Bytes()...),
					code:      code,
				}
			}
		}()
	}
	for index := range sequences {
		queue <- index
	}
	close(queue)
	wait.Wait()

	failures := 0
	for index, result := range results {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", index+1, len(results), result.directory)
		if len(result.stdout) > 0 {
			_, _ = stdout.Write(result.stdout)
			if result.stdout[len(result.stdout)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
		}
		if result.code != 0 {
			failures++
			if len(result.stderr) > 0 {
				_, _ = stderr.Write(result.stderr)
				if result.stderr[len(result.stderr)-1] != '\n' {
					fmt.Fprintln(stderr)
				}
			} else {
				fmt.Fprintf(stderr, "batch-render failed for %s (exit %d)\n", result.directory, result.code)
			}
		}
	}
	fmt.Fprintf(stdout, "Batch render: %d sequence(s), %d succeeded, %d failed\n", len(results), len(results)-failures, failures)
	if failures > 0 {
		return 1
	}
	return 0
}

func discoverBatchSequences(root, outputRoot string, recursive bool) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sequence root is not a directory: %s", root)
	}
	var sequences []string
	var scan func(string, bool) error
	scan = func(directory string, descend bool) error {
		if outputRoot != "" && pathWithin(outputRoot, directory) {
			return nil
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		children := make([]string, 0)
		hasImages := false
		for _, entry := range entries {
			if entry.IsDir() {
				children = append(children, filepath.Join(directory, entry.Name()))
				continue
			}
			if entry.Type().IsRegular() && imageExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
				hasImages = true
			}
		}
		if hasImages {
			sequences = append(sequences, directory)
		}
		if descend {
			for _, child := range children {
				if err := scan(child, true); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := scan(root, recursive); err != nil {
		return nil, err
	}
	if !recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			child := filepath.Join(root, entry.Name())
			if outputRoot != "" && pathWithin(outputRoot, child) {
				continue
			}
			if err := scan(child, false); err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(sequences)
	return sequences, nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func sameFilesystemPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
