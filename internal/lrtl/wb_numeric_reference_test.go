package lrtl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are analytical regression cases for the automatic worker's null
// reference branch, not full-application Java oracle results.
func TestWBNumericMissingReference(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ratings []string
		missing int
	}{
		{"first-key", []string{"4", "0", "4"}, 0},
		{"interior-reference", []string{"0", "4", "4"}, 1},
		{"crop-only-falls-back-to-first", []string{"5", "0", "5"}, 0},
		{"mixed-crop-and-develop", []string{"5", "4", "4"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := cropFixture(t, 3)
			for i, frame := range frames {
				properties := map[string]string{"xmp:Rating": tc.ratings[i], "crs:WhiteBalance": "Custom", "crs:Exposure2012": "1", "crs:VendorUnknown": "retain"}
				if i != tc.missing {
					for _, name := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
						properties["crs:"+name] = "10"
					}
				}
				writeTestXMP(t, frame.Image, properties)
			}
			plan, packets := interpolationPackets(t, frames, 50)
			removalCount := 0
			for _, change := range plan.Changes {
				removalCount += len(change.ScalarRemovals)
			}
			if removalCount != 8 {
				t.Fatalf("removals=%d want 8", removalCount)
			}
			for i, packet := range packets {
				for _, name := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
					if _, found, err := packet.GetScalar("crs:" + name); err != nil || found {
						t.Fatalf("frame %d retained %s: %v", i, name, err)
					}
				}
				assertTransitionScalar(t, packet, "Exposure2012", 1)
				if value, _, _ := packet.GetScalar("crs:VendorUnknown"); value != "retain" {
					t.Fatal("lost unrelated metadata")
				}
			}
		})
	}
}

func TestWBNumericReferenceZeroAndPerColumnAbsence(t *testing.T) {
	frames := cropFixture(t, 3)
	for i, frame := range frames {
		properties := map[string]string{"xmp:Rating": "4", "crs:WhiteBalance": "Custom", "crs:Tint": "0"}
		if i != 0 {
			properties["crs:Temperature"] = "5000"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for _, packet := range packets {
		assertTransitionScalar(t, packet, "Tint", 0)
		for _, name := range []string{"Temperature", "IncrementalTemperature", "IncrementalTint"} {
			if _, found, _ := packet.GetScalar("crs:" + name); found {
				t.Fatalf("invented or retained %s", name)
			}
		}
	}
}

func TestWBNumericReferenceClearKeepsPresentReferenceAndSingleton(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    []map[string]float64
		reference int
	}{
		{"present-interior-reference", []map[string]float64{{"Temperature": 3000}, {"Temperature": 5000}, {"Temperature": 7000}}, 1},
		{"zero-is-present", []map[string]float64{{"Tint": 0}, {"Tint": 10}}, 0},
		{"all-absent", []map[string]float64{nil, nil}, 0},
		{"singleton", []map[string]float64{{"Temperature": 5000}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := numericTransitionInputs{Values: tc.values, Missing: make([]map[string]float64, len(tc.values))}
			before := make([]map[string]float64, len(tc.values))
			for i, values := range tc.values {
				before[i] = make(map[string]float64)
				for name, value := range values {
					before[i][name] = value
				}
			}
			for i, removals := range inputs.clearMissingWBReferences(tc.reference) {
				if len(removals) != 0 || len(inputs.Values[i]) != len(before[i]) {
					t.Fatalf("unexpected clear: frame %d values=%v removals=%v", i, inputs.Values[i], removals)
				}
				for name, value := range before[i] {
					if got, found := inputs.Values[i][name]; !found || got != value {
						t.Fatalf("changed %s: %v", name, inputs.Values[i])
					}
				}
			}
		})
	}
}

func TestWBNumericReferenceClearCLIBackupDryRunAndNoop(t *testing.T) {
	frames := cropFixture(t, 3)
	originals := make([][]byte, len(frames))
	for i, frame := range frames {
		properties := map[string]string{"xmp:Rating": "4", "crs:WhiteBalance": "Custom"}
		if i > 0 {
			properties["crs:Temperature"] = "5000"
		}
		writeTestXMP(t, frame.Image, properties)
		var err error
		originals[i], err = os.ReadFile(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", stderr.String())
	}
	if !strings.Contains(out.String(), "crs:Temperature") || !strings.Contains(out.String(), "(remove)") {
		t.Fatalf("preview hid removal: %s", out.String())
	}
	for i, frame := range frames {
		data, err := os.ReadFile(frame.OutputSidecar())
		if err != nil || !bytes.Equal(data, originals[i]) {
			t.Fatal("preview modified sidecars")
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(backups) != 0 {
		t.Fatal("preview created backups")
	}
	out.Reset()
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write: %s", stderr.String())
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, _ := packet.GetScalar("crs:Temperature"); found {
			t.Fatal("retained missing-reference WB column")
		}
		if i > 0 {
			paths, err := filepath.Glob(frame.OutputSidecar() + "*.bak")
			if err != nil || len(paths) != 1 {
				t.Fatalf("backups=%v err=%v", paths, err)
			}
			backup, err := os.ReadFile(paths[0])
			if err != nil || !bytes.Equal(backup, originals[i]) {
				t.Fatal("backup does not preserve input")
			}
		}
	}
	plan, err := buildPlan(frames)
	if err != nil || plan.PropertyCount() != 0 {
		t.Fatalf("second plan is not a no-op: %+v err=%v", plan, err)
	}
}

func TestWBNumericMalformedDonorRejectedBeforeReferenceClear(t *testing.T) {
	frames := cropFixture(t, 3)
	for i, frame := range frames {
		properties := map[string]string{"xmp:Rating": "4", "crs:WhiteBalance": "Custom"}
		if i > 0 {
			properties["crs:Temperature"] = "NaN"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", filepath.Dir(frames[0].Image), "--write"}, &out, &stderr); code == 0 {
		t.Fatal("accepted invalid WB number")
	}
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if value, found, _ := packet.GetScalar("crs:Temperature"); !found || value != "NaN" {
		t.Fatal("failed preflight modified input")
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
	if len(backups) != 0 {
		t.Fatal("failed preflight created backups")
	}
}
