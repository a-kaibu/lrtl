package lrtl

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func feedbackFrames(t *testing.T) []Frame {
	t.Helper()
	dir := t.TempDir()
	frames := make([]Frame, 3)
	for i, value := range []uint16{40000, 50000, 40000} {
		img := image.NewGray16(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetGray16(x, y, color.Gray16{Y: value})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		sidecar := writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "0", "crs:Contrast2012": "40"})
		// Filtered sequences retain their original, noncontiguous indices.
		frames[i] = Frame{Index: 10 + i*10, Image: path, Sidecar: sidecar}
	}
	return frames
}

func measureFeedbackPlan(t *testing.T, frames []Frame, changes []DeflickerChange) []float64 {
	t.Helper()
	packets := make([]*XMPPacket, len(frames))
	for i, frame := range frames {
		var err error
		packets[i], err = PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range changes {
		for i, frame := range frames {
			if change.Frame.Image == frame.Image {
				packets[i] = change.Packet
			}
		}
	}
	cache := newFrameInputCache(imageInputOptions{}, false)
	defer cache.close()
	values, err := measureDeflickerPackets(frames, image.Rectangle{}, imageInputOptions{ApplyXMPBasic: true}, packets, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestDeflickerPixelFeedbackThroughNonlinearXMP(t *testing.T) {
	frames := feedbackFrames(t)
	originals := map[string][]byte{}
	for _, frame := range frames {
		for _, path := range []string{frame.Image, frame.Sidecar} {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			originals[path] = data
		}
	}
	plans := make([]*deflickerSequencePlan, 2)
	for i, passes := range []int{1, 4} {
		var err error
		plans[i], err = planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{ApplyXMPBasic: true}, 1, passes, true, "gaussian")
		if err != nil {
			t.Fatal(err)
		}
		if len(plans[i].Measurements) != passes {
			t.Fatalf("measurements=%d want %d", len(plans[i].Measurements), passes)
		}
	}
	residual := func(values []float64) float64 { return math.Abs(math.Log2(values[1] / values[0])) }
	one := residual(measureFeedbackPlan(t, frames, plans[0].Changes))
	multi := residual(measureFeedbackPlan(t, frames, plans[1].Changes))
	// Evaluate the old scalar simulation through the same nonlinear renderer.
	initial := plans[1].Measurements[0]
	gains, err := deflickerGainsWithSmoother(initial, 1, 4, true, "gaussian")
	if err != nil {
		t.Fatal(err)
	}
	simulated := make([]DeflickerChange, len(frames))
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:Exposure2012", fmt.Sprint(math.Log2(gains[i]))); err != nil {
			t.Fatal(err)
		}
		simulated[i] = DeflickerChange{Frame: frame, Packet: packet}
	}
	old := residual(measureFeedbackPlan(t, frames, simulated))
	t.Logf("residual EV: one=%g feedback4=%g simulated4=%g", one, multi, old)
	if !(multi < one && multi < old && multi < 0.01) {
		t.Fatalf("feedback did not converge: one=%g multi=%g old=%g", one, multi, old)
	}
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("planning changed %s: %v", path, err)
		}
	}
	// Consolidated writes must remain absolute when the CLI applies them again.
	before := measureFeedbackPlan(t, frames, plans[1].Changes)
	if err := applyDeflickerChanges(plans[1].Changes); err != nil {
		t.Fatal(err)
	}
	after := measureFeedbackPlan(t, frames, plans[1].Changes)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("final writes applied correction twice")
		}
	}
}

func TestFrameLuminanceRetainsAdjacent16BitValues(t *testing.T) {
	values := make([]float64, 2)
	for i := range values {
		img := image.NewGray16(image.Rect(0, 0, 2, 2))
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				img.SetGray16(x, y, color.Gray16{Y: uint16(1000 + i)})
			}
		}
		var err error
		values[i], err = frameLuminance(img, img.Bounds())
		if err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(values[1]/values[0]-1.001) > 1e-12 {
		t.Fatalf("lost 16-bit precision: %v", values)
	}
}

