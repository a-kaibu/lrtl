package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func TestMissingCorrectionCLIAndRepeat(t *testing.T) {
	root, control := t.TempDir(), t.TempDir()
	frames := cropFixture(t, 5)
	for i, frame := range frames {
		packet := localMissingPacket(t, "MaskGroupBasedCorrections", map[string]string{"LocalExposure2012": []string{".4", ".4", ".4", ".8", ".8"}[i], "CorrectionSyncID": fmt.Sprint("own-", i)}, nil)
		if i == 2 {
			packet = EmptyXMPPacket()
		}
		if i%2 == 0 {
			if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
				t.Fatal(err)
			}
		}
		if err := packet.SetScalar("dc:description", fmt.Sprint("preserve-", i)); err != nil {
			t.Fatal(err)
		}
		persistExposurePacket(t, frame, packet)
	}
	copyBatchDeflickerTree(t, filepath.Dir(frames[0].Image), root)
	copyBatchDeflickerTree(t, root, control)
	actual, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := DiscoverSequence(control)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := PacketForFrame(reference[2])
	if err != nil {
		t.Fatal(err)
	}
	_, array, err := ensurePacketCRSArray(packet, "MaskGroupBasedCorrections")
	if err != nil {
		t.Fatal(err)
	}
	item, err := ensureRDFArrayPosition(array, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"LocalExposure2012": ".4", "CorrectionAmount": "1", "CorrectionActive": "True"} {
		if err := setCRSElementValue(item, name, value); err != nil {
			t.Fatal(err)
		}
	}
	persistExposurePacket(t, reference[2], packet)
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"interpolate", root}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("dry run changed files")
	}
	for _, dir := range []string{root, control} {
		if Main([]string{"interpolate", dir, "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
	}
	for i, frame := range actual {
		got, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		want, err := PacketForFrame(reference[i])
		if err != nil {
			t.Fatal(err)
		}
		gotNodes, err := indexMaskTransitionNodes(got)
		if err != nil {
			t.Fatal(err)
		}
		wantNodes, err := indexMaskTransitionNodes(want)
		if err != nil {
			t.Fatal(err)
		}
		key := maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionIndex: 1}
		if !reflect.DeepEqual(gotNodes[key].values, wantNodes[key].values) {
			t.Fatalf("frame %d differs from independently prefilled keyframe: %v / %v", i, gotNodes[key].values, wantNodes[key].values)
		}
		text, _, _ := got.GetScalar("dc:description")
		if text != fmt.Sprint("preserve-", i) {
			t.Fatal("changed unrelated metadata")
		}
		if i != 2 {
			id, _, _ := crsElementValue(unwrapRDFArrayItem(gotNodes[key].correctionItem), "CorrectionSyncID")
			if id != fmt.Sprint("own-", i) {
				t.Fatal("replaced frame's own ID")
			}
		}
		backups, err := filepath.Glob(frame.Sidecar + "*.bak")
		data, readErr := os.ReadFile(frame.Sidecar)
		if err != nil || readErr != nil {
			t.Fatalf("read sidecar/backups: %v / %v", err, readErr)
		}
		if bytes.Equal(data, before[frame.Sidecar]) {
			if len(backups) != 0 {
				t.Fatal("unchanged XMP was backed up")
			}
		} else {
			if len(backups) != 1 {
				t.Fatalf("expected one original backup, got %v", backups)
			}
			assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
		}
	}
	after := batchFileSnapshot(t, root)
	if Main([]string{"interpolate", root, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
		t.Fatal("repeated interpolation changed XMP/backups")
	}
}

func TestMissingCorrectionAgainstInstalled755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-missing-correction-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Collection, Property, Expected, Container string
		Slot, ItemCount                           int
		Input                                     []*string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 16 {
		t.Fatal("expected 16 native missing-resource observations")
	}
	for n, tc := range cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			packets := make([]*XMPPacket, len(tc.Input))
			for i, value := range tc.Input {
				packets[i] = EmptyXMPPacket()
				if err := packets[i].SetScalar("dc:description", fmt.Sprint("keep-", i)); err != nil {
					t.Fatal(err)
				}
				if value == nil {
					continue
				}
				_, array, err := ensurePacketCRSArray(packets[i], tc.Collection)
				if err != nil {
					t.Fatal(err)
				}
				item, err := ensureRDFArrayPosition(array, tc.Slot, nil)
				if err != nil {
					t.Fatal(err)
				}
				for name, text := range map[string]string{tc.Property: *value, "CorrectionSyncID": fmt.Sprint("source-", i), "CorrectionName": "do not clone"} {
					if err := setCRSElementValue(unwrapRDFArrayItem(item), name, text); err != nil {
						t.Fatal(err)
					}
				}
			}
			changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
			if err != nil {
				t.Fatal(err)
			}
			for _, change := range changes[1] {
				if change.templateCorrection != nil || change.templateMask != nil {
					t.Fatal("selected missing correction cloned a source")
				}
				if err := applyMaskTransition(packets[1], change); err != nil {
					t.Fatal(err)
				}
			}
			property := packets[1].findRootProperty(crsNamespace, tc.Collection)
			container := rdfArrayContainer(property.element)
			if container == nil || container.Tag != tc.Container {
				t.Fatalf("container differs from native %s", tc.Container)
			}
			items := rdfArrayItemsInContainer(container)
			if len(items) != tc.ItemCount {
				t.Fatalf("got %d resources, native %d", len(items), tc.ItemCount)
			}
			got, found, err := crsElementValue(unwrapRDFArrayItem(items[tc.Slot-1]), tc.Property)
			if err != nil || !found {
				t.Fatalf("missing saved property: %s %v", got, err)
			}
			if tc.Property == "CorrectionActive" {
				if got != tc.Expected {
					t.Fatalf("got %s native %s", got, tc.Expected)
				}
			} else {
				a, errA := strconv.ParseFloat(got, 64)
				b, errB := strconv.ParseFloat(tc.Expected, 64)
				if errA != nil || errB != nil || a != b {
					t.Fatalf("got %s native %s", got, tc.Expected)
				}
			}
			for _, name := range []string{"CorrectionSyncID", "CorrectionName", "What"} {
				if _, found, _ := crsElementValue(unwrapRDFArrayItem(items[tc.Slot-1]), name); found {
					t.Fatalf("copied %s", name)
				}
			}
			text, _, _ := packets[1].GetScalar("dc:description")
			if text != "keep-1" {
				t.Fatal("overwrote unrelated metadata")
			}
			assertRDFResourceSyntax(t, packets[1].Document.Root())
		})
	}
}

