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
	"strings"
	"testing"
)

func pointCurveCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, stderr bytes.Buffer
	if code := Main(append([]string{"curve"}, args...), &out, &stderr); code != 0 {
		t.Fatalf("curve %v: %d %s", args, code, &stderr)
	}
	return out.String()
}

func TestPointCurveCLIInspectEditBackupAndNoop(t *testing.T) {
	frame := cropFixture(t, 1)[0]
	original, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	imageBefore, err := os.ReadFile(frame.Image)
	if err != nil {
		t.Fatal(err)
	}
	var rows []pointCurveInfo
	if err := json.Unmarshal([]byte(pointCurveCLI(t, frame.Image, "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Found || !rows[0].Editable || len(rows[0].Points) != 2 {
		t.Fatalf("inspection=%+v", rows)
	}
	args := []string{frame.Image, "--points", "0,0;128,64;255,255", "--mark"}
	preview := pointCurveCLI(t, append(args, "--dry-run")...)
	if !strings.Contains(preview, "crs:ToneCurvePV2012 points=0,0;128,64;255,255") {
		t.Fatalf("incomplete preview: %s", preview)
	}
	after, err := os.ReadFile(frame.Sidecar)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("dry run changed XMP")
	}
	pointCurveCLI(t, args...)
	packet, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	target, err := resolvePointCurveTarget(packet, "master", "")
	if err != nil {
		t.Fatal(err)
	}
	points, found, editable, err := readPointCurveTarget(target)
	if err != nil || !found || !editable || len(points) != 3 || points[1] != (toneCurvePoint{128, 64}) {
		t.Fatalf("points=%v %v", points, err)
	}
	if target.property.element == nil || toneCurveSequenceElement(target.property.element) == nil {
		t.Fatal("new curve was not an RDF Seq")
	}
	if rating, _ := packet.Rating(); rating != 4 {
		t.Fatal("did not mark develop keyframe")
	}
	if exposure, _, _ := packet.GetScalar("crs:Exposure2012"); exposure != "0.75" {
		t.Fatal("unrelated value changed")
	}
	backups, _ := filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup differs from original")
	}
	pointCurveCLI(t, args...)
	backups, _ = filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 1 {
		t.Fatal("identical edit created backup")
	}
	pointCurveCLI(t, frame.Sidecar, "--linear", "--no-backup")
	if !strings.Contains(pointCurveCLI(t, frame.Image), "points=0,0;255,255") {
		t.Fatal("linear reset failed")
	}
	imageAfter, err := os.ReadFile(frame.Image)
	if err != nil || !bytes.Equal(imageAfter, imageBefore) {
		t.Fatal("source image changed")
	}
}

