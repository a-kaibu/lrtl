package lrtl

import (
	"bytes"
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

// Independently authored packets exercise the observed storage shapes without
// including LRT's templates. Their nonzero offsets detect accidental overwrites.
func exposureTestPacket(t *testing.T, kind string) *XMPPacket {
	t.Helper()
	var collection, items string
	switch kind {
	case "modern":
		collection = "MaskGroupBasedCorrections"
		for _, role := range []string{"HG", "Deflicker", "Global"} {
			items += fmt.Sprintf(`<rdf:li rdf:parseType="Resource" crs:What="Correction" crs:CorrectionName="#LRT internal use (%s)" crs:CorrectionActive="true" crs:CorrectionAmount="1" crs:LocalExposure2012="0.2"/>`, role)
		}
	case "legacy", "unsigned":
		collection = "GradientBasedCorrections"
		for i := 1; i <= 7; i++ {
			maskName := "user gradient"
			if i >= 5 && kind == "legacy" {
				maskName = legacyLRTInternalMaskName
			}
			items += fmt.Sprintf(`<rdf:li rdf:parseType="Resource" crs:What="Correction" crs:LocalExposure2012="0.2"><crs:CorrectionMasks><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Mask/Gradient"><crs:MaskName>%s</crs:MaskName></rdf:li></rdf:Seq></crs:CorrectionMasks></rdf:li>`, maskName)
		}
	}
	structure := ""
	if collection != "" {
		structure = "<crs:" + collection + "><rdf:Seq>" + items + "</rdf:Seq></crs:" + collection + ">"
	}
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:vendor="urn:lrtl:test" crs:Exposure2012="0.4"><vendor:Note>preserve me</vendor:Note>` + structure + `</rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func persistExposurePacket(t *testing.T, frame Frame, packet *XMPPacket) Frame {
	t.Helper()
	if _, err := WritePacket(frame.OutputSidecar(), packet, false); err != nil {
		t.Fatal(err)
	}
	frame.Sidecar = frame.OutputSidecar()
	return frame
}

func assertExposureValue(t *testing.T, packet *XMPPacket, target xmpExposureTarget, want float64) {
	t.Helper()
	got, found, err := getXMPExposureTargetValue(packet, target)
	if err != nil || !found || math.Abs(got-want) > 1e-11 {
		t.Fatalf("%s = %.15g, want %.15g (found=%t err=%v)", target, got, want, found, err)
	}
}

func TestLRTExposureSlotSelection(t *testing.T) {
	for _, c := range []struct {
		kind, role string
		index      int
		collection string
	}{
		{"modern", "HG", 1, "MaskGroupBasedCorrections"},
		{"modern", "Deflicker", 2, "MaskGroupBasedCorrections"},
		{"modern", "Global", 3, "MaskGroupBasedCorrections"},
		{"legacy", "HG", 5, "GradientBasedCorrections"},
		{"legacy", "Deflicker", 6, "GradientBasedCorrections"},
		{"unsigned", "Deflicker", 0, ""},
		{"global", "Deflicker", 0, ""},
	} {
		t.Run(c.kind+"/"+c.role, func(t *testing.T) {
			got := findLRTExposureTarget(exposureTestPacket(t, c.kind), c.role, c.index)
			if got.Collection != c.collection || got.Index != c.index {
				t.Fatalf("target=%v", got)
			}
		})
	}
	packet := exposureTestPacket(t, "legacy")
	item, err := xmpExposureTargetElement(packet, xmpExposureTarget{"GradientBasedCorrections", 7})
	if err != nil {
		t.Fatal(err)
	}
	mask := unwrapRDFArrayItem(rdfArrayItems(findCRSDescendant(item, "CorrectionMasks"))[0])
	if err := setCRSElementValue(mask, "MaskName", "user edited"); err != nil {
		t.Fatal(err)
	}
	if got := findLRTExposureTarget(packet, "Deflicker", 6); got.Collection != "" {
		t.Fatal("partial legacy signature accepted")
	}
}

func TestDeflickerDedicatedSlotRoundTripAndReset(t *testing.T) {
	for _, kind := range []string{"modern", "legacy", "global", "unsigned"} {
		t.Run(kind, func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 3)
			for i := range frames {
				frames[i] = persistExposurePacket(t, frames[i], exposureTestPacket(t, kind))
			}
			measurements := []float64{.25, .5, .25}
			changes, err := buildDeflickerChanges(frames, measurements, 1, 1, true, false)
			if err != nil || len(changes) != 1 {
				t.Fatalf("changes=%d err=%v", len(changes), err)
			}
			change := changes[0]
			if math.Abs(change.Correction+.7) > 1e-12 {
				t.Fatalf("correction=%g", change.Correction)
			}
			before := .2
			if kind == "global" || kind == "unsigned" {
				before = .4
			}
			if err := applyDeflickerChanges(changes); err != nil {
				t.Fatal(err)
			}
			assertExposureValue(t, change.Packet, change.Target, before-.7)
			if change.Target.Collection != "" {
				assertExposureValue(t, change.Packet, globalXMPExposureTarget(), .4)
			}
			for _, change := range changes {
				frames[1] = persistExposurePacket(t, change.Frame, change.Packet)
			}
			again, err := buildDeflickerChanges(frames, measurements, 1, 1, true, false)
			if err != nil || len(again) != 0 {
				t.Fatalf("repeated application not idempotent: %d %v", len(again), err)
			}
			// A user's later edit must survive removing our managed delta.
			packet, err := PacketForFrame(frames[1])
			if err != nil {
				t.Fatal(err)
			}
			if err := setXMPExposureTargetValue(packet, change.Target, before-.7+.125); err != nil {
				t.Fatal(err)
			}
			frames[1] = persistExposurePacket(t, frames[1], packet)
			reset, err := buildDeflickerChanges(frames, nil, 1, 1, true, true)
			if err != nil || len(reset) != 1 {
				t.Fatalf("reset=%d %v", len(reset), err)
			}
			if err := applyDeflickerChanges(reset); err != nil {
				t.Fatal(err)
			}
			assertExposureValue(t, reset[0].Packet, change.Target, before+.125)
			for _, name := range []string{deflickerCorrectionProperty, deflickerTargetProperty} {
				if _, found, err := reset[0].Packet.GetScalar(name); err != nil || found {
					t.Fatalf("stale %s", name)
				}
			}
			data, err := reset[0].Packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("preserve me")) {
				t.Fatal("unrelated metadata lost")
			}
		})
	}
}

