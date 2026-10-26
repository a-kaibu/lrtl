package lrtl

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func resolveDNGConverter(path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("find Adobe DNG Converter %q: %w", path, err)
		}
		return resolved, nil
	}
	for _, name := range []string{"Adobe DNG Converter.exe", "Adobe DNG Converter"} {
		if resolved, err := exec.LookPath(name); err == nil {
			return resolved, nil
		}
	}
	candidates := []string{"/Applications/Adobe DNG Converter.app/Contents/MacOS/Adobe DNG Converter"}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if base := os.Getenv(variable); base != "" {
			candidates = append(candidates,
				filepath.Join(base, "Adobe", "Adobe DNG Converter", "Adobe DNG Converter.exe"),
				filepath.Join(base, "Adobe DNG Converter.exe"))
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Adobe DNG Converter was not found; install it or pass --dng-converter PATH (use --dnglab PATH for the optional DNGLab backend)")
}

// runDNGConverter always writes into a private directory. Converter options
// precede the absolute source path, including when a filename starts with '-'.
func runDNGConverter(executable, source, directory, name string, linear bool, compression string, longEdge int) error {
	if compression != "lossless" && compression != "uncompressed" && compression != "lossy" {
		return fmt.Errorf("unsupported Adobe DNG compression: %q", compression)
	}
	if longEdge < 0 || longEdge > 32768 || (longEdge > 0 && compression != "lossy") || (linear && (compression != "uncompressed" || longEdge != 0)) {
		return fmt.Errorf("invalid Adobe DNG resize/development options")
	}
	resolved, err := resolveDNGConverter(executable)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve RAW source: %w", err)
	}
	arguments := []string{"-c", "-p1", "-dng1.4"}
	if compression == "uncompressed" {
		arguments[0] = "-u"
	} else if compression == "lossy" {
		arguments[0] = "-lossy"
	}
	if linear {
		arguments = []string{"-u", "-l", "-p0", "-dng1.4"}
	}
	if longEdge > 0 {
		arguments = append(arguments, "-side", strconv.Itoa(longEdge))
	}
	arguments = append(arguments, "-d", directory, "-o", name, absolute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, resolved, arguments...)
	var diagnostic cappedBuffer
	diagnostic.limit = ffmpegStderrLimit
	command.Stdout, command.Stderr = &diagnostic, &diagnostic
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Adobe DNG Converter timed out for %s: %w", filepath.Base(source), ctx.Err())
		}
		return fmt.Errorf("Adobe DNG Converter failed for %s: %w: %s", filepath.Base(source), err, strings.TrimSpace(diagnostic.String()))
	}
	info, err := os.Stat(filepath.Join(directory, name))
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("Adobe DNG Converter produced no valid output for %s: %s", filepath.Base(source), strings.TrimSpace(diagnostic.String()))
	}
	if info.Size() > maxDevelopedRAWBytes {
		return fmt.Errorf("Adobe DNG Converter output exceeds the 1 GiB limit for %s", filepath.Base(source))
	}
	return nil
}

func developRawWithDNGConverter(source, executable string) ([]byte, error) {
	return developRawWithDNGConverterProfile(source, executable, "")
}

func developRawWithDNGConverterProfile(source, executable, cameraProfile string) ([]byte, error) {
	var directError error
	if strings.TrimSpace(executable) == "" && strings.EqualFold(filepath.Ext(source), ".dng") {
		if data, ok, err := developLinearDNGFile(source, cameraProfile); ok {
			if err == nil {
				return data, nil
			}
			directError = err
		}
	}
	directory, err := os.MkdirTemp("", "lrtl-linear-dng-*")
	if err != nil {
		return nil, fmt.Errorf("create RAW development directory: %w", err)
	}
	defer os.RemoveAll(directory)
	if err := runDNGConverter(executable, source, directory, "linear.dng", true, "uncompressed", 0); err != nil {
		if directError != nil {
			return nil, fmt.Errorf("direct Linear DNG development failed (%v); Adobe fallback failed: %w", directError, err)
		}
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(directory, "linear.dng"))
	if err != nil {
		return nil, err
	}
	return developLinearDNG(data, cameraProfile)
}

// A Linear DNG already contains demosaiced pixels. When the caller has not
// selected an external backend, decode supported DNG compression directly.
// Camera RAW and unsupported DNG variants continue through Adobe DNG Converter.
func developLinearDNGFile(source, cameraProfile string) ([]byte, bool, error) {
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxDevelopedRAWBytes {
		return nil, false, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, false, nil
	}
	fields, err := findLinearDNG(data)
	if err != nil || fields.integer(262, 0) != 34892 || fields.err != nil {
		return nil, false, nil
	}
	compression := fields.integer(259, 1)
	if compression != 1 && compression != 7 && compression != 8 {
		return nil, false, nil
	}
	developed, err := developLinearDNG(data, cameraProfile)
	return developed, true, err
}

