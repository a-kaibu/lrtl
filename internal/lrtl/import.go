package lrtl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ImportOptions struct {
	Recursive        bool
	SkipJPEG         bool
	SortByCapture    bool
	RenameByDate     bool
	DryRun           bool
	ConvertToDNG     bool
	DNGCompression   string
	DNGLabPath       string
	DNGConverterPath string
	SplitGap         time.Duration
}

type importItem struct {
	source      string
	sidecar     string
	destination string
	captureTime time.Time
	convertDNG  bool
}

var dngImportRawExtensions = map[string]bool{
	".raw": true, ".rwl": true, ".gpr": true, ".x3f": true,
	".3fr": true, ".ari": true, ".arw": true, ".cr2": true, ".cr3": true,
	".crw": true, ".dcr": true, ".dcs": true, ".erf": true, ".fff": true,
	".iiq": true, ".kdc": true, ".mef": true, ".mos": true, ".mrw": true,
	".nef": true, ".nrw": true, ".orf": true, ".pef": true, ".raf": true,
	".rw2": true, ".sr2": true, ".srf": true, ".srw": true,
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func resolveFuturePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absolute
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func collectImportImages(source string, recursive, skipJPEG bool) ([]string, error) {
	var images []string
	var walk func(string) error
	walk = func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				if recursive {
					if err := walk(path); err != nil {
						return err
					}
				}
				continue
			}
			if !entry.Type().IsRegular() || !imageExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
				continue
			}
			if skipJPEG {
				extension := strings.ToLower(filepath.Ext(entry.Name()))
				if extension == ".jpg" || extension == ".jpeg" {
					continue
				}
			}
			images = append(images, path)
		}
		return nil
	}
	if err := walk(source); err != nil {
		return nil, err
	}
	sort.SliceStable(images, func(left, right int) bool { return naturalLess(images[left], images[right]) })
	return images, nil
}

func planImport(source, destination string, options ImportOptions) ([]importItem, error) {
	if options.SplitGap < 0 {
		return nil, fmt.Errorf("split gap must be positive or zero (disabled)")
	}
	captureOrder := options.SortByCapture || options.RenameByDate || options.SplitGap > 0
	if options.DNGConverterPath != "" && options.DNGLabPath != "" {
		return nil, fmt.Errorf("--dng-converter and --dnglab cannot be combined")
	}
	if !options.ConvertToDNG && (options.DNGConverterPath != "" || options.DNGLabPath != "") {
		return nil, fmt.Errorf("RAW converter paths require --convert-dng")
	}
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return nil, fmt.Errorf("resolve output: %w", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, err
	}
	if !sourceInfo.IsDir() {
		return nil, fmt.Errorf("source must be a directory: %s", sourcePath)
	}
	compression := strings.ToLower(strings.TrimSpace(options.DNGCompression))
	if options.ConvertToDNG {
		if compression == "" {
			compression = "lossless"
		}
		if compression != "lossless" && compression != "uncompressed" {
			return nil, fmt.Errorf("DNG compression must be lossless or uncompressed: %q", options.DNGCompression)
		}
	} else if compression != "" {
		return nil, fmt.Errorf("DNG compression can only be selected with --convert-dng")
	}
	sourceCanonical, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve source path: %w", err)
	}
	destinationCanonical, err := resolveFuturePath(destinationPath)
	if err != nil {
		return nil, fmt.Errorf("resolve output path: %w", err)
	}
	if pathContains(sourceCanonical, destinationCanonical) {
		return nil, fmt.Errorf("output directory must be outside the source directory")
	}
	images, err := collectImportImages(sourcePath, options.Recursive, options.SkipJPEG)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("no supported images found in %s", sourcePath)
	}
	items := make([]importItem, len(images))
	for index, imagePath := range images {
		items[index].source = imagePath
		items[index].destination = filepath.Base(imagePath)
		extension := strings.ToLower(filepath.Ext(imagePath))
		if options.ConvertToDNG && dngImportRawExtensions[extension] {
			items[index].convertDNG = true
			items[index].destination = strings.TrimSuffix(items[index].destination, filepath.Ext(items[index].destination)) + ".dng"
		}
		if options.Recursive {
			relativeDirectory, err := filepath.Rel(sourcePath, filepath.Dir(imagePath))
			if err != nil {
				return nil, fmt.Errorf("resolve relative source path: %w", err)
			}
			if relativeDirectory != "." {
				prefix := strings.ReplaceAll(filepath.ToSlash(relativeDirectory), "/", "_")
				items[index].destination = prefix + "__" + items[index].destination
			}
		}
		sidecar, err := findSidecar(imagePath)
		if err != nil {
			return nil, fmt.Errorf("find sidecar for %s: %w", filepath.Base(imagePath), err)
		}
		items[index].sidecar = sidecar
		if captureOrder {
			frame := Frame{Image: imagePath, Sidecar: sidecar}
			items[index].captureTime, err = readFrameCaptureTime(frame)
			if err != nil {
				return nil, fmt.Errorf("%s: capture time is required for date sorting, renaming or splitting: %w", filepath.Base(imagePath), err)
			}
		}
	}
	if captureOrder {
		sort.SliceStable(items, func(left, right int) bool {
			if !items[left].captureTime.Equal(items[right].captureTime) {
				return items[left].captureTime.Before(items[right].captureTime)
			}
			return naturalLess(items[left].source, items[right].source)
		})
	}
	if options.RenameByDate {
		nameCounts := make(map[string]int)
		for index := range items {
			stamp := items[index].captureTime.Format("20060102_150405")
			name := stamp
			if duplicate := nameCounts[stamp]; duplicate > 0 {
				name = fmt.Sprintf("%s_%03d", stamp, duplicate)
			}
			nameCounts[stamp]++
			items[index].destination = name + filepath.Ext(items[index].destination)
		}
	} else if captureOrder {
		for index := range items {
			items[index].destination = fmt.Sprintf("%06d_%s", index+1, items[index].destination)
		}
	}
	if options.SplitGap > 0 {
		sequence := 0
		folder := ""
		for i := range items {
			if i == 0 || items[i].captureTime.Sub(items[i-1].captureTime) > options.SplitGap {
				sequence++
				folder = fmt.Sprintf("sequence_%03d_%s", sequence, items[i].captureTime.Format("20060102_150405"))
			}
			items[i].destination = filepath.Join(folder, items[i].destination)
		}
	}
	seenNames := make(map[string]string, len(items))
	for index, item := range items {
		key := strings.ToLower(item.destination)
		if earlier, exists := seenNames[key]; exists {
			return nil, fmt.Errorf("destination name collision: %s and %s both map to %s", filepath.Base(earlier), filepath.Base(item.source), item.destination)
		}
		seenNames[key] = item.source
		imageDestination := filepath.Join(destinationPath, item.destination)
		resolved, err := resolveFuturePath(imageDestination)
		if err != nil {
			return nil, fmt.Errorf("resolve image destination: %w", err)
		}
		if !pathContains(destinationCanonical, resolved) {
			return nil, fmt.Errorf("image destination escapes output directory: %s", imageDestination)
		}
		if info, err := os.Stat(filepath.Dir(imageDestination)); err == nil && !info.IsDir() {
			return nil, fmt.Errorf("image destination parent is not a directory: %s", filepath.Dir(imageDestination))
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("check image destination parent: %w", err)
		}
		for _, candidate := range sidecarCandidates(imageDestination) {
			if _, err := os.Lstat(candidate); err == nil {
				return nil, fmt.Errorf("destination already exists: %s", candidate)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("check destination %s: %w", candidate, err)
			}
		}
		if _, err := os.Lstat(imageDestination); err == nil {
			return nil, fmt.Errorf("destination already exists: %s", imageDestination)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("check destination %s: %w", imageDestination, err)
		}
		items[index].destination = item.destination
	}
	return items, nil
}

