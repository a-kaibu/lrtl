package lrtl

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/evanoberholster/imagemeta"
)

// The camera subset of the native CSV reader. Capture dates are intentionally
// separate: their save conditions and clock-dependent effects are not covered
// by the camera-cache observations.
type nativeCameraMetadata struct {
	Width, Height                     int32
	Orientation                       int32
	Aperture, Shutter, ISO, Mode, HDR string
}

func nativeCameraInteger(text string) (int32, bool) {
	if text != selectedJavaTrim(text) {
		return 0, false
	}
	if strings.HasPrefix(text, "+") {
		rest := text[1:]
		if rest != selectedJavaTrim(rest) || strings.HasPrefix(rest, "+") || strings.HasPrefix(rest, "-") {
			return 0, false
		}
	}
	return selectedJavaInteger(text)
}

func parseNativeCameraFields(fields []string) (nativeCameraMetadata, error) {
	state := nativeCameraMetadata{Orientation: 1, Aperture: "n/a", Shutter: "n/a", ISO: "n/a", Mode: "-", HDR: "false"}
	if len(fields) < 2 {
		return state, fmt.Errorf("missing camera metadata dimensions")
	}
	dimensions := strings.FieldsFunc(fields[1], func(r rune) bool { return r == 'x' })
	if len(dimensions) == 2 {
		var valid bool
		state.Width, valid = nativeCameraInteger(dimensions[0])
		if !valid {
			return state, fmt.Errorf("invalid metadata width %q", dimensions[0])
		}
		state.Height, valid = nativeCameraInteger(dimensions[1])
		if !valid {
			return state, fmt.Errorf("invalid metadata height %q", dimensions[1])
		}
	}
	if len(fields) > 3 && fields[3] != "-" {
		if selectedDoublePattern.MatchString(selectedJavaTrim(fields[3])) {
			state.Aperture = fields[3]
		}
		if state.Aperture == "" {
			state.Aperture = "n/a"
		}
	}
	if len(fields) > 4 && fields[4] != "-" && fields[4] != "" {
		state.Shutter = fields[4]
	}
	if len(fields) > 5 && fields[5] != "-" {
		state.ISO = fields[5]
	}
	if len(fields) > 7 {
		state.Mode = fields[7]
		if state.Mode == "" {
			state.Mode = "n/a"
		}
	}
	if len(fields) > 10 {
		if value, valid := nativeCameraInteger(fields[10]); valid {
			state.Orientation = value
		}
	}
	if len(fields) > 13 && strings.EqualFold(strings.TrimSpace(fields[13]), "true") {
		state.HDR = "true"
	}
	return state, nil
}

func applyNativeCameraMetadata(packet *XMPPacket, state nativeCameraMetadata) error {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return err
	}
	values := map[string]string{"Aperture": state.Aperture, "ShutterSpeed": state.Shutter, "Iso": state.ISO, "ShootingMode": state.Mode, "IsMergedHDR": state.HDR}
	if state.Width > 0 {
		values["Width"] = strconv.FormatInt(int64(state.Width), 10)
	}
	if state.Height > 0 {
		values["Height"] = strconv.FormatInt(int64(state.Height), 10)
	}
	for name, value := range values {
		if value == "" {
			continue
		}
		if err := setSaveStateProperty(after, pasteName{namespacePrefixes["lrt"], name}, "lrt", value); err != nil {
			return err
		}
	}
	if err := setSaveStateProperty(after, pasteName{namespacePrefixes["tiff"], "Orientation"}, "tiff", strconv.FormatInt(int64(state.Orientation), 10)); err != nil {
		return err
	}
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}

