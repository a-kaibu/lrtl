package lrtl

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeTestXMP(t *testing.T, image string, properties map[string]string) string {
	t.Helper()
	packet := EmptyXMPPacket()
	for name, value := range properties {
		if err := packet.SetScalar(name, value); err != nil {
			t.Fatal(err)
		}
	}
	path := image + ".xmp"
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeFrames(t *testing.T, folder string, count int) []Frame {
	t.Helper()
	for index := 0; index < count; index++ {
		path := filepath.Join(folder, "frame"+strconvTwo(index+1)+".dng")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(folder)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}

func strconvTwo(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func TestXMPPacketRoundTripPreservesPrefixesUnknownPropertiesAndPacketPIs(t *testing.T) {
	source := []byte(`<?xpacket begin="x"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
      xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
      xmlns:vendor="urn:example:vendor">
      <crs:Exposure2012>+0.50</crs:Exposure2012>
      <vendor:Note>keep &amp; preserve</vendor:Note>
      <xmp:Rating xmlns:xmp="http://ns.adobe.com/xap/1.0/">2</xmp:Rating>
      <!-- retained comment -->
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)
	packet, err := parseXMP(source)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := packet.GetScalar("crs:Exposure2012"); err != nil || !found || value != "+0.50" {
		t.Fatalf("got value=%q found=%v err=%v", value, found, err)
	}
	if err := packet.SetScalar("crs:Exposure2012", "1.25"); err != nil {
		t.Fatal(err)
	}
	encoded, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{"<x:xmpmeta", "<crs:Exposure2012>", "<vendor:Note>", "xmlns:vendor=", "<?xpacket end=", "<!-- retained comment -->"} {
		if !strings.Contains(text, expected) {
			t.Errorf("serialized XMP is missing %q:\n%s", expected, text)
		}
	}
	reparsed, err := parseXMP(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, _ := reparsed.GetScalar("crs:Exposure2012"); !found || value != "1.25" {
		t.Fatalf("updated value was not preserved: %q", value)
	}
	if value, found, _ := reparsed.GetScalar("xmp:Rating"); !found || value != "2" {
		t.Fatalf("unrelated rating was not preserved: %q", value)
	}
}

func TestEmptyPacketSerializesAsParseableXMP(t *testing.T) {
	packet := EmptyXMPPacket()
	if _, err := MarkKeyframe(packet, "default"); err != nil {
		t.Fatal(err)
	}
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := parseXMP(data)
	if err != nil {
		t.Fatal(err)
	}
	if kind := reparsed.KeyframeKind(); kind != "default" {
		t.Fatalf("got keyframe kind %q", kind)
	}
}

func TestXMPRejectsMalformedAndStructuredPackets(t *testing.T) {
	if _, err := parseXMP([]byte("<xmp>")); err == nil {
		t.Fatal("expected malformed XML error")
	}
	if _, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"/>`)); err == nil {
		t.Fatal("expected missing RDF description error")
	}
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"><crs:ToneCurve><rdf:Seq><rdf:li>0,0</rdf:li></rdf:Seq></crs:ToneCurve></rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := packet.GetScalar("crs:ToneCurve"); err != nil || found {
		t.Fatalf("structured property treated as scalar: found=%v err=%v", found, err)
	}
	if err := packet.SetScalar("crs:ToneCurve", "0,0"); err == nil {
		t.Fatal("expected structured property update to be rejected")
	}
}

func TestMarkAndUnmarkRestoresRatings(t *testing.T) {
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "1"); err != nil {
		t.Fatal(err)
	}
	if rating, err := MarkKeyframe(packet, "default"); err != nil || rating != 4 {
		t.Fatalf("rating=%d err=%v", rating, err)
	}
	if err := UnmarkKeyframe(packet, false); err != nil {
		t.Fatal(err)
	}
	if rating, valid := packet.Rating(); !valid || rating != 1 || packet.IsKeyframe() {
		t.Fatalf("rating=%d valid=%v kind=%q", rating, valid, packet.KeyframeKind())
	}

	packet = EmptyXMPPacket()
	if _, err := MarkKeyframe(packet, "crop"); err != nil {
		t.Fatal(err)
	}
	if err := UnmarkKeyframe(packet, false); err != nil {
		t.Fatal(err)
	}
	if _, valid := packet.Rating(); valid {
		t.Fatal("rating should have been removed")
	}
}

