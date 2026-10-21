package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func FuzzRadialMaskTransform(f *testing.F) {
	f.Add([]byte{0, 45, 90, 127, 255}, uint8(3), uint8(60), uint8(40))
	f.Fuzz(func(t *testing.T, data []byte, countByte, widthByte, heightByte uint8) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}
		count := int(countByte%8) + 1
		shapes := make([]radialMaskShape, count)
		for i := range shapes {
			v := [5]float64{}
			for j := range v {
				v[j] = float64(data[(i*5+j)%len(data)])/255*4 - 2
			}
			shapes[i] = radialMaskShape{v[0], v[1], v[2], v[3], v[4] * 135, float64(int(widthByte)+1) * 100, float64(int(heightByte)+1) * 100}
			if shapes[i].width == 0 {
				shapes[i].width = 100
			}
			if shapes[i].height == 0 {
				shapes[i].height = 100
			}
		}
		before := append([]radialMaskShape(nil), shapes...)
		coordinates, offset, err := prepareRadialMasks(shapes)
		if err != nil {
			t.Fatal(err)
		}
		for i, c := range coordinates {
			s := shapes[i]
			r := restoreRadialMask(c, offset, s.width, s.height)
			for _, v := range []float64{r.top, r.bottom, r.left, r.right, r.angle, c.x, c.y, c.rx, c.ry, c.angle, offset} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatal("nonfinite radial geometry")
				}
			}
			if math.Abs((r.left+r.right)/2-c.x) > 1e-9 || math.Abs((r.top+r.bottom)/2-c.y) > 1e-9 {
				t.Fatal("radial transform moved its center")
			}
			dx, dy := (r.right-r.left)*s.width/2, (r.bottom-r.top)*s.height/2
			want := c.rx*c.rx + c.ry*c.ry
			if math.Abs(dx*dx+dy*dy-want) > math.Max(1, want)*1e-12 {
				t.Fatal("radial transform changed its diagonal length")
			}
			if r.angle < -45 || r.angle > 45 {
				t.Fatal("invalid restored angle")
			}
		}
		if !reflect.DeepEqual(before, shapes) {
			t.Fatal("radial preparation modified source geometry")
		}
		again, shift, err := prepareRadialMasks(shapes)
		if err != nil || shift != offset || !reflect.DeepEqual(coordinates, again) {
			t.Fatal("radial preparation is not deterministic", err)
		}
	})
}

func TestRadialMaskInvalidGeometry(t *testing.T) {
	for _, s := range []radialMaskShape{
		{0, 1, 0, 1, math.NaN(), 100, 100}, {0, 1, 0, 1, 0, 0, 100}, {0, 1, 0, 1, 0, 100, -1},
		{math.Inf(1), 1, 0, 1, 0, 100, 100}, {-math.MaxFloat64, math.MaxFloat64, 0, 1, 0, 100, 100},
	} {
		if _, _, err := prepareRadialMasks([]radialMaskShape{s}); err == nil {
			t.Fatal("accepted invalid radial geometry")
		}
	}
	if values, offset, err := prepareRadialMasks(nil); err != nil || len(values) != 0 || offset != 0 {
		t.Fatal("empty radial sequence", values, offset, err)
	}
}

