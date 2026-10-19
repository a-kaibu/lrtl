package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTransitionMaskFormatAgainstInstalled755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-mask-format-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Case, Detected, Effective int }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 6 {
		t.Fatal("expected six native format observations")
	}
	properties := [][]string{nil, {"GradientBasedCorrections"}, {"CircularGradientBasedCorrections"}, {"PaintBasedCorrections"}, {"MaskGroupBasedCorrections"}, {"GradientBasedCorrections", "MaskGroupBasedCorrections"}}
	for i, tc := range cases {
		if tc.Case != i {
			t.Fatal("unexpected fixture order")
		}
		packet := EmptyXMPPacket()
		for _, property := range properties[i] {
			if _, _, err := ensurePacketCRSArray(packet, property); err != nil {
				t.Fatal(err)
			}
		}
		want := "modern"
		if tc.Effective == 1 {
			want = "legacy"
		}
		got, err := transitionMaskFormat([]*XMPPacket{packet}, "auto")
		if err != nil || got != want {
			t.Fatalf("case %d: %s vs native %s: %v", i, got, want, err)
		}
		// Later masks do not change the first-image decision.
		later := EmptyXMPPacket()
		if _, _, err := ensurePacketCRSArray(later, "MaskGroupBasedCorrections"); err != nil {
			t.Fatal(err)
		}
		if got, err := transitionMaskFormat([]*XMPPacket{packet, later}, "auto"); err != nil || got != want {
			t.Fatalf("later frame changed decision: %s %v", got, err)
		}
		for _, format := range []string{"legacy", "modern"} {
			if got, err := transitionMaskFormat([]*XMPPacket{packet}, format); err != nil || got != format {
				t.Fatalf("explicit format %s: %s %v", format, got, err)
			}
		}
	}
}

func mixedMaskAnglePacket(t *testing.T, tc maskAngleObservation, form int) *XMPPacket {
	t.Helper()
	legacy, modern := tc, tc
	legacy.Mode, modern.Mode = 1, 2
	packet := maskAnglePacket(t, legacy, form)
	if _, err := packet.CopyRootPropertyFrom(maskAnglePacket(t, modern, form), "crs:MaskGroupBasedCorrections"); err != nil {
		t.Fatal(err)
	}
	return packet
}

func assertMixedMaskAngleObservation(t *testing.T, packet *XMPPacket, tc maskAngleObservation) {
	t.Helper()
	assertMaskAngleObservation(t, packet, tc)
	other := tc
	other.Mode = 3 - tc.Mode
	assertMaskAngleObservation(t, packet, other)
}

func TestMixedMaskAngleNativeDispatch(t *testing.T) {
	for n, tc := range maskAngleObservations(t) {
		for form := 0; form < 3; form++ {
			t.Run(fmt.Sprintf("%d/form%d", n, form), func(t *testing.T) {
				packet := mixedMaskAnglePacket(t, tc, form)
				format := "legacy"
				if tc.Mode == 2 {
					format = "auto"
				} // MaskGroup wins mixed auto detection.
				changes, err := planMaskTransitionsWithFormat([]*XMPPacket{packet}, []int{0}, 50, format)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range changes[0] {
					if err := applyMaskTransition(packet, change); err != nil {
						t.Fatal(err)
					}
				}
				data, err := packet.Serialize()
				if err != nil {
					t.Fatal(err)
				}
				saved, err := parseXMP(data)
				if err != nil {
					t.Fatal(err)
				}
				assertMixedMaskAngleObservation(t, saved, tc)
			})
		}
	}
}

func TestMixedMaskAngleCopySplineAndMissingColumns(t *testing.T) {
	for _, scenario := range []string{"copy", "spline", "missing"} {
		for _, format := range []string{"legacy", "modern"} {
			t.Run(scenario+"/"+format, func(t *testing.T) {
				packets := make([]*XMPPacket, 7)
				indexes := make([]map[maskTransitionKey]maskTransitionNode, len(packets))
				for i := range packets {
					value := 60. + float64(i)*10
					if scenario != "spline" {
						value = 90
					}
					tc := maskAngleObservation{Mode: 1, Input: &value}
					if scenario == "missing" && i == 3 {
						tc.Input = nil
					}
					packets[i] = mixedMaskAnglePacket(t, tc, i%3)
					var err error
					indexes[i], err = indexMaskTransitionNodes(packets[i])
					if err != nil {
						t.Fatal(err)
					}
				}
				keys := []int{0, 3, 6}
				changes, err := planMaskTransitionsWithFormat(packets, keys, 50, format)
				if err != nil {
					t.Fatal(err)
				}
				for i := range packets {
					for _, change := range changes[i] {
						if err := applyMaskTransition(packets[i], change); err != nil {
							t.Fatal(err)
						}
					}
					nodes, err := indexMaskTransitionNodes(packets[i])
					if err != nil {
						t.Fatal(err)
					}
					for _, key := range []maskTransitionKey{
						{collection: "CircularGradientBasedCorrections", correctionIndex: 1, maskIndex: 1},
						{collection: "MaskGroupBasedCorrections", correctionIndex: 8, maskIndex: 1},
					} {
						want := 90.
						if scenario == "spline" {
							want = 60. + float64(i)*10
							if i != 0 && i != 3 && i != 6 {
								// Independent dispatch control: sample the existing numeric
								// spline without any angle postprocessing.
								want = maskTransitionSeriesWithAngleMode(keys, indexes, key, len(packets), 50, nil, false)[i]["Angle"]
							}
						}
						active := format == "legacy" && key.collection == "CircularGradientBasedCorrections" || format == "modern" && key.collection == "MaskGroupBasedCorrections"
						if active {
							want -= 90
						}
						if got := nodes[key].values["Angle"]; formatNumber(got) != formatNumber(want) {
							t.Fatalf("frame %d %s: %g, want %g", i, key.collection, got, want)
						}
					}
				}
			})
		}
	}
}