func TestPixelFeedbackGainsRemeasuresClippedPixels(t *testing.T) {
	calls := 0
	gains, err := pixelFeedbackGains(3, 1, 4, true, "gaussian", func(gains []float64) ([]float64, error) {
		calls++
		return []float64{0.5, math.Min(0.1*gains[1], 0.2126), 0.5}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := deflickerGainsWithSmoother([]float64{0.5, 0.1, 0.5}, 1, 4, true, "gaussian")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || gains[1] <= scalar[1] {
		t.Fatalf("clipping was ignored: calls=%d gains=%v simulated=%v", calls, gains, scalar)
	}
	sentinel := errors.New("measurement failed")
	if _, err := pixelFeedbackGains(1, 1, 2, true, "gaussian", func([]float64) ([]float64, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("lost callback error: %v", err)
	}
	for _, values := range [][]float64{nil, {0}, {math.NaN()}, {math.Inf(1)}} {
		if _, err := pixelFeedbackGains(1, 1, 2, true, "gaussian", func([]float64) ([]float64, error) { return values, nil }); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
	for _, c := range []struct {
		count, radius, passes int
		smoother              string
	}{{0, 1, 1, "gaussian"}, {1, -1, 1, "gaussian"}, {1, 1, 0, "gaussian"}, {1, 1, 1, "unknown"}} {
		called := false
		if _, err := pixelFeedbackGains(c.count, c.radius, c.passes, true, c.smoother, func([]float64) ([]float64, error) { called = true; return []float64{1}, nil }); err == nil || called {
			t.Fatalf("invalid options were not rejected before measurement: %+v", c)
		}
	}
}

func TestDeflickerFeedbackFailureDoesNotWriteSidecars(t *testing.T) {
	frames := feedbackFrames(t)
	before, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frames[2].Image, []byte("corrupt image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{ApplyXMPBasic: true}, 1, 4, true, "gaussian"); err == nil {
		t.Fatal("accepted corrupt frame")
	}
	after, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed plan wrote a sidecar")
	}
	backups, err := filepath.Glob(frames[0].Sidecar + ".bak*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("unexpected intermediate backups: %v %v", backups, err)
	}
}

func TestDeflickerFeedbackCLICommitsOnlyFinalPlan(t *testing.T) {
	frames := feedbackFrames(t)
	plan, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{ApplyXMPBasic: true}, 1, 4, true, "gaussian")
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{}
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		originals[frame.Sidecar] = data
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"deflicker", filepath.Dir(frames[0].Image), "--constant", "--apply-xmp-basic", "--passes", "4", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("CLI %d: %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "passes: 4") {
		t.Fatalf("missing measured passes: %s", &out)
	}
	for _, change := range plan.Changes {
		got, err := os.ReadFile(change.Frame.Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		want, err := change.Packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("CLI differs from final plan: %s", change.Frame.Sidecar)
		}
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak*"))
	if err != nil || len(backups) != len(plan.Changes) {
		t.Fatalf("intermediate backups: %v %v", backups, err)
	}
	for _, path := range backups {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		matches := false
		for _, original := range originals {
			matches = matches || bytes.Equal(data, original)
		}
		if !matches {
			t.Fatalf("backup contains intermediate state: %s", path)
		}
	}
}

func TestDeflickerSlotAmountControlsPhysicalResponse(t *testing.T) {
	frames := []Frame{{Image: "a.png"}, {Image: "b.png"}, {Image: "c.png"}}
	target := xmpExposureTarget{Collection: "MaskGroupBasedCorrections", Index: 2}
	for _, amount := range []string{"0.5", "-0.5", "0"} {
		t.Run(amount, func(t *testing.T) {
			packets := make([]*XMPPacket, len(frames))
			for i := range packets {
				packets[i] = exposureTestPacket(t, "modern")
				el, err := xmpExposureTargetElement(packets[i], target)
				if err != nil {
					t.Fatal(err)
				}
				el.RemoveAttr("crs:CorrectionAmount")
				el.CreateAttr("crs:CorrectionAmount", amount)
			}
			changes, err := buildDeflickerChangesFromPackets(frames, []float64{0.25, 0.5, 0.25}, 1, 1, true, false, "gaussian", false, packets)
			if amount == "0" {
				if err == nil {
					t.Fatal("accepted nonresponsive target")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response, _ := scalarNumber(amount)
			if len(changes) != 1 || math.Abs(changes[0].Correction*response+0.7) > 1e-12 {
				t.Fatalf("wrong amount compensation: %+v", changes)
			}
		})
	}
	packet := exposureTestPacket(t, "modern")
	el, err := xmpExposureTargetElement(packet, target)
	if err != nil {
		t.Fatal(err)
	}
	el.RemoveAttr("crs:CorrectionActive")
	el.CreateAttr("crs:CorrectionActive", "false")
	if _, err := buildDeflickerChangesFromPackets(frames, []float64{0.25, 0.5, 0.25}, 1, 1, true, false, "gaussian", false, []*XMPPacket{packet, packet, packet}); err == nil {
		t.Fatal("accepted inactive target")
	}
}

func TestRenderDeflickerFeedbackWritesConvergedAVIPixels(t *testing.T) {
	frames := feedbackFrames(t)
	output := filepath.Join(filepath.Dir(frames[0].Image), "feedback.avi")
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 100, Deflicker: true, DeflickerPasses: 4, SmoothRadius: 1, Constant: true, ApplyXMPBasic: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var values []float64
	for {
		start := bytes.Index(data, []byte{0xff, 0xd8})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{0xff, 0xd9})
		if end < 0 {
			t.Fatal("truncated JPEG")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		v, err := frameLuminance(img, img.Bounds())
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, v)
		data = data[end+2:]
	}
	if len(values) != 3 || math.Abs(math.Log2(values[1]/values[0])) > 0.025 {
		t.Fatalf("rendered pixels did not converge: %v", values)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, _ := packet.GetScalar(deflickerCorrectionProperty); found {
			t.Fatal("render changed XMP")
		}
	}
}

func TestDeflickerFeedbackWithoutXMPReplacesPreviousCorrection(t *testing.T) {
	for _, smoother := range []string{"gaussian", "lowess"} {
		t.Run(smoother, func(t *testing.T) {
			frames := feedbackFrames(t)
			options := imageInputOptions{}
			first, err := planDeflickerSequence(frames, image.Rectangle{}, options, 1, 4, true, smoother)
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Changes) != 1 || len(first.Measurements) != 4 {
				t.Fatalf("unexpected plan: %+v", first)
			}
			initial := first.Measurements[0]
			final := first.Measurements[3]
			if math.Abs(math.Log2(final[1]/final[0])) >= math.Abs(math.Log2(initial[1]/initial[0])) {
				t.Fatal("feedback did not change measured pixels")
			}
			for _, change := range first.Changes {
				if _, err := WritePacket(change.Frame.Sidecar, change.Packet, false); err != nil {
					t.Fatal(err)
				}
			}
			again, err := planDeflickerSequence(frames, image.Rectangle{}, options, 1, 4, true, smoother)
			if err != nil {
				t.Fatal(err)
			}
			if len(again.Changes) != 0 {
				t.Fatalf("replacement accumulated prior correction: %+v", again.Changes)
			}
			for i := range initial {
				if again.Measurements[0][i] != initial[i] {
					t.Fatal("default measurement included existing XMP")
				}
			}
		})
	}
}

func TestDeflickerFeedbackValidatesBeforeWriting(t *testing.T) {
	frames := feedbackFrames(t)
	for _, reference := range []image.Rectangle{image.Rect(0, 0, 17, 16), image.Rect(-1, 0, 4, 4)} {
		if _, err := planDeflickerSequence(frames, reference, imageInputOptions{}, 1, 4, true, "gaussian"); err == nil {
			t.Fatalf("accepted ROI %v", reference)
		}
	}
	packets := []*XMPPacket{EmptyXMPPacket()}
	if _, err := readSequenceXMPAdjustmentsFromPackets(frames, 16, 16, imageInputOptions{}, packets); err == nil {
		t.Fatal("accepted mismatched adjustment packets")
	}
	if _, err := readXMPCropsFromPackets(frames, 16, 16, packets); err == nil {
		t.Fatal("accepted mismatched crop packets")
	}
	if _, err := buildDeflickerChangesFromPackets(frames, []float64{1, 1, 1}, 1, 1, true, false, "gaussian", false, packets); err == nil {
		t.Fatal("accepted mismatched Deflicker packets")
	}
}