func developLinearDNG(data []byte, cameraProfile string) ([]byte, error) {
	img, err := decodeLinearDNGWithProfileAndDirs(data, cameraProfile, cameraProfileDirectories())
	if err != nil {
		return nil, fmt.Errorf("decode Linear DNG pixels: %w", err)
	}
	fields, err := findLinearDNG(data)
	if err != nil {
		return nil, err
	}
	orientation := fields.integer(274, 1)
	if fields.err != nil || orientation < 1 || orientation > 8 {
		return nil, fmt.Errorf("invalid Linear DNG orientation")
	}
	return encodeDevelopedDNGTIFF(img, uint16(orientation))
}

// Keep the IFD and Orientation before the pixel strip. The metadata library
// cannot reliably read an IFD relocated beyond its initial read window.
// A single uncompressed RGB16 strip also avoids discarding 16-bit precision.
func encodeDevelopedDNGTIFF(img *image.NRGBA64, orientation uint16) ([]byte, error) {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	const entryCount = 11
	const bitsOffset = 8 + 2 + entryCount*12 + 4
	const pixelOffset = bitsOffset + 6
	if width < 1 || height < 1 || width > 32768 || height > 32768 || orientation < 1 || orientation > 8 || uint64(width)*uint64(height)*6+pixelOffset > maxDevelopedRAWBytes {
		return nil, fmt.Errorf("invalid developed TIFF dimensions/orientation")
	}
	pixelBytes := width * height * 6
	data := make([]byte, pixelOffset+pixelBytes)
	order := binary.BigEndian
	copy(data, "MM")
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], entryCount)
	entries := []struct {
		tag, kind    uint16
		count, value uint32
	}{
		{256, 4, 1, uint32(width)}, {257, 4, 1, uint32(height)}, {258, 3, 3, bitsOffset},
		{259, 3, 1, 1}, {262, 3, 1, 2}, {273, 4, 1, pixelOffset}, {274, 3, 1, uint32(orientation)},
		{277, 3, 1, 3}, {278, 4, 1, uint32(height)}, {279, 4, 1, uint32(pixelBytes)}, {284, 3, 1, 1},
	}
	for index, entry := range entries {
		start := 10 + index*12
		order.PutUint16(data[start:], entry.tag)
		order.PutUint16(data[start+2:], entry.kind)
		order.PutUint32(data[start+4:], entry.count)
		if entry.kind == 3 && entry.count == 1 {
			order.PutUint16(data[start+8:], uint16(entry.value))
		} else {
			order.PutUint32(data[start+8:], entry.value)
		}
	}
	for index := 0; index < 3; index++ {
		order.PutUint16(data[bitsOffset+index*2:], 16)
	}
	output := data[pixelOffset:]
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			position := img.PixOffset(x, y)
			copy(output[:6], img.Pix[position:position+6])
			output = output[6:]
		}
	}
	return data, nil
}

func convertRawWithDNGConverter(executable, source, destination, compression string, longEdge int) error {
	directory, err := os.MkdirTemp(filepath.Dir(destination), ".lrtl-adobe-dng-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := runDNGConverter(executable, source, directory, "converted.dng", false, compression, longEdge); err != nil {
		return err
	}
	if err := validateConvertedDNG(filepath.Join(directory, "converted.dng")); err != nil {
		return err
	}
	if longEdge > 0 {
		data, err := os.ReadFile(filepath.Join(directory, "converted.dng"))
		if err != nil {
			return err
		}
		fields, err := findLinearDNG(data)
		if err != nil {
			return fmt.Errorf("check resized DNG dimensions: %w", err)
		}
		width, height := fields.integer(256, 0), fields.integer(257, 0)
		if fields.err != nil || width < 1 || height < 1 || max(width, height) > longEdge {
			return fmt.Errorf("Adobe DNG Converter output did not meet the requested long-edge limit %d", longEdge)
		}
	}
	// Publish using the import workflow's existing no-overwrite checks.
	if err := os.Rename(filepath.Join(directory, "converted.dng"), destination); err != nil {
		return fmt.Errorf("publish Adobe DNG output: %w", err)
	}
	return nil
}

func validateConvertedDNG(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	var header [8]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return fmt.Errorf("truncated converted DNG: %w", err)
	}
	var order binary.ByteOrder
	if string(header[:2]) == "II" {
		order = binary.LittleEndian
	} else if string(header[:2]) == "MM" {
		order = binary.BigEndian
	} else {
		return fmt.Errorf("converted DNG has no TIFF byte order")
	}
	if order.Uint16(header[2:4]) != 42 {
		return fmt.Errorf("converted DNG has no classic TIFF header")
	}
	reader := &tiffXMPReader{file: file, size: uint64(info.Size()), order: order}
	entries, _, err := reader.readDirectory(uint64(order.Uint32(header[4:8])))
	if err != nil {
		return fmt.Errorf("invalid converted DNG directory: %w", err)
	}
	for _, entry := range entries {
		if entry.tag == 50706 && entry.typeID == 1 && entry.count == 4 && entry.value[0] == 1 {
			return nil
		}
	}
	return fmt.Errorf("converted DNG has no valid DNGVersion")
}