func TestMixedMaskAngleCLIAndBatchFirstImageDispatch(t *testing.T) {
	for _, command := range []string{"interpolate", "batch-interpolate"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			for _, tc := range maskAngleObservations(t) {
				if tc.Input == nil || *tc.Input != 45.001 {
					continue
				}
				dir := batchInterpolationFixture(t, root, fmt.Sprint("mode", tc.Mode), false)
				frames, err := DiscoverSequence(dir)
				if err != nil {
					t.Fatal(err)
				}
				for i, frame := range frames {
					packet := mixedMaskAnglePacket(t, tc, i%3)
					if i == 0 && tc.Mode == 1 {
						if err := packet.RemoveProperty("crs:MaskGroupBasedCorrections"); err != nil {
							t.Fatal(err)
						}
						if _, _, err := ensurePacketCRSArray(packet, "GradientBasedCorrections"); err != nil {
							t.Fatal(err)
						}
						// Legacy first-image detection governs later mixed-layout packets.
					}
					if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
						t.Fatal(err)
					}
					persistExposurePacket(t, frame, packet)
				}
			}
			before := batchFileSnapshot(t, root)
			run := func(write bool) {
				args := []string{command, root}
				if write {
					args = append(args, "--write")
				}
				if command == "batch-interpolate" {
					markerCommand(t, append(args, "--workers", "2")...)
				} else {
					for _, name := range []string{"mode1", "mode2"} {
						args[1] = filepath.Join(root, name)
						markerCommand(t, args...)
					}
				}
			}
			run(false)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("dry run wrote files")
			}
			run(true)
			for _, tc := range maskAngleObservations(t) {
				if tc.Input == nil || *tc.Input != 45.001 {
					continue
				}
				frames, err := DiscoverSequence(filepath.Join(root, fmt.Sprint("mode", tc.Mode)))
				if err != nil {
					t.Fatal(err)
				}
				for i, frame := range frames {
					packet, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					if i == 0 && tc.Mode == 1 {
						assertMaskAngleObservation(t, packet, tc)
					} else {
						assertMixedMaskAngleObservation(t, packet, tc)
					}
					assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
					backups, err := filepath.Glob(frame.Sidecar + "*.bak")
					if err != nil || len(backups) != 1 {
						t.Fatalf("backups: %v %v", backups, err)
					}
					assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
				}
			}
			after := batchFileSnapshot(t, root)
			run(true)
			if !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
				t.Fatal("repeat rewrote stable angles")
			}
			for _, invalid := range []string{"v3", ""} {
				var out, stderr bytes.Buffer
				dir := root
				if command == "interpolate" {
					dir = filepath.Join(root, "mode1")
				}
				if Main([]string{command, dir, "--mask-format", invalid, "--write"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "mask format") {
					t.Fatalf("invalid format: %s", &stderr)
				}
				if !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
					t.Fatal("invalid format wrote files")
				}
			}
		})
	}
}

func TestTransitionMaskFormatEmbeddedFirstImage(t *testing.T) {
	dir := t.TempDir()
	var tc maskAngleObservation
	for _, candidate := range maskAngleObservations(t) {
		if candidate.Mode == 1 && candidate.Input != nil && *candidate.Input == 45.001 {
			tc = candidate
			break
		}
	}
	if tc.Input == nil {
		t.Fatal("missing native legacy case")
	}
	first := maskAnglePacket(t, tc, 0)
	if _, _, err := ensurePacketCRSArray(first, "GradientBasedCorrections"); err != nil {
		t.Fatal(err)
	}
	if err := first.SetScalar("xmp:Rating", "4"); err != nil {
		t.Fatal(err)
	}
	data, err := first.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(dir, "frame0.jpg")
	imageBytes := writeEmbeddedTransitionJPEG(t, imagePath, data)
	secondPath := filepath.Join(dir, "frame1.png")
	writeRenderFrame(t, secondPath, "png", 64, 32)
	second := mixedMaskAnglePacket(t, tc, 1)
	if err := second.SetScalar("xmp:Rating", "4"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(secondPath+".xmp", second, false); err != nil {
		t.Fatal(err)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(frames)
	if err != nil || plan.MaskFormat != "legacy" {
		t.Fatalf("embedded first-frame detection: %v %v", plan, err)
	}
	before := batchFileSnapshot(t, dir)
	markerCommand(t, "interpolate", dir, "--write")
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			assertMaskAngleObservation(t, packet, tc)
		} else {
			assertMixedMaskAngleObservation(t, packet, tc)
		}
	}
	assertPreviewEVFileBytes(t, imagePath, imageBytes)
	assertPreviewEVFileBytes(t, secondPath, before[secondPath])
	backups, err := filepath.Glob(secondPath + ".xmp*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("existing XMP backup: %v %v", backups, err)
	}
	assertPreviewEVFileBytes(t, backups[0], before[secondPath+".xmp"])
}