func TestRadialMaskDimensionSources(t *testing.T) {
	row := radialPipelineObservations(t)[0]
	packet := radialPipelinePacket(t, row, 0, 0, true)
	if err := packet.SetScalar("tiff:ImageWidth", "12"); err != nil {
		t.Fatal(err)
	}
	if err := packet.SetScalar("tiff:ImageLength", "34"); err != nil {
		t.Fatal(err)
	}
	sizes, err := radialMaskFrameSizes([]*XMPPacket{packet}, nil)
	if err != nil || sizes[0] != (radialMaskSize{float64(row.Width), float64(row.Height)}) {
		t.Fatal("EXIF dimensions lost precedence", sizes, err)
	}
	if err := packet.RemoveProperty("exif:PixelXDimension"); err != nil {
		t.Fatal(err)
	}
	if err := packet.RemoveProperty("exif:PixelYDimension"); err != nil {
		t.Fatal(err)
	}
	sizes, err = radialMaskFrameSizes([]*XMPPacket{packet}, nil)
	if err != nil || sizes[0] != (radialMaskSize{12, 34}) {
		t.Fatal("TIFF dimensions not used", sizes, err)
	}
	for _, text := range []string{"0", "-1", "1.5", "NaN", "2147483648"} {
		if err := packet.SetScalar("exif:PixelXDimension", text); err != nil {
			t.Fatal(err)
		}
		if _, err := radialMaskFrameSizes([]*XMPPacket{packet}, nil); err == nil {
			t.Fatalf("accepted invalid radial dimension %s", text)
		}
	}
	unknown := radialPipelinePacket(t, row, 0, 0, false)
	sizes, err = radialMaskFrameSizes([]*XMPPacket{unknown}, []Frame{{Image: filepath.Join(t.TempDir(), "missing.nef")}})
	if err != nil || sizes[0] != (radialMaskSize{}) {
		t.Fatal("XMP-only dimensions invented", sizes, err)
	}
	index, err := indexMaskTransitionNodes(unknown)
	if err != nil {
		t.Fatal(err)
	}
	for key := range index {
		if radialTransitionKey(key) {
			series, active, err := radialTransitionSeries([]map[maskTransitionKey]maskTransitionNode{index}, []int{0}, key, sizes, 50)
			if err != nil || active || series != nil {
				t.Fatal("unknown dimensions enabled physical geometry", active, series, err)
			}
		}
	}
	// Do not inspect unused dimension metadata for incomplete selectors.
	angle := 0.0
	incomplete := maskAnglePacket(t, maskAngleObservation{Mode: 2, Input: &angle}, 0)
	if err := incomplete.SetScalar("exif:PixelXDimension", "invalid but unused"); err != nil {
		t.Fatal(err)
	}
	if _, err := planMaskTransitionsWithFormat([]*XMPPacket{incomplete, incomplete}, []int{0, 1}, 50, "modern"); err != nil {
		t.Fatal("unused dimensions blocked ordinary interpolation", err)
	}
}

