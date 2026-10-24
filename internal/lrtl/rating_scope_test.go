package lrtl

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

func ratingWorkflowPacket(t *testing.T, form string) *XMPPacket {
	t.Helper()
	attrs, body := "", ""
	switch form {
	case "attribute":
		attrs = `xmp:Rating="4"`
	case "qualified":
		body = `<xmp:Rating xml:lang="en-US">4</xmp:Rating>`
	case "uri":
		body = `<xmp:Rating rdf:resource="4"/>`
	case "wrapped":
		body = `<xmp:Rating rdf:parseType="Resource"><rdf:value>4</rdf:value><f:Qualifier>keep</f:Qualifier></xmp:Rating>`
	case "absent":
	default:
		t.Fatal("unknown rating form", form)
	}
	return pasteTestPacket(t, fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="%s"><rdf:Description xmlns:xmp="%s" xmlns:ol="%s" xmlns:f="urn:lrtl:rating:scope" %s>%s<f:Nested><rdf:Description xmp:Rating="5" ol:KeyframeKind="crop" ol:PreviousRating="77" ol:HolyGrailKeyframeKind="2" ol:HolyGrailPreviousRating="__absent__"><f:Keep>nested</f:Keep></rdf:Description></f:Nested></rdf:Description></rdf:RDF></x:xmpmeta>`, rdfNamespace, xmpNamespace, namespacePrefixes["ol"], attrs, body))
}

func ratingScopeNode(t *testing.T, packet *XMPPacket) *pasteNode {
	t.Helper()
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	return clonePasteNode(selectedField(nodes, pasteName{"urn:lrtl:rating:scope", "Nested"}))
}

func TestRatingRootRepresentationsMarkAndRestore(t *testing.T) {
	for _, form := range []string{"attribute", "qualified", "uri", "wrapped", "absent"} {
		t.Run(form, func(t *testing.T) {
			packet := ratingWorkflowPacket(t, form)
			before := ratingScopeNode(t, packet)
			original, err := pastePacketNodes(packet)
			if err != nil {
				t.Fatal(err)
			}
			var originalRating *pasteNode
			if node := selectedField(original, pasteName{xmpNamespace, "Rating"}); node != nil {
				originalRating = clonePasteNode(node)
			}
			rating, present := packet.Rating()
			kind := packet.KeyframeKind()
			if form == "absent" {
				if present || kind != "" {
					t.Fatal("nested rating/marker leaked into global keyframe", rating, kind)
				}
			} else if !present || rating != 4 || kind != "default" {
				t.Fatal("rating representation not recognized", rating, present, kind)
			}
			if _, err := MarkKeyframe(packet, "crop"); err != nil {
				t.Fatal(err)
			}
			if value, found := packet.Rating(); !found || value != 5 || packet.KeyframeKind() != "crop" {
				t.Fatal("mark did not update root rating")
			}
			if !pasteNodesEqual(before, ratingScopeNode(t, packet)) {
				t.Fatal("mark changed nested subjects")
			}
			if err := UnmarkKeyframe(packet, false); err != nil {
				t.Fatal(err)
			}
			after, err := pastePacketNodes(packet)
			if err != nil {
				t.Fatal(err)
			}
			restored := selectedField(after, pasteName{xmpNamespace, "Rating"})
			if (originalRating == nil) != (restored == nil) || restored != nil && !pasteNodesEqual(originalRating, restored) {
				t.Fatal("unmark did not restore rating type/value/qualifiers")
			}
			if !pasteNodesEqual(before, ratingScopeNode(t, packet)) {
				t.Fatal("unmark changed nested subjects")
			}
			if form != "absent" {
				if err := UnmarkKeyframe(packet, true); err != nil {
					t.Fatal(err)
				}
				if _, found, _ := readRootRating(packet); found {
					t.Fatal("force clear retained root rating")
				}
				if !pasteNodesEqual(before, ratingScopeNode(t, packet)) {
					t.Fatal("force clear removed nested ratings")
				}
			}
		})
	}
}

func TestRatingRootSelectionAndReference(t *testing.T) {
	for _, form := range []string{"attribute", "qualified", "uri", "wrapped", "absent"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
			frame := metadataDateFrame(t, dir, "frame.jpg", "True", "qualified")
			packet := ratingWorkflowPacket(t, form)
			if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
				t.Fatal(err)
			}
			selected, err := filterSequenceFrames([]Frame{frame}, dir, frameSelectionOptions{Ratings: []int{4}})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if form == "absent" {
				want = 0
			}
			if len(selected) != want {
				t.Fatal("selection used wrong rating subject/type", len(selected), want)
			}
			first := EmptyXMPPacket()
			if reference := nativeFrameStateReference([]*XMPPacket{first, packet}); reference != want {
				t.Fatal("frame-state reference ignored rating representation", reference, want)
			}
		})
	}
}

func TestRatingRootHolyGrailMarkAndReset(t *testing.T) {
	packet := ratingWorkflowPacket(t, "absent")
	nested := ratingScopeNode(t, packet)
	rating, restore, previous, err := holyGrailKeyframePlan(packet, 2)
	if err != nil || rating != 2 || restore || previous != absentRating {
		t.Fatal("nested HG marker interfered with root plan", rating, restore, previous, err)
	}
	if err := applyHolyGrailPlan([]HolyGrailChange{{Packet: packet, KeyframeRating: rating, PreviousRating: previous}}); err != nil {
		t.Fatal(err)
	}
	if value, found := packet.Rating(); !found || value != 2 {
		t.Fatal("HG rating not written to root", value, found)
	}
	if !pasteNodesEqual(nested, ratingScopeNode(t, packet)) {
		t.Fatal("HG mark changed nested subjects")
	}
	_, restore, previous, err = holyGrailKeyframePlan(packet, 0)
	if err != nil || !restore || previous != absentRating {
		t.Fatal("invalid root HG reset", restore, previous, err)
	}
	if err := applyHolyGrailPlan([]HolyGrailChange{{Packet: packet, RestoreKeyframe: true, PreviousRating: previous}}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := readRootRating(packet); found {
		t.Fatal("HG reset retained root rating")
	}
	if !pasteNodesEqual(nested, ratingScopeNode(t, packet)) {
		t.Fatal("HG reset removed nested ratings/markers")
	}
}

func TestRatingMarkFailuresDoNotMutate(t *testing.T) {
	for _, operation := range []string{"mark", "unmark"} {
		t.Run(operation, func(t *testing.T) {
			packet := ratingWorkflowPacket(t, "absent")
			body := packet.rootDescriptions()[0]
			if operation == "mark" {
				node := body.CreateElement("ol:KeyframeKind")
				node.CreateAttr("rdf:parseType", "Resource")
				node.CreateElement("f:Value").SetText("invalid")
			} else {
				if err := setCaptureTimestampScalar(packet, "ol:KeyframeKind", "crop"); err != nil {
					t.Fatal(err)
				}
				if err := setCaptureTimestampScalar(packet, "ol:PreviousRating", "4"); err != nil {
					t.Fatal(err)
				}
				node := body.CreateElement("xmp:Rating")
				node.CreateAttr("rdf:parseType", "Resource")
				node.CreateElement("f:Value").SetText("invalid")
			}
			before, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if operation == "mark" {
				_, err = MarkKeyframe(packet, "default")
			} else {
				err = UnmarkKeyframe(packet, false)
			}
			if err == nil {
				t.Fatal("accepted structured marker/rating")
			}
			after, _ := packet.Serialize()
			if !bytes.Equal(before, after) {
				t.Fatal("failed mark/unmark partially changed packet")
			}
		})
	}
}

func TestRatingCopyQualifiedURISelectionWorkflow(t *testing.T) {
	for _, form := range []string{"qualified", "uri", "wrapped"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
			frame := metadataDateFrame(t, dir, "frame.jpg", "True", "qualified")
			packet := ratingWorkflowPacket(t, form)
			nested := ratingScopeNode(t, packet)
			if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(t.TempDir(), "source.xmp")
			if _, err := WritePacket(source, EmptyXMPPacket(), false); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if Main([]string{"copy-settings", source, dir, "--ratings", "4", "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			saved, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			if value, found := saved.Rating(); !found || value != 4 || saved.KeyframeKind() != "default" {
				t.Fatal("copied RDF rating invisible to keyframe workflow", value, found)
			}
			if !pasteNodesEqual(nested, ratingScopeNode(t, saved)) {
				t.Fatal("copy changed nested keyframe subjects")
			}
		})
	}
}

func FuzzRatingMarkRootScope(f *testing.F) {
	for _, row := range reloadRatingObservations(f).Cases {
		if row.Parent == "sequence" {
			f.Add([]byte(row.Input), false, false)
		}
	}
	for _, body := range []string{`<xmp:Rating rdf:resource="4"/>`, `<xmp:Rating rdf:parseType="Resource"><rdf:value>4</rdf:value><f:Qualifier>keep</f:Qualifier></xmp:Rating>`, `<f:Nested><rdf:Description xmp:Rating="5" ol:KeyframeKind="crop"/></f:Nested>`, `<ol:KeyframeKind rdf:parseType="Resource"><f:Value>invalid</f:Value></ol:KeyframeKind>`} {
		data := fmt.Sprintf(`<rdf:RDF xmlns:rdf="%s"><rdf:Description xmlns:xmp="%s" xmlns:ol="%s" xmlns:f="urn:lrtl:rating:scope">%s</rdf:Description></rdf:RDF>`, rdfNamespace, xmpNamespace, namespacePrefixes["ol"], body)
		f.Add([]byte(data), true, true)
	}
	f.Fuzz(func(t *testing.T, data []byte, crop, force bool) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		packet, err := parseXMP(data)
		if err != nil {
			return
		}
		before, err := packet.Serialize()
		if err != nil {
			return
		}
		original, originalErr := pastePacketNodes(packet)
		kind := "default"
		if crop {
			kind = "crop"
		}
		_, markErr := MarkKeyframe(packet, kind)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if markErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed mark mutated packet")
			}
			return
		}
		if originalErr != nil {
			t.Fatal("marked unsupported metadata instead of rejecting it")
		}
		checkScope := func() {
			actual, err := pastePacketNodes(packet)
			if err != nil {
				t.Fatal(err)
			}
			permitted := func(name pasteName) bool {
				return name == (pasteName{xmpNamespace, "Rating"}) || name == (pasteName{namespacePrefixes["ol"], "KeyframeKind"}) || name == (pasteName{namespacePrefixes["ol"], "PreviousRating"})
			}
			for _, node := range original {
				if permitted(node.Name) {
					continue
				}
				got := selectedField(actual, node.Name)
				if got == nil || !pasteNodesEqual(got, node) {
					t.Fatal("mark/unmark changed unrelated root or nested subjects", node.Name)
				}
			}
			for _, node := range actual {
				if !permitted(node.Name) && selectedField(original, node.Name) == nil {
					t.Fatal("mark/unmark added unrelated metadata", node.Name)
				}
			}
		}
		checkScope()
		if _, err := parseXMP(after); err != nil {
			t.Fatal("invalid marked XMP", err)
		}
		if err := UnmarkKeyframe(packet, force); err != nil {
			current, _ := packet.Serialize()
			if !bytes.Equal(after, current) {
				t.Fatal("failed unmark mutated packet")
			}
			return
		}
		checkScope()
		current, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseXMP(current); err != nil {
			t.Fatal("invalid unmarked XMP", err)
		}
	})
}

func TestRatingRootCLIInterpolationAndAVIPixels(t *testing.T) {
	dir := t.TempDir()
	frames := make([]Frame, 3)
	forms := []string{"uri", "absent", "wrapped"}
	for i, form := range forms {
		frames[i] = metadataDateFrame(t, dir, fmt.Sprintf("frame-%02d.jpg", i), "True", "qualified")
		packet := ratingWorkflowPacket(t, form)
		value := []string{"0", "-1", "1"}[i]
		if err := packet.SetScalar("crs:Exposure2012", value); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frames[i].Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	if Main([]string{"mark", frames[1].Image, "--kind", "default"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"unmark", frames[1].Image}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	middle, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := readRootRating(middle); found {
		t.Fatal("mark/unmark did not restore absent middle-frame rating")
	}
	nested := ratingScopeNode(t, middle)
	selected, err := filterSequenceFrames(frames, dir, frameSelectionOptions{Ratings: []int{4}})
	if err != nil || len(selected) != 2 {
		t.Fatal("RDF endpoint selection failed", len(selected), err)
	}
	if Main([]string{"interpolate", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	middle, err = PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := middle.GetRootScalar("crs:Exposure2012")
	if err != nil || !found {
		t.Fatal(value, found, err)
	}
	number, valid := scalarNumber(value)
	if !valid || number != .5 {
		t.Fatal("nested rating interfered with URI/wrapped endpoint interpolation", value)
	}
	if !pasteNodesEqual(nested, ratingScopeNode(t, middle)) {
		t.Fatal("interpolation changed nested ratings/markers")
	}
	render := func() []byte {
		path := filepath.Join(t.TempDir(), "video.avi")
		if Main([]string{"render", dir, "--output", path, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI payload")
		}
		return data[offset:]
	}
	interpolated := render()
	if err := middle.SetScalar("crs:Exposure2012", "0.5"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frames[1].Sidecar, middle, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(interpolated, render()) {
		t.Fatal("RDF-rated keyframe interpolation differs from explicit exposure control pixels")
	}
}