func TestDeflickerMigratesOldGlobalCorrection(t *testing.T) {
	frame := makeFrames(t, t.TempDir(), 3)[1]
	packet := exposureTestPacket(t, "modern")
	// Previous releases stored only a global delta and no target marker.
	packet.SetScalar("crs:Exposure2012", "-0.3")
	packet.SetScalar(deflickerCorrectionProperty, "-0.7")
	frame = persistExposurePacket(t, frame, packet)
	changes, err := buildDeflickerChanges([]Frame{frame}, []float64{.25}, 1, 1, true, false)
	if err != nil || len(changes) != 1 {
		t.Fatalf("migration=%d %v", len(changes), err)
	}
	if err := applyDeflickerChanges(changes); err != nil {
		t.Fatal(err)
	}
	assertExposureValue(t, changes[0].Packet, globalXMPExposureTarget(), .4)
	assertExposureValue(t, changes[0].Packet, changes[0].Target, .2)
	if _, found, _ := changes[0].Packet.GetScalar(deflickerCorrectionProperty); found {
		t.Fatal("zero correction retained")
	}
}

func TestManagedExposureRendersExactlyOnce(t *testing.T) {
	frame := makeFrames(t, t.TempDir(), 1)[0]
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			packet := exposureTestPacket(t, "modern")
			packet.SetScalar(holyGrailCorrectionProperty, "0.1")
			packet.SetScalar(holyGrailTargetProperty, "MaskGroupBasedCorrections:1")
			packet.SetScalar(deflickerCorrectionProperty, "0.3")
			packet.SetScalar(deflickerTargetProperty, "MaskGroupBasedCorrections:2")
			target := xmpExposureTarget{"MaskGroupBasedCorrections", 2}
			setXMPExposureTargetValue(packet, target, .5)
			element, err := xmpExposureTargetElement(packet, target)
			if err != nil {
				t.Fatal(err)
			}
			setCRSElementValue(element, "CorrectionActive", fmt.Sprint(active))
			setCRSElementValue(element, "CorrectionAmount", "0.5")
			frame = persistExposurePacket(t, frame, packet)
			want := .4 + .2
			if active {
				want += .25
			}
			exposure, err := readXMPExposure(frame)
			if err != nil || math.Abs(exposure-want) > 1e-12 {
				t.Fatalf("exposure=%g want %g err=%v", exposure, want, err)
			}
			basic, err := readXMPBasicAdjustments(frame)
			if err != nil || math.Abs(basic.exposure-want) > 1e-12 {
				t.Fatalf("basic exposure=%g want %g err=%v", basic.exposure, want, err)
			}
			if len(basic.local) != 0 {
				t.Fatal("reserved slots also rendered as masks")
			}
		})
	}
}

func TestDeflickerRejectsBrokenTrackingWithoutWriting(t *testing.T) {
	for _, c := range []struct{ delta, target string }{
		{"NaN", "global"}, {"Inf", "global"}, {"100", "global"}, {"", "MaskGroupBasedCorrections:2"},
		{"1", "MaskGroupBasedCorrections:99"}, {"1", "MaskGroupBasedCorrections:0"}, {"1", "Unknown:1"},
	} {
		t.Run(c.delta+"/"+c.target, func(t *testing.T) {
			frame := makeFrames(t, t.TempDir(), 1)[0]
			packet := exposureTestPacket(t, "modern")
			if c.delta != "" {
				packet.SetScalar(deflickerCorrectionProperty, c.delta)
			}
			packet.SetScalar(deflickerTargetProperty, c.target)
			frame = persistExposurePacket(t, frame, packet)
			before, err := os.ReadFile(frame.Sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := buildDeflickerChanges([]Frame{frame}, nil, 1, 1, true, true); err == nil {
				t.Fatal("invalid tracking accepted")
			}
			after, err := os.ReadFile(frame.Sidecar)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("sidecar changed after error")
			}
		})
	}
}

