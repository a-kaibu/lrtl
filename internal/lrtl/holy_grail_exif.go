package lrtl

import (
	"bufio"
	"math"
	"os"

	"github.com/evanoberholster/imagemeta/exif2"
	"github.com/evanoberholster/imagemeta/exif2/ifds"
	"github.com/evanoberholster/imagemeta/exif2/ifds/exififd"
	"github.com/evanoberholster/imagemeta/exif2/tag"
	"github.com/evanoberholster/imagemeta/imagetype"
	"github.com/evanoberholster/imagemeta/isobmff"
	"github.com/evanoberholster/imagemeta/jpeg"
	"github.com/evanoberholster/imagemeta/tiff"
)

// Retain the original rational precision before formatting HG camera labels.
// imagemeta's public ExposureTime/FNumber fields use float32, which can move
// decimal rounding boundaries (for example, 2.35 becomes 2.3499999...).
func readHolyGrailLRTExposureMetadata(frame Frame) (CameraExposure, error) {
	camera, err := readCameraExposureMetadata(frame)
	if err != nil {
		return CameraExposure{}, err
	}
	file, err := os.Open(frame.Image)
	if err != nil {
		return CameraExposure{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	imageType, err := imagetype.ScanBuf(reader)
	if err != nil {
		return CameraExposure{}, err
	}
	decoder := exif2.NewIfdReader(exif2.Logger)
	defer decoder.Close()
	var exposureTime, fNumber, apexTime, apexAperture float64
	var hasExposureTime, hasFNumber, hasAPEXTime, hasAPEXAperture bool
	decoder.SetCustomTagParser(func(parser exif2.TagParser, field exif2.Tag) error {
		if field.Ifd != ifds.ExifIFD || (!field.IsType(tag.TypeRational) && !field.IsType(tag.TypeSignedRational)) {
			return nil
		}
		switch field.ID {
		case exififd.ExposureTime, exififd.FNumber, exififd.ShutterSpeedValue, exififd.ApertureValue:
		default:
			return nil
		}
		value := parser.ParseRationalU(field)
		if value[1] == 0 {
			return nil
		}
		precise := float64(value[0]) / float64(value[1])
		// ExifTool interprets ShutterSpeedValue as signed even for cameras
		// incorrectly declaring it unsigned. Other tags use their field type.
		if field.IsType(tag.TypeSignedRational) || field.ID == exififd.ShutterSpeedValue {
			precise = float64(int32(value[0])) / float64(int32(value[1]))
		}
		switch field.ID {
		case exififd.ExposureTime:
			exposureTime, hasExposureTime = precise, true
		case exififd.FNumber:
			fNumber, hasFNumber = precise, true
		case exififd.ShutterSpeedValue:
			hasAPEXTime = true
			if math.Abs(precise) < 100 {
				apexTime = math.Exp2(-precise)
			}
		case exififd.ApertureValue:
			hasAPEXAperture = true
			// The composite Aperture tag treats a raw zero APEX value as
			// absent, even though its mathematical f-number would be one.
			if precise != 0 {
				apexAperture = math.Exp2(precise / 2)
			}
		}
		return nil
	})
	switch imageType {
	case imagetype.ImageJPEG:
		err = jpeg.ScanJPEG(reader, decoder.DecodeJPEGIfd, nil)
	case imagetype.ImageTiff, imagetype.ImageDNG, imagetype.ImageCR2, imagetype.ImagePanaRAW, imagetype.ImageHEIF:
		header, headerErr := tiff.ScanTiffHeader(reader, imageType)
		if headerErr != nil {
			return CameraExposure{}, headerErr
		}
		err = decoder.DecodeTiff(reader, header)
	case imagetype.ImageCR3, imagetype.ImageAVIF:
		boxes := isobmff.NewReader(reader)
		defer boxes.Close()
		boxes.ExifReader = decoder.DecodeIfd
		if err = boxes.ReadFTYP(); err == nil {
			err = boxes.ReadMetadata()
		}
	}
	if hasExposureTime {
		camera.ExposureTime = exposureTime
	} else if hasAPEXTime {
		camera.ExposureTime = apexTime
	}
	if hasFNumber && fNumber != 0 {
		camera.FNumber = fNumber
	} else if hasAPEXAperture {
		camera.FNumber = apexAperture
	} else if hasFNumber {
		camera.FNumber = fNumber
	}
	return camera, err
}
