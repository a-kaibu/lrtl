package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMaskBooleanTransitionsAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-local-boolean.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Registered []string
		Cases      []struct {
			Property        string
			Input, Expected []*bool
			Ratings         []int
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 24 || len(fixture.Registered) != 26 {
		t.Fatal("incomplete component fixture")
	}
	var registered []string
	for _, collection := range maskTransitionCollections {
		for position := 1; position <= 12; position++ {
			for selector := 0; selector <= 2; selector++ {
				key := maskTransitionKey{collection: collection, correctionIndex: position, maskIndex: selector}
				for _, name := range maskBooleanTransitionProperties(key) {
					path := fmt.Sprintf("%s[%d]", collection, position)
					if selector > 0 {
						path += fmt.Sprintf("/crs:CorrectionMasks[%d]", selector)
					}
					registered = append(registered, path+"/crs:"+name)
				}
			}
		}
	}
	sort.Strings(registered)
	sort.Strings(fixture.Registered)
	if !reflect.DeepEqual(registered, fixture.Registered) {
		t.Fatalf("registry differs: %v", registered)
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			collection := strings.Split(c.Property, "[")[0]
			name := c.Property[strings.LastIndex(c.Property, ":")+1:]
			key := maskTransitionKey{collection: collection, correctionIndex: 1}
			if name == "Flipped" {
				key.maskIndex = 1
			}
			packets := make([]*XMPPacket, len(c.Input))
			for i, value := range c.Input {
				local, geometry := map[string]string{}, map[string]string{}
				if value != nil {
					text := "False"
					if *value {
						text = "True"
					}
					if name == "Flipped" {
						geometry[name] = text
					} else {
						local[name] = text
					}
				}
				packets[i] = localMissingPacket(t, collection, local, geometry)
				if value == nil && name == "CorrectionActive" {
					maskGroup := packets[i].findRootProperty(crsNamespace, collection)
					unwrapRDFArrayItem(rdfArrayItems(maskGroup.element)[0]).RemoveAttr("crs:CorrectionActive")
				}
				if err := packets[i].SetScalar("xmp:Rating", fmt.Sprint(c.Ratings[i])); err != nil {
					t.Fatal(err)
				}
			}
			keys, _, _ := transitionKeyframeGroups(packets)
			changes, err := planMaskTransitions(packets, keys, 50)
			if err != nil {
				t.Fatal(err)
			}
			for i, packet := range packets {
				for _, change := range changes[i] {
					if err := applyMaskTransition(packet, change); err != nil {
						t.Fatal(err)
					}
				}
				index, err := indexMaskTransitionNodes(packet)
				if err != nil {
					t.Fatal(err)
				}
				item := index[key].correctionItem
				if key.maskIndex > 0 {
					item = index[key].maskItem
				}
				text, found, err := crsElementValue(unwrapRDFArrayItem(item), name)
				if err != nil || found != (c.Expected[i] != nil) {
					t.Fatalf("frame %d found=%t expected=%v err=%v", i, found, c.Expected[i], err)
				}
				if found {
					value, err := parseCameraRawBool(text)
					if err != nil || value != *c.Expected[i] {
						t.Fatalf("frame %d got %s want %t", i, text, *c.Expected[i])
					}
				}
			}
		})
	}
}

func TestMaskBooleanTransitionPreflightRejectsInvalidBeforeWrites(t *testing.T) {
	for _, bad := range []string{"invalid", "", "structured"} {
		t.Run(bad, func(t *testing.T) {
			frames := cropFixture(t, 3)
			for i, frame := range frames {
				packet := localMissingPacket(t, "CircularGradientBasedCorrections", nil, map[string]string{"Flipped": "True"})
				if i == 2 {
					property := packet.findRootProperty(crsNamespace, "CircularGradientBasedCorrections")
					correction := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
					mask := unwrapRDFArrayItem(rdfArrayItems(findCRSDescendant(correction, "CorrectionMasks"))[0])
					if bad == "structured" {
						mask.RemoveAttr("crs:Flipped")
						mask.CreateElement("crs:Flipped").CreateElement("crs:Unexpected")
					} else if err := setCRSElementValue(mask, "Flipped", bad); err != nil {
						t.Fatal(err)
					}
				}
				persistExposurePacket(t, frame, packet)
			}
			before, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := Main([]string{"interpolate", filepath.Dir(frames[0].Image), "--write"}, &out, &stderr); code == 0 {
				t.Fatal("accepted invalid boolean")
			}
			after, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed preflight wrote sidecar")
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
			if len(backups) != 0 {
				t.Fatal("failed preflight created backups")
			}
		})
	}
}

