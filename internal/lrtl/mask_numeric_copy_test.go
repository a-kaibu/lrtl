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
	"strconv"
	"strings"
	"testing"
)

func TestMaskNumericCopyAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-local-numeric-copy.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Property        string
			Input, Expected []float64
			Ratings         []int
			Copied          bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 28 {
		t.Fatal("incomplete installed component fixture")
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			packets := make([]*XMPPacket, len(c.Input))
			indexes := make([]map[maskTransitionKey]maskTransitionNode, len(c.Input))
			name := c.Property[strings.LastIndex(c.Property, ":")+1:]
			key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}
			if strings.Contains(c.Property, "CorrectionMasks") {
				key.maskIndex = 1
			}
			for i, value := range c.Input {
				local, geometry := map[string]string{}, map[string]string{}
				if key.maskIndex == 1 {
					geometry[name] = strconv.FormatFloat(value, 'g', -1, 64)
				} else {
					local[name] = strconv.FormatFloat(value, 'g', -1, 64)
				}
				packets[i] = localMissingPacket(t, key.collection, local, geometry)
				if err := packets[i].SetScalar("xmp:Rating", fmt.Sprint(c.Ratings[i])); err != nil {
					t.Fatal(err)
				}
				indexes[i], err = indexMaskTransitionNodes(packets[i])
				if err != nil {
					t.Fatal(err)
				}
			}
			keys, _, _ := transitionKeyframeGroups(packets)
			prepared := prepareMaskTransitionInputs(indexes)
			copies := maskNumericCopyReferences(keys, prepared.Indexes, key)
			value, copied := copies[name]
			if copied != c.Copied || (copied && math.Float64bits(value) != math.Float64bits(c.Expected[0])) {
				t.Fatalf("gate=%t value=%g want gate=%t value=%g", copied, value, c.Copied, c.Expected[0])
			}
			if !copied {
				return
			}
			for _, smoothness := range []int{0, 50, 100} {
				changes, err := planMaskTransitions(packets, keys, smoothness)
				if err != nil {
					t.Fatal(err)
				}
				for i, packet := range packets {
					serialized, err := packet.Serialize()
					if err != nil {
						t.Fatal(err)
					}
					result, err := parseXMP(serialized)
					if err != nil {
						t.Fatal(err)
					}
					for _, change := range changes[i] {
						if err := applyMaskTransition(result, change); err != nil {
							t.Fatal(err)
						}
					}
					index, err := indexMaskTransitionNodes(result)
					if err != nil {
						t.Fatal(err)
					}
					got, found := index[key].values[name]
					if !found || math.Float64bits(got) != math.Float64bits(c.Expected[i]) {
						t.Fatalf("smoothness %d frame %d got %g found=%t want %g", smoothness, i, got, found, c.Expected[i])
					}
				}
			}
		})
	}
}

func TestMaskNumericCopyPreservesScopeAndClonesNonKeyResources(t *testing.T) {
	for _, collection := range maskTransitionCollections {
		t.Run(collection, func(t *testing.T) {
			packet := localMissingPacket(t, collection, map[string]string{"LocalExposure2012": "6", "CorrectionSyncID": "source"}, nil)
			packets := []*XMPPacket{packet, EmptyXMPPacket()}
			changes, err := planMaskTransitions(packets, []int{0}, 50)
			if err != nil {
				t.Fatal(err)
			}
			for _, change := range changes[1] {
				if err := applyMaskTransition(packets[1], change); err != nil {
					t.Fatal(err)
				}
			}
			index, err := indexMaskTransitionNodes(packets[1])
			if err != nil {
				t.Fatal(err)
			}
			key := maskTransitionKey{collection: collection, correctionIndex: 1}
			if index[key].values["LocalExposure2012"] != 6 {
				t.Fatal("copy clamped while cloning missing non-key resource")
			}
			for _, excluded := range []maskTransitionKey{{collection: collection, correctionID: "source"}, {collection: collection, correctionIndex: 99}, {collection: collection, correctionIndex: 1, maskIndex: 2}} {
				if len(maskNumericCopyReferences([]int{0}, []map[maskTransitionKey]maskTransitionNode{{excluded: index[key]}, {excluded: index[key]}}, excluded)) != 0 {
					t.Fatalf("copied unregistered key: %+v", excluded)
				}
			}
		})
	}
}

func TestMaskNumericCopyCLIThroughVideo(t *testing.T) {
	frames := cropFixture(t, 3)
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 16})
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
		value := "0"
		if i == 1 {
			value = "6"
		}
		packet := localMissingPacket(t, "GradientBasedCorrections", map[string]string{"LocalExposure2012": value, "CorrectionSyncID": fmt.Sprint("original-", i)}, nil)
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
	key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		index, err := indexMaskTransitionNodes(packet)
		if err != nil {
			t.Fatal(err)
		}
		if index[key].values["LocalExposure2012"] != 6 {
			t.Fatal("copy clamped")
		}
		id, _, _ := crsElementValue(unwrapRDFArrayItem(index[key].correctionItem), "CorrectionSyncID")
		if id != fmt.Sprint("original-", i) {
			t.Fatal("replaced target correction ID")
		}
	}
	backups, err := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup differs")
	}
	plan, err := buildPlan(frames)
	if err != nil || plan.PropertyCount() != 0 {
		t.Fatalf("repeat differs: %+v err=%v", plan, err)
	}
	output := filepath.Join(dir, "local-copy.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := 255 * (1.055*math.Pow(64*math.Pow((16.0/255+.055)/1.055, 2.4), 1/2.4) - .055)
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