func TestPointCurveChannelsAndExistingRepresentations(t *testing.T) {
	for _, channel := range []string{"master", "red", "green", "blue"} {
		for _, kind := range []string{"attribute", "text", "sequence", "legacy"} {
			t.Run(channel+"/"+kind, func(t *testing.T) {
				primary, fallback, _ := toneCurveChannelProperties(channel)
				name := primary
				if kind == "legacy" {
					name = fallback
				}
				property := fmt.Sprintf(`<raw:%s>0, 0 255, 255</raw:%s>`, name, name)
				attribute := ""
				if kind == "attribute" {
					property = ""
					attribute = fmt.Sprintf(`raw:%s="0, 0 255, 255"`, name)
				}
				if kind == "sequence" {
					property = fmt.Sprintf(`<raw:%s><r:Seq vendor:Keep="container"><r:li>0, 0</r:li><r:li>255, 255</r:li></r:Seq></raw:%s>`, name, name)
				}
				xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><r:RDF xmlns:r="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><r:Description xmlns:raw="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:rdf="urn:lrtl:vendor-rdf" xmlns:crs="urn:lrtl:vendor-crs" xmlns:vendor="urn:lrtl:vendor" %s><vendor:Note>keep</vendor:Note>%s</r:Description></r:RDF></x:xmpmeta>`, attribute, property)
				path := filepath.Join(t.TempDir(), "only.xmp")
				if err := os.WriteFile(path, []byte(xml), 0600); err != nil {
					t.Fatal(err)
				}
				pointCurveCLI(t, path, "--channel", channel, "--points", "0,0;128,80;255,255", "--no-backup")
				packet, err := PacketForSidecar(path)
				if err != nil {
					t.Fatal(err)
				}
				target, err := resolvePointCurveTarget(packet, channel, "")
				if err != nil {
					t.Fatal(err)
				}
				points, found, editable, err := readPointCurveTarget(target)
				if err != nil || !found || !editable || len(points) != 3 || points[1].y != 80 || target.name != name {
					t.Fatalf("curve=%v target=%+v err=%v", points, target, err)
				}
				if kind == "attribute" && target.property.attribute == nil {
					t.Fatal("attribute representation changed")
				}
				if kind == "text" {
					if _, scalar := scalarElementText(target.property.element); !scalar {
						t.Fatal("text representation changed")
					}
				}
				data, _ := packet.Serialize()
				if !bytes.Contains(data, []byte("keep")) {
					t.Fatal("vendor data lost")
				}
				if kind == "sequence" && !bytes.Contains(data, []byte(`Keep="container"`)) {
					t.Fatal("sequence attribute lost")
				}
			})
		}
	}
}

func TestPointCurveBadOptionsDoNotWrite(t *testing.T) {
	frame := cropFixture(t, 1)[0]
	original, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--points", ""}, {"--points", "0,0;128,64"}, {"--points", "1,0;255,255"}, {"--points", "0,0;128,64;128,80;255,255"}, {"--points", "0,0;128.5,80;255,255"}, {"--points", "0,0;255,256"}, {"--points", "0,0;NaN,80;255,255"}, {"--points", strings.Repeat("0,0;", 2000)}, {"--linear", "--points", "0,0;255,255"}, {"--channel", "invalid"}, {"--linear", "--json"}, {"--mark"}, {"--dry-run"}, {"--no-backup"}, {"--mask", "MaskGroupBasedCorrections[1].CorrectionMasks[1]", "--linear"}} {
		var out, stderr bytes.Buffer
		if code := Main(append([]string{"curve", frame.Image}, args...), &out, &stderr); code == 0 {
			t.Fatalf("accepted %v", args)
		}
		after, err := os.ReadFile(frame.Sidecar)
		if err != nil || !bytes.Equal(after, original) {
			t.Fatal("invalid edit changed XMP")
		}
	}
	backups, _ := filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 0 {
		t.Fatal("invalid input created backup")
	}
}

func TestPointCurveDirectoryPreflightAndHGProtection(t *testing.T) {
	frames := cropFixture(t, 2)
	original, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	xml := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"><crs:ToneCurvePV2012><rdf:Bag><rdf:li>0,0</rdf:li><rdf:li>255,255</rdf:li></rdf:Bag></crs:ToneCurvePV2012></rdf:Description></rdf:RDF></x:xmpmeta>`
	if err := os.WriteFile(frames[1].Sidecar, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"curve", filepath.Dir(frames[0].Image), "--points", "0,0;128,80;255,255"}, &out, &stderr); code == 0 {
		t.Fatal("overwrote unsupported later representation")
	}
	after, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("preflight wrote earlier file")
	}
	writeTestXMP(t, frames[1].Image, map[string]string{"xmp:Rating": "2"})
	if code := Main([]string{"curve", frames[1].Image, "--linear", "--mark"}, &out, &stderr); code == 0 {
		t.Fatal("overwrote HG mark")
	}
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := readXMPToneCurve(packet, "ToneCurvePV2012"); found {
		t.Fatal("failed HG marking left a partial curve")
	}
	if code := Main([]string{"curve", filepath.Dir(frames[0].Image), "--linear", "--mark"}, &out, &stderr); code == 0 {
		t.Fatal("marked a whole directory")
	}
}

func TestPointCurveLocalTargetsAndInternalProtection(t *testing.T) {
	for _, collection := range []string{"MaskGroupBasedCorrections", "GradientBasedCorrections"} {
		frame := cropFixture(t, 1)[0]
		packet := localMissingPacket(t, collection, nil, nil)
		persistExposurePacket(t, frame, packet)
		mask := collection + "[1]"
		for _, channel := range []string{"master", "red", "green", "blue"} {
			pointCurveCLI(t, frame.Image, "--mask", mask, "--channel", channel, "--points", "0,0;128,80;255,255", "--no-backup")
			var rows []pointCurveInfo
			if err := json.Unmarshal([]byte(pointCurveCLI(t, frame.Image, "--mask", mask, "--channel", channel, "--json")), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || !rows[0].Found || rows[0].Equalized != (collection == "MaskGroupBasedCorrections") {
				t.Fatalf("local info=%+v", rows)
			}
		}
	}
	frame := cropFixture(t, 1)[0]
	packet := exposureTestPacket(t, "modern")
	persistExposurePacket(t, frame, packet)
	var out, stderr bytes.Buffer
	if code := Main([]string{"curve", frame.Image, "--mask", "MaskGroupBasedCorrections[1]", "--linear"}, &out, &stderr); code == 0 {
		t.Fatal("edited internal HG layer")
	}
}

func TestPointCurveCLIReferenceEqualizationThroughAVIPixels(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprint(local), func(t *testing.T) {
			frames := cropFixture(t, 3)
			img := image.NewGray(image.Rect(0, 0, 32, 16))
			for y := 0; y < 16; y++ {
				for x := 0; x < 32; x++ {
					img.SetGray(x, y, color.Gray{Y: 128})
				}
			}
			var source bytes.Buffer
			if err := png.Encode(&source, img); err != nil {
				t.Fatal(err)
			}
			for _, frame := range frames {
				if err := os.WriteFile(frame.Image, source.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				packet := EmptyXMPPacket()
				if local {
					packet = localMissingPacket(t, "MaskGroupBasedCorrections", nil, nil)
				}
				persistExposurePacket(t, frame, packet)
			}
			args := []string{frames[1].Image, "--points", "0,0;128,64;255,255", "--mark"}
			if local {
				args = append(args, "--mask", "MaskGroupBasedCorrections[1]")
			}
			pointCurveCLI(t, args...)
			var out, stderr bytes.Buffer
			dir := filepath.Dir(frames[0].Image)
			if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
				t.Fatalf("equalization: %s", &stderr)
			}
			output := filepath.Join(dir, "curve.avi")
			if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
				t.Fatalf("render: %s", &stderr)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
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
				result, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
				if err != nil {
					t.Fatal(err)
				}
				red, _, _, _ := result.At(16, 8).RGBA()
				if math.Abs(float64(red>>8)-64) > 2 {
					t.Fatalf("frame %d red=%d want64", count, red>>8)
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
					t.Fatal("source changed")
				}
			}
		})
	}
}