func assertRDFResourceSyntax(t *testing.T, root *etree.Element) {
	t.Helper()
	resource := false
	for _, attr := range root.Attr {
		if attributeMatches(&attr, rdfNamespace, "parseType") && attr.Value == "Resource" {
			resource = true
		}
	}
	if resource {
		for _, attr := range root.Attr {
			if attr.Space != "xmlns" && attr.Key != "xmlns" && !attributeMatches(&attr, rdfNamespace, "parseType") {
				t.Fatalf("RDF Resource contains an illegal property attribute: %s:%s", attr.Space, attr.Key)
			}
		}
	}
	for _, child := range root.ChildElements() {
		assertRDFResourceSyntax(t, child)
	}
}

func generatedMaskResources(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for _, format := range []string{"modern", "legacy"} {
		packet := EmptyXMPPacket()
		var ids [18]string
		for i := range ids {
			ids[i] = fmt.Sprintf("%032X", i+1)
		}
		if _, err := initializeMaskPacket(packet, format, ids); err != nil {
			t.Fatal(err)
		}
		assertRDFResourceSyntax(t, packet.Document.Root())
		data, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, format+".xmp")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	for _, collection := range maskTransitionCollections {
		packets := []*XMPPacket{localMissingPacket(t, collection, map[string]string{"LocalExposure2012": ".4"}, nil), EmptyXMPPacket(), localMissingPacket(t, collection, map[string]string{"LocalExposure2012": ".8"}, nil)}
		changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range changes[1] {
			if err := applyMaskTransition(packets[1], change); err != nil {
				t.Fatal(err)
			}
		}
		assertRDFResourceSyntax(t, packets[1].Document.Root())
		data, err := packets[1].Serialize()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, collection+".xmp")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	for i, tc := range missingSelectorObservations(t) {
		packets := missingSelectorPackets(t, tc)
		applySelectedSelector(t, packets)
		assertRDFResourceSyntax(t, packets[1].Document.Root())
		data, err := packets[1].Serialize()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("selector-%d.xmp", i))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	for i, tc := range missingRangeObservations(t) {
		packets := missingRangePackets(t, tc)
		applySelectedRange(t, packets)
		assertRDFResourceSyntax(t, packets[1].Document.Root())
		data, err := packets[1].Serialize()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("range-%d.xmp", i))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	return files
}

func TestGeneratedMaskResourcesRDFSyntax(t *testing.T) {
	generatedMaskResources(t)
}

func TestGeneratedMaskResourcesAdobeParser(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for optional installed Adobe XMP parser verification")
	}
	files := generatedMaskResources(t)
	dir := t.TempDir()
	source := `package lrtlprobe;
import java.lang.reflect.*; import java.io.*; import java.util.*;
public class GeneratedXMPParser {
 public static void main(String[] paths)throws Exception {
  Class.forName("tQa"); Class<?> factory=Class.forName("ABA"),wd=Class.forName("Wd");
  Method parse=Arrays.stream(factory.getMethods()).filter(m->m.getName().equals("A")&&m.getReturnType()==wd&&Arrays.equals(m.getParameterTypes(),new Class<?>[]{InputStream.class})).findFirst().orElseThrow();
  for(String path:paths)try(InputStream stream=new FileInputStream(path)){if(parse.invoke(null,stream)==null)throw new Exception("null XMP packet");}
  System.out.println("PARSED "+paths.length+" GENERATED XMP PACKETS");
 }
}`
	path := filepath.Join(dir, "GeneratedXMPParser.java")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("javac", "-d", dir, path).CombinedOutput(); err != nil {
		t.Fatalf("compile native parser harness: %v\n%s", err, output)
	}
	args := append([]string{"-cp", dir + string(os.PathListSeparator) + jar, "lrtlprobe.GeneratedXMPParser"}, files...)
	output, err := exec.Command("java", args...).CombinedOutput()
	if err != nil || !strings.Contains(string(output), fmt.Sprintf("PARSED %d GENERATED XMP PACKETS", len(files))) {
		t.Fatalf("installed parser: %v\n%s", err, output)
	}
}