func TestDeflickerCLIUsesReservedSlotAndBackup(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		writeRenderFrame(t, filepath.Join(dir, fmt.Sprintf("frame%d.jpg", i)), "jpeg", 16, 16)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		// Different effective image brightness produces a measurable correction.
		packet := exposureTestPacket(t, "modern")
		if i == 1 {
			packet.SetScalar("crs:Exposure2012", "1.4")
		}
		persistExposurePacket(t, frame, packet)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"deflicker", dir, "--constant", "--apply-xmp-exposure", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("CLI %d: %s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "MaskGroupBasedCorrections[2].LocalExposure2012") {
		t.Fatalf("missing target in output: %s", out.String())
	}
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	firstText, _, _ := first.GetScalar(deflickerCorrectionProperty)
	firstCorrection, valid := scalarNumber(firstText)
	if !valid {
		t.Fatal("missing first correction")
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"deflicker", dir, "--constant", "--apply-xmp-exposure", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("second pass %d: %s", code, stderr.String())
	}
	second, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	secondText, _, _ := second.GetScalar(deflickerCorrectionProperty)
	secondCorrection, valid := scalarNumber(secondText)
	if !valid || !(secondCorrection < firstCorrection) {
		t.Fatalf("residual replaced previous correction: first=%g second=%g", firstCorrection, secondCorrection)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "*.bak"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestDeflickerResidualAddsToPreviousDelta(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 3)
	packet := exposureTestPacket(t, "modern")
	packet.SetScalar(deflickerCorrectionProperty, "-0.7")
	packet.SetScalar(deflickerTargetProperty, "MaskGroupBasedCorrections:2")
	target := xmpExposureTarget{"MaskGroupBasedCorrections", 2}
	setXMPExposureTargetValue(packet, target, -.5)
	frames[1] = persistExposurePacket(t, frames[1], packet)
	// Original +1 EV flicker has a +0.3 EV residual after the first 70% step.
	measurements := []float64{.25, .25 * math.Exp2(.3), .25}
	changes, err := buildDeflickerChangesForInput(frames, measurements, 1, 1, true, false, "gaussian", true)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%d err=%v", len(changes), err)
	}
	// -0.7 + (-0.3 * 0.7) = -0.91, applied to the original local offset +0.2.
	if math.Abs(changes[0].Correction+.91) > 1e-12 {
		t.Fatalf("delta=%g", changes[0].Correction)
	}
	if err := applyDeflickerChanges(changes); err != nil {
		t.Fatal(err)
	}
	assertExposureValue(t, changes[0].Packet, target, -.71)
}

func TestManagedExposurePixelsThroughCLIRender(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	img := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{100, 100, 100, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	packet := exposureTestPacket(t, "modern")
	packet.SetScalar(holyGrailCorrectionProperty, "0.1")
	packet.SetScalar(holyGrailTargetProperty, "MaskGroupBasedCorrections:1")
	packet.SetScalar(deflickerCorrectionProperty, "0.2")
	packet.SetScalar(deflickerTargetProperty, "MaskGroupBasedCorrections:2")
	setXMPExposureTargetValue(packet, xmpExposureTarget{"MaskGroupBasedCorrections", 2}, .4)
	frame := persistExposurePacket(t, frames[0], packet)
	sidecar, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--apply-xmp-exposure", "--apply-xmp-basic"} {
		t.Run(flag, func(t *testing.T) {
			output := filepath.Join(dir, strings.TrimPrefix(flag, "--")+".avi")
			var stdout, stderr bytes.Buffer
			if code := Main([]string{"render", dir, flag, "--quality", "100", "--output", output}, &stdout, &stderr); code != 0 {
				t.Fatalf("render %d: %s", code, stderr.String())
			}
			avi, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			start := bytes.Index(avi, []byte{0xff, 0xd8})
			if start < 0 {
				t.Fatal("no JPEG frame in AVI")
			}
			actual, err := jpeg.Decode(bytes.NewReader(avi[start:]))
			if err != nil {
				t.Fatal(err)
			}
			pixel := color.NRGBAModel.Convert(actual.At(16, 12)).(color.NRGBA)
			// Total exposure is .4 global + .2 HG + .4 Deflicker = 1 EV.
			// Linear-light sRGB gives 138.180114 for an input channel of 100.
			for _, value := range []uint8{pixel.R, pixel.G, pixel.B} {
				if value < 137 || value > 139 {
					t.Fatalf("rendered channel=%d, want 138 within JPEG rounding", value)
				}
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(unchanged, encoded.Bytes()) {
				t.Fatal("source pixels changed")
			}
			unchanged, err = os.ReadFile(frame.Sidecar)
			if err != nil || !bytes.Equal(unchanged, sidecar) {
				t.Fatal("render changed XMP")
			}
		})
	}
}