func TestRadialMaskBatchIsolation(t *testing.T) {
	var cases []radialHelperObservation
	for _, row := range radialPipelineObservations(t) {
		if row.Width == 1000 && row.Smoothness == 50 && ((row.Slot == 1 && (row.Pattern == 0 || row.Pattern == 4)) || (row.Slot == 2 && (row.Pattern == 6 || row.Pattern == 9))) {
			cases = append(cases, row)
		}
	}
	if len(cases) != 4 {
		t.Fatal("missing independent radial sequence cases")
	}
	serialRoot, parallelRoot := t.TempDir(), t.TempDir()
	imageBytes := encodeNativeGray(t, nativeGraySource(16, 16, 8, 0, false), "png")
	for sequence, row := range cases {
		dir := filepath.Join(serialRoot, fmt.Sprintf("case%d", sequence))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for frame := range row.Input {
			path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", frame))
			if err := os.WriteFile(path, imageBytes, 0600); err != nil {
				t.Fatal(err)
			}
			packet := radialPipelinePacket(t, row, frame, sequence%3, true)
			data, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".xmp", data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for path, data := range batchFileSnapshot(t, serialRoot) {
		rel, err := filepath.Rel(serialRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(parallelRoot, rel)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	markerCommand(t, "batch-interpolate", serialRoot, "--workers", "1", "--write", "--no-backup")
	markerCommand(t, "batch-interpolate", parallelRoot, "--workers", "4", "--write", "--no-backup")
	serial, parallel := batchFileSnapshot(t, serialRoot), batchFileSnapshot(t, parallelRoot)
	if len(serial) != len(parallel) {
		t.Fatal("parallel radial processing changed file count")
	}
	for path, data := range serial {
		rel, err := filepath.Rel(serialRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		assertPreviewEVFileBytes(t, filepath.Join(parallelRoot, rel), data)
	}
	for sequence, row := range cases {
		frames, err := DiscoverSequence(filepath.Join(parallelRoot, fmt.Sprintf("case%d", sequence)))
		if err != nil {
			t.Fatal(err)
		}
		for i, frame := range frames {
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			assertRadialPacket(t, packet, row, i)
		}
	}
}

func TestRadialMaskCLISaveAndFailure(t *testing.T) {
	var row radialHelperObservation
	for _, candidate := range radialPipelineObservations(t) {
		if candidate.Slot == 1 && candidate.Width == 1000 && candidate.Pattern == 4 && candidate.Smoothness == 50 {
			row = candidate
			break
		}
	}
	if len(row.Input) != 7 {
		t.Fatal("missing square rotation regression")
	}
	imageBytes := encodeNativeGray(t, nativeGraySource(1000, 1000, 8, 0, false), "png")
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("broken%v", broken), func(t *testing.T) {
			dir := t.TempDir()
			for i := range row.Input {
				path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
				if err := os.WriteFile(path, imageBytes, 0600); err != nil {
					t.Fatal(err)
				}
				packet := radialPipelinePacket(t, row, i, 0, false)
				if broken && i == 6 {
					if err := packet.SetScalar("exif:PixelXDimension", "not-an-integer"); err != nil {
						t.Fatal(err)
					}
				}
				data, err := packet.Serialize()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".xmp", data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			if broken {
				var out, stderr bytes.Buffer
				if Main([]string{"interpolate", dir, "--write"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "PixelXDimension") {
					t.Fatal("accepted invalid radial dimensions", stderr.String())
				}
				for path, data := range before {
					assertPreviewEVFileBytes(t, path, data)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 14 {
					t.Fatal("failed radial interpolation wrote files", entries, err)
				}
				return
			}
			markerCommand(t, "interpolate", dir)
			for path, data := range before {
				assertPreviewEVFileBytes(t, path, data)
			}
			render := func(name string) [][]byte {
				path := filepath.Join(t.TempDir(), name+".avi")
				markerCommand(t, "render", dir, "--apply-xmp-basic", "--output-height", "32", "--quality", "100", "--output", path)
				data := denoiseRead(t, path)
				var frames [][]byte
				for {
					start := bytes.Index(data, []byte{255, 216})
					if start < 0 {
						break
					}
					data = data[start:]
					end := bytes.Index(data, []byte{255, 217})
					if end < 0 {
						t.Fatal("truncated radial AVI")
					}
					payload := bytes.Clone(data[:end+2])
					config, err := jpeg.DecodeConfig(bytes.NewReader(payload))
					if err != nil || config.Width != 32 || config.Height != 32 {
						t.Fatal("invalid radial AVI frame", config, err)
					}
					frames = append(frames, payload)
					data = data[end+2:]
				}
				if len(frames) != 7 {
					t.Fatalf("radial AVI frame count%d", len(frames))
				}
				return frames
			}
			originalVideo := render("before")
			markerCommand(t, "interpolate", dir, "--write")
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for i, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				assertRadialPacket(t, packet, row, i)
				corrections, err := readXMPLocalCorrections(packet)
				if err != nil || len(corrections) != 1 || len(corrections[0].masks) != 1 || corrections[0].masks[0].kind != localMaskCircularGradient {
					t.Fatal("restored radial mask is not renderable", corrections, err)
				}
				assertPreviewEVFileBytes(t, frame.Image, imageBytes)
				backups := 0
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), filepath.Base(frame.OutputSidecar())+".") && strings.HasSuffix(entry.Name(), ".bak") {
						backups++
						assertPreviewEVFileBytes(t, filepath.Join(dir, entry.Name()), before[frame.OutputSidecar()])
					}
				}
				if i%3 == 0 {
					if backups != 0 {
						t.Fatal("unchanged keyframe was backed up")
					}
					assertPreviewEVFileBytes(t, frame.OutputSidecar(), before[frame.OutputSidecar()])
				} else if backups != 1 {
					t.Fatalf("frame%d backup count%d", i, backups)
				}
			}
			updatedVideo := render("after")
			changed := 0
			for i := range originalVideo {
				same := bytes.Equal(originalVideo[i], updatedVideo[i])
				if i%3 == 0 && !same {
					t.Fatal("radial interpolation changed a keyframe's video pixels")
				}
				if i%3 != 0 && !same {
					changed++
				}
			}
			if changed == 0 {
				t.Fatal("radial interpolation did not change intermediate AVI pixels")
			}
			after := batchFileSnapshot(t, dir)
			markerCommand(t, "interpolate", dir, "--write")
			for path, data := range after {
				assertPreviewEVFileBytes(t, path, data)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				for path, data := range batchFileSnapshot(t, dir) {
					previous, found := after[path]
					if !found {
						t.Log("new after repeat", filepath.Base(path))
					} else if !bytes.Equal(previous, data) {
						t.Log("changed after repeat", filepath.Base(path))
					}
				}
				plan, err := buildPlanWithSmoothness(frames, 50)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range plan.Changes {
					t.Logf("repeat plan %s root%v masks%+v", filepath.Base(change.Frame.Image), change.Values, change.MaskTransitions)
				}
				t.Fatal("repeat radial interpolation created or changed files")
			}
		})
	}
}

func TestRadialPreciseTrig(t *testing.T) {
	for _, row := range []struct {
		x        float64
		sin, cos uint64
	}{
		{0x1.8f66546883cfap-1, 0x3fe681b7834429f7, 0x3fe6bf5b4f21b73d},
		{0x1.94d9162001d36p-1, 0x3fe6bf5b4f21b73d, 0x3fe681b7834429f8},
	} {
		sin, cos := radialExactSinCos(row.x)
		if math.Float64bits(sin) != row.sin || math.Float64bits(cos) != row.cos {
			t.Fatalf("precise trig %x/%x", math.Float64bits(sin), math.Float64bits(cos))
		}
	}
}

type radialHelperObservation struct {
	Ratings                                        []int
	Smoothness                                     int
	Mode, Slot, Width, Height, Pattern             int
	NativePrefix, RegisteredPrefix                 string
	NativeAngleIndex, RegisteredAngleIndex         int
	Offset                                         float64
	Input, Prepared, Restored                      [][]float64
	PreparedHelpers, GivenHelpers, RestoredHelpers [][]*float64
}

func radialPipelineObservations(t *testing.T) []radialHelperObservation {
	t.Helper()
	var rows []radialHelperObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-radial-pipeline-7.5.5.json"), &rows); err != nil || len(rows) != 180 {
		t.Fatalf("expected 180 radial component pipeline observations: %v", err)
	}
	return rows
}

func radialPipelinePacket(t *testing.T, row radialHelperObservation, frame, form int, dimensions bool) *XMPPacket {
	t.Helper()
	var tree strings.Builder
	slot := row.Slot + 7
	tree.WriteString(`<crs:MaskGroupBasedCorrections><rdf:Seq>`)
	for i := 1; i < slot; i++ {
		tree.WriteString(`<rdf:li rdf:parseType="Resource"/>`)
	}
	fmt.Fprintf(&tree, `<rdf:li rdf:parseType="Resource" crs:What="Correction" crs:CorrectionSyncID="own-radial-%d" crs:CorrectionName="preserve name" crs:LocalExposure2012="0.25"><crs:CorrectionMasks><rdf:Seq>`, slot)
	names := []string{"Top", "Bottom", "Left", "Right", "Angle"}
	if form == 0 {
		tree.WriteString(`<rdf:li><rdf:Description crs:What="Mask/CircularGradient" crs:MaskSyncID="own-mask" crs:Feather="50"`)
		for j, name := range names {
			fmt.Fprintf(&tree, ` crs:%s="%.17g"`, name, row.Input[frame][j])
		}
		tree.WriteString(`/></rdf:li>`)
	} else {
		if form == 1 {
			tree.WriteString(`<rdf:li rdf:parseType="Resource">`)
		} else {
			tree.WriteString(`<rdf:li><rdf:Description>`)
		}
		tree.WriteString(`<crs:What>Mask/CircularGradient</crs:What><crs:MaskSyncID>own-mask</crs:MaskSyncID><crs:Feather>50</crs:Feather>`)
		for j, name := range names {
			fmt.Fprintf(&tree, `<crs:%s>%.17g</crs:%s>`, name, row.Input[frame][j], name)
		}
		if form == 1 {
			tree.WriteString(`</rdf:li>`)
		} else {
			tree.WriteString(`</rdf:Description></rdf:li>`)
		}
	}
	tree.WriteString(`</rdf:Seq></crs:CorrectionMasks></rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections>`)
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">` + tree.String() + `</rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	rating := 0
	if frame%3 == 0 {
		rating = 4
	}
	if len(row.Ratings) == len(row.Input) {
		rating = row.Ratings[frame]
	}
	if rating > 0 {
		if err := packet.SetScalar("xmp:Rating", fmt.Sprint(rating)); err != nil {
			t.Fatal(err)
		}
	}
	if dimensions {
		for name, value := range map[string]int{"exif:PixelXDimension": row.Width, "exif:PixelYDimension": row.Height} {
			if err := packet.SetScalar(name, fmt.Sprint(value)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := packet.SetScalar("dc:description", "preserve unrelated metadata"); err != nil {
		t.Fatal(err)
	}
	return packet
}

func assertRadialPacket(t *testing.T, packet *XMPPacket, row radialHelperObservation, frame int) {
	t.Helper()
	index, err := indexMaskTransitionNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]float64
	for key, node := range index {
		if key.collection == "MaskGroupBasedCorrections" && key.correctionIndex == row.Slot+7 && key.maskIndex == 1 {
			values = node.values
		}
	}
	if values == nil {
		t.Fatal("missing restored radial mask")
	}
	for j, name := range []string{"Top", "Bottom", "Left", "Right", "Angle"} {
		if v, found := values[name]; !found || math.Abs(v-row.Restored[frame][j]) > 2e-9 {
			t.Fatalf("frame%d %s %.15g want%.15g", frame, name, v, row.Restored[frame][j])
		}
	}
	if got, _, err := packet.GetScalar("dc:description"); err != nil || got != "preserve unrelated metadata" {
		t.Fatal("changed unrelated metadata", got, err)
	}
	xml, err := packet.Document.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	for _, helper := range []string{"lrt:CX", "lrt:CY", "lrt:RX", "lrt:RY", "lrt:AN"} {
		if strings.Contains(xml, helper) {
			t.Fatalf("leaked transient helper %s", helper)
		}
	}
}

func TestRadialMaskNativePipeline(t *testing.T) {
	testRadialPipelines(t, radialPipelineObservations(t))
}

func TestRadialMaskNativeMixedMarks(t *testing.T) {
	var rows []radialHelperObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-radial-mixed-7.5.5.json"), &rows); err != nil || len(rows) != 180 {
		t.Fatalf("expected 180 mixed-mark radial pipelines: %v", err)
	}
	for _, row := range rows {
		if !reflect.DeepEqual(row.Ratings, []int{4, 0, 5, 4, 2, 0, 4}) {
			t.Fatal("unexpected native mark configuration")
		}
	}
	testRadialPipelines(t, rows)
}

func testRadialPipelines(t *testing.T, rows []radialHelperObservation) {
	for _, row := range rows {
		for form := 0; form < 3; form++ {
			t.Run(fmt.Sprintf("slot%d/%dx%d/p%d/s%d/rdf%d", row.Slot, row.Width, row.Height, row.Pattern, row.Smoothness, form), func(t *testing.T) {
				packets := make([]*XMPPacket, len(row.Input))
				for i := range packets {
					packets[i] = radialPipelinePacket(t, row, i, form, true)
				}
				changes, err := planMaskTransitionsWithFormat(packets, []int{0, 3, 6}, row.Smoothness, "modern")
				if err != nil {
					t.Fatal(err)
				}
				for i, planned := range changes {
					for _, change := range planned {
						if err := applyMaskTransition(packets[i], change); err != nil {
							t.Fatal(err)
						}
					}
					assertRadialPacket(t, packets[i], row, i)
				}
			})
		}
	}
}

func radialHelperObservations(t *testing.T) []radialHelperObservation {
	t.Helper()
	var rows []radialHelperObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-radial-helper-7.5.5.json"), &rows); err != nil || len(rows) != 120 {
		t.Fatalf("expected 120 radial helper observations: %v", err)
	}
	return rows
}

func TestRadialMaskNativeHelper(t *testing.T) {
	for _, row := range radialHelperObservations(t) {
		t.Run(fmt.Sprintf("mode%d/slot%d/%dx%d/p%d", row.Mode, row.Slot, row.Width, row.Height, row.Pattern), func(t *testing.T) {
			if row.Mode == 1 {
				if row.NativeAngleIndex != -1 || row.RegisteredAngleIndex < 0 {
					t.Fatal("unexpected legacy path observation")
				}
				for i := range row.Input {
					for j := 0; j < 5; j++ {
						if row.Input[i][j] != row.Prepared[i][j] || row.Input[i][j] != row.Restored[i][j] || row.PreparedHelpers[i][j] != nil {
							t.Fatal("legacy helper unexpectedly changed a value")
						}
					}
				}
				return
			}
			var shapes []radialMaskShape
			for _, i := range []int{0, 3, 6} {
				v := row.Input[i]
				shapes = append(shapes, radialMaskShape{v[0], v[1], v[2], v[3], v[4], float64(row.Width), float64(row.Height)})
			}
			coordinates, offset, err := prepareRadialMasks(shapes)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(offset-row.Offset) > 1e-8 {
				t.Fatalf("offset %.15g want%.15g", offset, row.Offset)
			}
			for anchor, c := range coordinates {
				for j, v := range []float64{c.x, c.y, c.rx, c.ry, c.angle} {
					want := row.PreparedHelpers[anchor*3][j]
					if want == nil || math.Abs(v-*want) > 1e-8 {
						t.Fatalf("prepared anchor%d field%d %.15g want%v", anchor, j, v, want)
					}
				}
			}
			for _, i := range []int{1, 2, 4, 5} {
				aux := row.GivenHelpers[i]
				c := radialMaskCoordinates{*aux[0], *aux[1], *aux[2], *aux[3], *aux[4]}
				r := restoreRadialMask(c, row.Offset, float64(row.Width), float64(row.Height))
				for j, v := range []float64{r.top, r.bottom, r.left, r.right, r.angle} {
					if math.Abs(v-row.Restored[i][j]) > 1e-10 {
						t.Fatalf("restored field%d %.15g want%.15g", j, v, row.Restored[i][j])
					}
				}
			}
		})
	}
}