func TestPointCurveEmbeddedXMPAndDuplicateProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embedded.jpg")
	packet := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:v="urn:lrtl:vendor" crs:ToneCurvePV2012="0,0 255,255"><v:Keep>original</v:Keep></rdf:Description></rdf:RDF></x:xmpmeta>`)
	original := writeEmbeddedTransitionJPEG(t, path, packet)
	pointCurveCLI(t, path, "--points", "0,0;128,80;255,255")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("embedded original changed")
	}
	data, err := os.ReadFile(path + ".xmp")
	if err != nil || !bytes.Contains(data, []byte("original")) {
		t.Fatal("unknown embedded data lost")
	}
	duplicate := bytes.Replace(packet, []byte(`<v:Keep>`), []byte(`<crs:ToneCurvePV2012>0,0 255,255</crs:ToneCurvePV2012><v:Keep>`), 1)
	if err := os.WriteFile(path+".xmp", duplicate, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"curve", path, "--linear"}, &out, &stderr); code == 0 {
		t.Fatal("edited ambiguous duplicate curve")
	}
}

func TestPointCurveLimitsAndDirectorySuccess(t *testing.T) {
	for _, count := range []int{64, 65} {
		pairs := make([]string, count)
		for i := range pairs {
			x := i * 4
			if i == count-1 {
				x = 255
			}
			pairs[i] = fmt.Sprintf("%d,%d", x, x)
		}
		points, err := parsePointCurveEdit(strings.Join(pairs, ";"))
		if count == 64 && (err != nil || len(points) != 64) {
			t.Fatalf("64 points: %v", err)
		}
		if count == 65 && err == nil {
			t.Fatal("accepted 65 points")
		}
	}
	if _, err := parsePointCurveEdit("0,255;255,0"); err != nil {
		t.Fatalf("decreasing y curve: %v", err)
	}
	frames := cropFixture(t, 3)
	dir := filepath.Dir(frames[0].Image)
	pointCurveCLI(t, dir, "--channel", "red", "--points", "0,0;128,80;255,255", "--no-backup")
	var rows []pointCurveInfo
	if err := json.Unmarshal([]byte(pointCurveCLI(t, dir, "--channel", "red", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
	for _, row := range rows {
		if !row.Found || row.Points[1].Y != 80 || !row.Equalized {
			t.Fatalf("row=%+v", row)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(backups) != 0 {
		t.Fatal("no-backup ignored")
	}
	pointCurveCLI(t, "--help")
}