func ImportSequence(source, destination string, options ImportOptions, stdout io.Writer) error {
	items, err := planImport(source, destination, options)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintf(stdout, "%-42s <- %s", item.destination, filepath.Base(item.source))
		if item.convertDNG {
			fmt.Fprint(stdout, " [convert to DNG]")
		}
		fmt.Fprintln(stdout)
		if item.sidecar != "" {
			fmt.Fprintf(stdout, "  XMP: %s\n", filepath.Base(item.sidecar))
		}
	}
	if options.DryRun {
		fmt.Fprintf(stdout, "Preview only. %d image(s) would be imported to %s.\n", len(items), destination)
		return nil
	}
	dnglabExecutable := ""
	for _, item := range items {
		if item.convertDNG {
			if strings.TrimSpace(options.DNGLabPath) != "" {
				dnglabExecutable, err = resolveDNGLab(options.DNGLabPath)
			} else {
				dnglabExecutable, err = resolveDNGConverter(options.DNGConverterPath)
			}
			if err != nil {
				return err
			}
			break
		}
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	created := make([]string, 0, len(items)*2)
	var createdDirectories []string
	rollback := func() {
		for index := len(created) - 1; index >= 0; index-- {
			_ = os.Remove(created[index])
		}
		for i := len(createdDirectories) - 1; i >= 0; i-- {
			_ = os.Remove(createdDirectories[i])
		}
	}
	for _, item := range items {
		imageDestination := filepath.Join(destination, item.destination)
		if options.SplitGap > 0 {
			directory := filepath.Dir(imageDestination)
			if err := os.Mkdir(directory, 0755); err == nil {
				createdDirectories = append(createdDirectories, directory)
			} else if !os.IsExist(err) {
				rollback()
				return fmt.Errorf("create sequence directory: %w", err)
			}
		}
		var imageErr error
		if item.convertDNG {
			fmt.Fprintf(stdout, "Converting %s to DNG\n", filepath.Base(item.source))
			compression := strings.ToLower(strings.TrimSpace(options.DNGCompression))
			if strings.TrimSpace(compression) == "" {
				compression = "lossless"
			}
			if strings.TrimSpace(options.DNGLabPath) != "" {
				imageErr = convertRawToDNG(dnglabExecutable, item.source, imageDestination, compression)
			} else {
				imageErr = convertRawWithDNGConverter(dnglabExecutable, item.source, imageDestination, compression)
			}
		} else {
			imageErr = copyFile(item.source, imageDestination)
		}
		if imageErr != nil {
			rollback()
			if item.convertDNG {
				return fmt.Errorf("convert %s to DNG: %w", filepath.Base(item.source), imageErr)
			}
			return fmt.Errorf("copy %s: %w", filepath.Base(item.source), imageErr)
		}
		created = append(created, imageDestination)
		if item.sidecar != "" {
			sidecarDestination := imageDestination + ".xmp"
			if err := copyFile(item.sidecar, sidecarDestination); err != nil {
				rollback()
				return fmt.Errorf("copy XMP for %s: %w", filepath.Base(item.source), err)
			}
			created = append(created, sidecarDestination)
		}
	}
	fmt.Fprintf(stdout, "Imported %d image(s) to %s.\n", len(items), destination)
	return nil
}