func TestExternalRatingRequiresForceToUnmark(t *testing.T) {
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "5"); err != nil {
		t.Fatal(err)
	}
	if err := UnmarkKeyframe(packet, false); err == nil {
		t.Fatal("expected marker ownership error")
	}
	if err := UnmarkKeyframe(packet, true); err != nil {
		t.Fatal(err)
	}
	if _, valid := packet.Rating(); valid {
		t.Fatal("rating should have been removed")
	}
}

func TestSequenceNaturalOrderAndSidecarSelection(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{"frame10.CR3", "frame2.cr3", "frame1.JPG", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(folder, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(folder)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(frames[0].Image)+","+filepath.Base(frames[1].Image)+","+filepath.Base(frames[2].Image), "frame1.JPG,frame2.cr3,frame10.CR3"; got != want {
		t.Fatalf("got order %q; want %q", got, want)
	}

	image := filepath.Join(folder, "photo.CR3")
	if err := os.WriteFile(image, nil, 0600); err != nil {
		t.Fatal(err)
	}
	appended := image + ".xmp"
	replaced := strings.TrimSuffix(image, filepath.Ext(image)) + ".xmp"
	for _, path := range []string{appended, replaced} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Unix(1_800_000_000, 0)
	if err := os.Chtimes(appended, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replaced, base.Add(time.Second), base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	frames, err = DiscoverSequence(folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		if filepath.Base(frame.Image) == "photo.CR3" && frame.Sidecar != replaced {
			t.Fatalf("selected sidecar %q; want newer %q", frame.Sidecar, replaced)
		}
	}
}

func TestInterpolationWhitelistAndSharedProperties(t *testing.T) {
	for _, name := range []string{"Exposure", "RedHue", "Exposure2012", "Highlights2012"} {
		if !slices.Contains(NumericCRSProperties, name) {
			t.Errorf("whitelist missing %s", name)
		}
	}
	for _, name := range []string{"CropUnits", "ToneCurve", "HasCrop", "LocalExposure2012"} {
		if slices.Contains(NumericCRSProperties, name) {
			t.Errorf("whitelist unexpectedly contains %s", name)
		}
	}

	folder := t.TempDir()
	frames := makeFrames(t, folder, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{
		"xmp:Rating": "4", "crs:Exposure2012": "0", "crs:Brightness": "10", "crs:CropUnits": "0",
	})
	writeTestXMP(t, frames[2].Image, map[string]string{
		"xmp:Rating": "4", "crs:Exposure2012": "2", "crs:Brightness": "30", "crs:CropUnits": "1",
	})
	frames, err := DiscoverSequence(folder)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("got %d changes", len(plan.Changes))
	}
	if plan.Changes[0].Values["Exposure2012"] != "1" || plan.Changes[0].Values["Brightness"] != "20" {
		t.Fatalf("unexpected values: %#v", plan.Changes[0].Values)
	}
	if _, exists := plan.Changes[0].Values["CropUnits"]; exists {
		t.Fatal("categorical CropUnits should not be interpolated")
	}
}

func TestSingleAndUnmarkedSequencesUseReferenceFrames(t *testing.T) {
	t.Run("one marked keyframe", func(t *testing.T) {
		folder := t.TempDir()
		frames := makeFrames(t, folder, 3)
		writeTestXMP(t, frames[1].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "-1.5"})
		frames, _ = DiscoverSequence(folder)
		plan, err := buildPlan(frames)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.KeyframeIndices) != 1 || plan.KeyframeIndices[0] != 1 || len(plan.Changes) != 2 {
			t.Fatalf("unexpected plan: %#v", plan)
		}
		if !plan.Changes[0].CreatedSidecar || !plan.Changes[1].CreatedSidecar {
			t.Fatal("missing sidecars should be marked for creation")
		}
		for _, change := range plan.Changes {
			if change.Values["Exposure2012"] != "-1.5" {
				t.Fatalf("single reference value not propagated: %#v", change.Values)
			}
		}
	})

	t.Run("no marked keyframes", func(t *testing.T) {
		folder := t.TempDir()
		frames := makeFrames(t, folder, 3)
		writeTestXMP(t, frames[0].Image, map[string]string{"crs:RedHue": "-20"})
		frames, _ = DiscoverSequence(folder)
		plan, err := buildPlan(frames)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.KeyframeIndices) != 1 || plan.KeyframeIndices[0] != 0 || len(plan.Changes) != 2 {
			t.Fatalf("unexpected plan: %#v", plan)
		}
		for _, change := range plan.Changes {
			if change.Values["RedHue"] != "-20" {
				t.Fatalf("unexpected change values: %#v", change.Values)
			}
		}
	})
}

