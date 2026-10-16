package lrtl

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type measuredHolyGrailExifCase struct {
	TimeNumerator, TimeDenominator         uint32
	ApertureNumerator, ApertureDenominator uint32
	ISO                                    uint16
	ShutterLabel, ApertureLabel            string
	ExpectedTime, ExpectedAperture         float64
	ExpectedISO                            float64
}

func measuredHolyGrailExifCases(t *testing.T) []measuredHolyGrailExifCase {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-exif.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []measuredHolyGrailExifCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 94 {
		t.Fatalf("unexpected EXIF fixture size: %d", len(fixture.Cases))
	}
	return fixture.Cases
}

func holyGrailExifJPEG(t *testing.T, c measuredHolyGrailExifCase) []byte {
	t.Helper()
	data := holyGrailCameraJPEGWithISO(t, c.ISO)
	// The fixture's APP1 TIFF starts at byte 12; offsets 68..83 hold
	// ExposureTime and FNumber rationals. Retain independently encoded pixels.
	binary.LittleEndian.PutUint32(data[12+68:], c.TimeNumerator)
	binary.LittleEndian.PutUint32(data[12+72:], c.TimeDenominator)
	binary.LittleEndian.PutUint32(data[12+76:], c.ApertureNumerator)
	binary.LittleEndian.PutUint32(data[12+80:], c.ApertureDenominator)
	return data
}

func TestHolyGrailExifAgainstInstalledLRT755(t *testing.T) {
	for _, c := range measuredHolyGrailExifCases(t) {
		t.Run(c.ShutterLabel+"_f"+c.ApertureLabel, func(t *testing.T) {
			seconds := float64(c.TimeNumerator) / float64(c.TimeDenominator)
			fNumber := float64(c.ApertureNumerator) / float64(c.ApertureDenominator)
			if got := holyGrailLRTShutterLabel(seconds); got != c.ShutterLabel {
				t.Fatalf("shutter label=%s want %s", got, c.ShutterLabel)
			}
			if got := holyGrailLRTApertureLabel(fNumber); got != c.ApertureLabel {
				t.Fatalf("aperture label=%s want %s", got, c.ApertureLabel)
			}
			frame := Frame{Image: filepath.Join(t.TempDir(), "frame.jpg")}
			if err := os.WriteFile(frame.Image, holyGrailExifJPEG(t, c), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readHolyGrailLRTCameraExposure(frame, false)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(got.ExposureTime) != math.Float64bits(c.ExpectedTime) || math.Float64bits(got.FNumber) != math.Float64bits(c.ExpectedAperture) || holyGrailLRTISO(got.ISO) != c.ExpectedISO {
				t.Fatalf("camera=%+v want time=%g f=%g ISO=%g", got, c.ExpectedTime, c.ExpectedAperture, c.ExpectedISO)
			}
			physical, err := readCameraExposure(frame, false)
			if err != nil || physical.ExposureTime != float64(float32(c.TimeNumerator)/float32(c.TimeDenominator)) || physical.FNumber != float64(float32(c.ApertureNumerator)/float32(c.ApertureDenominator)) || physical.ISO != float64(c.ISO) {
				t.Fatalf("physical EXIF changed: %+v err=%v", physical, err)
			}
		})
	}
}

func TestHolyGrailChangingCameraControllerCLIThroughVideo(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-camera-controller.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []measuredHolyGrailControllerCase
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 {
		t.Fatal("incomplete changing-camera controller fixture")
	}
	for n, c := range fixture.Cases {
		if len(c.CameraShutter) != len(c.Input) || len(c.CameraAperture) != len(c.Input) {
			t.Fatal("missing camera labels")
		}
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			runHolyGrailCLIAndVideoWithCachedMetadata(t, c, false, false)
		})
	}
	// The same native values are reachable from sidecar-only camera metadata.
	t.Run("cached", func(t *testing.T) { runHolyGrailCLIAndVideoWithCachedMetadata(t, fixture.Cases[0], false, true) })
}

func TestHolyGrailPreciseTIFFCameraRationals(t *testing.T) {
	for _, c := range measuredHolyGrailExifCases(t) {
		if c.TimeNumerator != 235 && c.ApertureNumerator != 235 {
			continue
		}
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			t.Run(fmt.Sprintf("%d_%d_%s", c.TimeNumerator, c.ApertureNumerator, order), func(t *testing.T) {
				data := make([]byte, 84)
				copy(data, "II")
				if order == binary.BigEndian {
					copy(data, "MM")
				}
				order.PutUint16(data[2:], 42)
				order.PutUint32(data[4:], 8)
				entry := func(offset int, id, kind uint16, value uint32) {
					order.PutUint16(data[offset:], id)
					order.PutUint16(data[offset+2:], kind)
					order.PutUint32(data[offset+4:], 1)
					if kind == 3 {
						order.PutUint16(data[offset+8:], uint16(value))
					} else {
						order.PutUint32(data[offset+8:], value)
					}
				}
				order.PutUint16(data[8:], 1)
				entry(10, 0x8769, 4, 26)
				order.PutUint16(data[26:], 3)
				entry(28, 0x829a, 5, 68)
				entry(40, 0x829d, 5, 76)
				entry(52, 0x8827, 3, uint32(c.ISO))
				for i, v := range []uint32{c.TimeNumerator, c.TimeDenominator, c.ApertureNumerator, c.ApertureDenominator} {
					order.PutUint32(data[68+i*4:], v)
				}
				frame := Frame{Image: filepath.Join(t.TempDir(), "frame.tif")}
				if err := os.WriteFile(frame.Image, data, 0600); err != nil {
					t.Fatal(err)
				}
				camera, err := readHolyGrailLRTCameraExposure(frame, false)
				if err != nil || camera.ExposureTime != c.ExpectedTime || camera.FNumber != c.ExpectedAperture {
					t.Fatalf("camera=%+v err=%v", camera, err)
				}
			})
		}
	}
}