func TestMaskBooleanUnregisteredFlagsAndExistingSpellingSurvive(t *testing.T) {
	for _, tc := range []struct{ collection, flag string }{
		{"GradientBasedCorrections", "Flipped"},
		{"MaskGroupBasedCorrections", "MaskInverted"},
	} {
		t.Run(tc.collection+"/"+tc.flag, func(t *testing.T) {
			packets := make([]*XMPPacket, 3)
			for i := range packets {
				packets[i] = localMissingPacket(t, tc.collection, map[string]string{"CorrectionActive": "tRuE"}, map[string]string{tc.flag: []string{"True", "False", "True"}[i]})
			}
			changes, err := planMaskTransitions(packets, []int{1}, 50)
			if err != nil {
				t.Fatal(err)
			}
			for i, packet := range packets {
				for _, change := range changes[i] {
					if err := applyMaskTransition(packet, change); err != nil {
						t.Fatal(err)
					}
				}
				index, err := indexMaskTransitionNodes(packet)
				if err != nil {
					t.Fatal(err)
				}
				key := maskTransitionKey{collection: tc.collection, correctionIndex: 1}
				active, _, _ := crsElementValue(unwrapRDFArrayItem(index[key].correctionItem), "CorrectionActive")
				key.maskIndex = 1
				flag, _, _ := crsElementValue(unwrapRDFArrayItem(index[key].maskItem), tc.flag)
				if active != "tRuE" || flag != []string{"True", "False", "True"}[i] {
					t.Fatalf("frame %d active=%s flag=%s", i, active, flag)
				}
			}
		})
	}
	if got := maskBooleanTransitionProperties(maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionID: "id"}); len(got) != 0 {
		t.Fatalf("registered SyncID fallback: %v", got)
	}
}

func TestMaskBooleanTransitionCLIThroughVideo(t *testing.T) {
	frames := cropFixture(t, 3)
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 64})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		if err := os.WriteFile(frame.Image, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		active, flipped := "False", "True"
		if i == 1 {
			active, flipped = "True", "False"
		}
		packet := localMissingPacket(t, "MaskGroupBasedCorrections", map[string]string{"CorrectionActive": active, "LocalExposure2012": "1", "CorrectionSyncID": fmt.Sprint("keep-", i)}, nil)
		index, err := indexMaskTransitionNodes(packet)
		if err != nil {
			t.Fatal(err)
		}
		mask := index[maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionIndex: 1, maskIndex: 1}]
		if err := setCRSElementValue(unwrapRDFArrayItem(mask.maskItem), "Flipped", flipped); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
				t.Fatal(err)
			}
		}
		persistExposurePacket(t, frame, packet)
	}
	original, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	after, err := os.ReadFile(frames[0].OutputSidecar())
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preview wrote sidecar")
	}
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		index, err := indexMaskTransitionNodes(packet)
		if err != nil {
			t.Fatal(err)
		}
		correction := unwrapRDFArrayItem(index[maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionIndex: 1}].correctionItem)
		active, _, _ := crsElementValue(correction, "CorrectionActive")
		id, _, _ := crsElementValue(correction, "CorrectionSyncID")
		mask := unwrapRDFArrayItem(index[maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionIndex: 1, maskIndex: 1}].maskItem)
		flipped, _, _ := crsElementValue(mask, "Flipped")
		if active != "True" || flipped != "False" || id != fmt.Sprint("keep-", i) {
			t.Fatalf("frame %d active=%s flipped=%s id=%s", i, active, flipped, id)
		}
	}
	backups, _ := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup differs")
	}
	plan, err := buildPlan(frames)
	if err != nil || plan.PropertyCount() != 0 {
		t.Fatalf("repeat differs: %+v err=%v", plan, err)
	}
	output := filepath.Join(dir, "boolean.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := 255 * (1.055*math.Pow(2*math.Pow((64.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
	count := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 {
			t.Fatal("truncated JPEG")
		}
		frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		r, _, _, _ := frame.At(32, 16).RGBA()
		if math.Abs(float64(r>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d want %g", count, r>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != 3 {
		t.Fatalf("frames=%d", count)
	}
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(data, source.Bytes()) {
			t.Fatal("source modified")
		}
	}
}