func TestInterpolationExtendsNearestKeyframeToEndpoints(t *testing.T) {
	folder := t.TempDir()
	frames := makeFrames(t, folder, 7)
	writeTestXMP(t, frames[0].Image, map[string]string{"crs:Exposure2012": "-4"})
	writeTestXMP(t, frames[2].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "0"})
	writeTestXMP(t, frames[4].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "4"})
	writeTestXMP(t, frames[6].Image, map[string]string{"crs:Exposure2012": "8"})
	frames, _ = DiscoverSequence(folder)
	// With zero Bezier tangents the outer segments are constant, and the
	// middle segment passes through value 2 at frame 3. Unmarked endpoint
	// values are overwritten by the nearest keyframe under lrtl's current
	// endpoint policy. Exact LRT endpoint parity remains unverified.
	plan, err := buildPlanWithSmoothness(frames, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 5 {
		t.Fatalf("got changes at %d frames", len(plan.Changes))
	}
	for index, want := range []string{"0", "0", "2", "4", "4"} {
		change := plan.Changes[index]
		if change.Frame.Index != []int{0, 1, 3, 5, 6}[index] || change.Values["Exposure2012"] != want {
			t.Errorf("change %d: index=%d value=%q", index, change.Frame.Index, change.Values["Exposure2012"])
		}
	}
}

func TestLocalCorrectionsArePreservedDuringInterpolation(t *testing.T) {
	folder := t.TempDir()
	frames := makeFrames(t, folder, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{
		"xmp:Rating": "4", "crs:Exposure2012": "0", "crs:LocalExposure2012": "-1",
	})
	middlePath := writeTestXMP(t, frames[1].Image, map[string]string{
		"crs:LocalExposure2012": "17", "crs:CorrectionAmount": "0.75",
	})
	writeTestXMP(t, frames[2].Image, map[string]string{
		"xmp:Rating": "4", "crs:Exposure2012": "2", "crs:LocalExposure2012": "1",
	})
	frames, _ = DiscoverSequence(folder)
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := applyPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || plan.Changes[0].Values["Exposure2012"] != "1" {
		t.Fatalf("unexpected transition plan: %#v", plan)
	}
	data, err := outputs[0].Packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := parseXMP(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"crs:Exposure2012": "1", "crs:LocalExposure2012": "17", "crs:CorrectionAmount": "0.75",
	} {
		if got, found, _ := serialized.GetScalar(name); !found || got != want {
			t.Errorf("%s: got %q found=%v, want %q", name, got, found, want)
		}
	}
	original, err := os.ReadFile(middlePath)
	if err != nil {
		t.Fatal(err)
	}
	originalPacket, err := parseXMP(original)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := originalPacket.GetScalar("crs:LocalExposure2012"); got != "17" {
		t.Fatalf("dry plan unexpectedly changed source file: %q", got)
	}
}

