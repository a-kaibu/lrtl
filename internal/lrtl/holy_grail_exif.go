package lrtl

import (
	"bufio"
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
	decoder.SetCustomTagParser(func(parser exif2.TagParser, field exif2.Tag) error {
		if field.Ifd != ifds.ExifIFD || !field.IsType(tag.TypeRational) || (field.ID != exififd.ExposureTime && field.ID != exififd.FNumber) {
			return nil
		}
		value := parser.ParseRationalU(field)
		if value[1] == 0 {
			return nil
		}
		precise := float64(value[0]) / float64(value[1])
		if field.ID == exififd.ExposureTime {
			camera.ExposureTime = precise
		} else {
			camera.FNumber = precise
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
	return camera, err
}
