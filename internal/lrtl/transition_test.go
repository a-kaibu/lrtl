package lrtl

import "testing"

func TestTransitionClampsExposureForStandardAndHDRSequences(t *testing.T) {
	for _, test := range []struct {
		name   string
		merged string
		want   []string
	}{
		{"standard", "false", []string{"-5", "0", "5"}},
		{"HDR", "true", []string{"-10", "0", "10"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 5)
			writeTestXMP(t, frames[0].Image, map[string]string{
				"xmp:Rating": "4", "crs:Exposure2012": "-40", "crs:IsMergedHDR": test.merged,
			})
			writeTestXMP(t, frames[4].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "40"})
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := buildPlanWithSmoothness(frames, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Changes) != 3 {
				t.Fatalf("changes=%d want 3", len(plan.Changes))
			}
			for index, change := range plan.Changes {
				if change.Frame.Index != index+1 || change.Values["Exposure2012"] != test.want[index] {
					t.Errorf("frame=%d Exposure2012=%s want frame=%d value=%s", change.Frame.Index, change.Values["Exposure2012"], index+1, test.want[index])
				}
			}
		})
	}
}

func TestTransitionRoundsRegisteredIntegerControls(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{"xmp:Rating": "4", "crs:Sharpness": "0"})
	writeTestXMP(t, frames[2].Image, map[string]string{"xmp:Rating": "4", "crs:Sharpness": "3"})
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	// The midpoint is 1.5, which must be stored as the integer 2.
	if len(plan.Changes) != 1 || plan.Changes[0].Values["Sharpness"] != "2" {
		t.Fatalf("integer midpoint was not rounded: %#v", plan.Changes)
	}
}

func TestTransitionSkipsUnregisteredLegacyExposure(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure": "-1"})
	writeTestXMP(t, frames[1].Image, map[string]string{"crs:Exposure": "3"})
	writeTestXMP(t, frames[2].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure": "1"})
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.KeyframeIndices) != 2 || len(plan.Changes) != 0 {
		t.Fatalf("legacy Exposure must remain untouched: %#v", plan)
	}
}