// Read available image metadata without rendering pixels. Existing LRT camera
// labels supply missing image tags (including manual lens values). Unknown image
// formats retain their XMP state. RAW dimensions are not taken from imagemeta's
// uint16 fields, which would truncate large dimensions.
func readNativeCameraMetadata(frame Frame, packet *XMPPacket) (*nativeCameraMetadata, error) {
	file, err := os.Open(frame.Image)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config, format, configErr := image.DecodeConfig(file)
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	metadata, metadataErr := imagemeta.Decode(file)
	width, height, hasTIFFSize, err := nativeTIFFImageSize(frame.Image)
	if err != nil {
		return nil, err
	}
	if configErr != nil && metadataErr != nil && !hasTIFFSize {
		return nil, nil
	}
	fields := make([]string, 16)
	for i := range fields {
		fields[i] = "-"
	}
	fields[0] = frame.Image
	if hasTIFFSize {
		fields[1] = fmt.Sprintf("%dx%d", width, height)
	} else if configErr == nil && config.Width > 0 && config.Height > 0 {
		if config.Width > math.MaxInt32 || config.Height > math.MaxInt32 {
			return nil, fmt.Errorf("image dimensions exceed native metadata range")
		}
		fields[1] = fmt.Sprintf("%dx%d", config.Width, config.Height)
	}
	for index, name := range map[int]string{3: "Aperture", 4: "ShutterSpeed", 5: "Iso", 7: "ShootingMode", 13: "IsMergedHDR"} {
		value, present, err := packet.GetRootScalar("lrt:" + name)
		if err != nil {
			return nil, err
		}
		if present {
			fields[index] = value
		}
	}
	if configErr == nil && format == "png" {
		orientation, present, err := readNativePNGOrientation(file)
		if err != nil {
			return nil, err
		}
		if present {
			fields[10] = strconv.FormatUint(uint64(orientation), 10)
		}
	}
	if metadataErr == nil {
		if metadata.Orientation != 0 {
			fields[10] = strconv.FormatUint(uint64(metadata.Orientation), 10)
		}
		if uint16(metadata.ExposureMode) == 1 || uint16(metadata.ExposureProgram) == 1 {
			fields[7] = "Manual"
		}
		camera, err := readHolyGrailLRTExposureMetadata(frame)
		if err == nil {
			if camera.FNumber > 0 && !math.IsInf(camera.FNumber, 0) && !math.IsNaN(camera.FNumber) {
				fields[3] = holyGrailLRTApertureLabel(camera.FNumber)
			}
			if camera.ExposureTime > 0 && !math.IsInf(camera.ExposureTime, 0) && !math.IsNaN(camera.ExposureTime) {
				fields[4] = holyGrailLRTShutterLabel(camera.ExposureTime)
			}
			if camera.ISO > 0 && !math.IsInf(camera.ISO, 0) && !math.IsNaN(camera.ISO) {
				fields[5] = strconv.FormatFloat(camera.ISO, 'f', -1, 64)
			}
		}
	}
	state, err := parseNativeCameraFields(fields)
	return &state, err
}

func readNativePNGOrientation(file *os.File) (uint16, bool, error) {
	data, found, err := readNativePNGExif(file)
	if err != nil || !found {
		return 0, found, err
	}
	orientation, err := pngTIFFOrientation(data)
	return orientation, true, err
}

func readNativePNGExif(file *os.File) ([]byte, bool, error) {
	if _, err := file.Seek(8, io.SeekStart); err != nil {
		return nil, false, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	for count := 0; count < 4096; count++ {
		var header [8]byte
		if _, err := io.ReadFull(file, header[:]); err != nil {
			return nil, false, err
		}
		length := int64(binary.BigEndian.Uint32(header[:4]))
		position, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, false, err
		}
		if length+4 > info.Size()-position {
			return nil, false, fmt.Errorf("truncated PNG metadata chunk")
		}
		kind := string(header[4:])
		if kind == "IEND" {
			return nil, false, nil
		}
		if kind != "eXIf" {
			if _, err := file.Seek(length+4, io.SeekCurrent); err != nil {
				return nil, false, err
			}
			continue
		}
		if length > 1<<20 {
			return nil, false, fmt.Errorf("PNG EXIF exceeds metadata size limit")
		}
		data := make([]byte, int(length))
		if _, err := io.ReadFull(file, data); err != nil {
			return nil, false, err
		}
		var checksum [4]byte
		if _, err := io.ReadFull(file, checksum[:]); err != nil {
			return nil, false, err
		}
		hash := crc32.NewIEEE()
		hash.Write(header[4:])
		hash.Write(data)
		if hash.Sum32() != binary.BigEndian.Uint32(checksum[:]) {
			return nil, false, fmt.Errorf("invalid PNG eXIf checksum")
		}
		return data, true, nil
	}
	return nil, false, fmt.Errorf("too many PNG metadata chunks")
}