func TestBadSidecarErrorIncludesPath(t *testing.T) {
	folder := t.TempDir()
	frames := makeFrames(t, folder, 1)
	path := frames[0].Image + ".xmp"
	if err := os.WriteFile(path, []byte("<broken"), 0600); err != nil {
		t.Fatal(err)
	}
	frames, _ = DiscoverSequence(folder)
	if _, err := buildPlan(frames); err == nil || !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("expected path-bearing parse error, got %v", err)
	}
}

func TestWritePacketMakesBackupAndReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.xmp")
	if err := os.WriteFile(path, []byte("old sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
		t.Fatal(err)
	}
	backup, err := WritePacket(path, packet, true)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected a backup path")
	}
	backupData, err := os.ReadFile(backup)
	if err != nil || string(backupData) != "old sidecar" {
		t.Fatalf("backup=%q err=%v", backupData, err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseXMP(updated)
	if err != nil {
		t.Fatal(err)
	}
	if rating, valid := parsed.Rating(); !valid || rating != 4 {
		t.Fatalf("rating=%d valid=%v", rating, valid)
	}
}

func TestCLICommandsRestoreMarksAndDefaultToDryRun(t *testing.T) {
	folder := t.TempDir()
	image := filepath.Join(folder, "photo.CR3")
	if err := os.WriteFile(image, nil, 0600); err != nil {
		t.Fatal(err)
	}
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "1"); err != nil {
		t.Fatal(err)
	}
	sidecar := image + ".xmp"
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"mark", image, "--kind", "crop"}, &stdout, &stderr); code != 0 {
		t.Fatalf("mark exit=%d stderr=%s", code, stderr.String())
	}
	markedData, _ := os.ReadFile(sidecar)
	marked, err := parseXMP(markedData)
	if err != nil {
		t.Fatal(err)
	}
	if rating, valid := marked.Rating(); !valid || rating != 5 || marked.KeyframeKind() != "crop" {
		t.Fatalf("rating=%d valid=%v kind=%s", rating, valid, marked.KeyframeKind())
	}
	backups, _ := filepath.Glob(sidecar + ".lrtl-*.bak")
	if len(backups) != 1 {
		t.Fatalf("got %d backups", len(backups))
	}
	if code := Main([]string{"unmark", image}, &stdout, &stderr); code != 0 {
		t.Fatalf("unmark exit=%d stderr=%s", code, stderr.String())
	}
	restoredData, _ := os.ReadFile(sidecar)
	restored, err := parseXMP(restoredData)
	if err != nil {
		t.Fatal(err)
	}
	if rating, valid := restored.Rating(); !valid || rating != 1 {
		t.Fatalf("rating=%d valid=%v", rating, valid)
	}

	sequence := filepath.Join(folder, "sequence")
	if err := os.Mkdir(sequence, 0700); err != nil {
		t.Fatal(err)
	}
	frames := makeFrames(t, sequence, 3)
	writeTestXMP(t, frames[0].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "0"})
	writeTestXMP(t, frames[2].Image, map[string]string{"xmp:Rating": "4", "crs:Exposure2012": "2"})
	frames, _ = DiscoverSequence(sequence)
	middleSidecar := frames[1].OutputSidecar()
	stdout.Reset()
	if code := Main([]string{"interpolate", sequence}, &stdout, &stderr); code != 0 {
		t.Fatalf("interpolate preview exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(middleSidecar); !os.IsNotExist(err) {
		t.Fatal("preview unexpectedly wrote an XMP sidecar")
	}
	if code := Main([]string{"interpolate", sequence, "--write"}, &stdout, &stderr); code != 0 {
		t.Fatalf("interpolate write exit=%d stderr=%s", code, stderr.String())
	}
	middleData, err := os.ReadFile(middleSidecar)
	if err != nil {
		t.Fatal(err)
	}
	middle, err := parseXMP(middleData)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, _ := middle.GetScalar("crs:Exposure2012"); !found || value != "1" {
		t.Fatalf("interpolated value=%q found=%v", value, found)
	}
}
