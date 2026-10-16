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

func cropFixture(t *testing.T, count int) []Frame {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		writeRenderFrame(t, path, "png", 64, 32)
		writeTestXMP(t, path, map[string]string{"crs:HasCrop": "False", "crs:Exposure2012": "0.75", "xmp:Rating": "1"})
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}

func cropCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, stderr bytes.Buffer
	if code := Main(append([]string{"crop"}, args...), &out, &stderr); code != 0 {
		t.Fatalf("crop %v: exit=%d %s", args, code, &stderr)
	}
	return out.String()
}

func TestCropCLIStoresCompleteEditAndOneOriginalBackup(t *testing.T) {
	frame := cropFixture(t, 1)[0]
	original, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	imageBefore, err := os.ReadFile(frame.Image)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{frame.Image, "--rect", "0.1,0.2,0.9,0.8", "--angle", "-10", "--mark"}
	cropCLI(t, append(args, "--dry-run")...)
	after, err := os.ReadFile(frame.Sidecar)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("dry run changed XMP")
	}
	cropCLI(t, args...)
	packet, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	crop, err := readCropMetadata(packet)
	if err != nil {
		t.Fatal(err)
	}
	want := cropMetadata{Left: .1, Top: .2, Right: .9, Bottom: .8, Angle: -10, Active: true}
	if crop != want {
		t.Fatalf("crop=%+v want %+v", crop, want)
	}
	if rating, valid := packet.Rating(); !valid || rating != 5 {
		t.Fatal("missing crop keyframe")
	}
	if kind, _, _ := packet.GetScalar("ol:KeyframeKind"); kind != "crop" {
		t.Fatalf("kind=%s", kind)
	}
	if exposure, _, _ := packet.GetScalar("crs:Exposure2012"); exposure != "0.75" {
		t.Fatal("unrelated exposure changed")
	}
	backups, err := filepath.Glob(frame.Sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup is not the original packet")
	}
	// An identical command must not create another backup or rewrite the packet.
	cropCLI(t, args...)
	backups, _ = filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 1 {
		t.Fatal("no-op created another backup")
	}
	cropCLI(t, frame.Sidecar, "--clear", "--no-backup")
	packet, err = PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	crop, err = readCropMetadata(packet)
	if err != nil || crop != (cropMetadata{Right: 1, Bottom: 1}) {
		t.Fatalf("clear=%+v err=%v", crop, err)
	}
	if rating, _ := packet.Rating(); rating != 5 {
		t.Fatal("clear removed keyframe rating")
	}
	imageAfter, err := os.ReadFile(frame.Image)
	if err != nil || !bytes.Equal(imageBefore, imageAfter) {
		t.Fatal("crop changed image")
	}
}

func TestCropCLIPartialEditAndInspection(t *testing.T) {
	frame := cropFixture(t, 1)[0]
	cropCLI(t, frame.Sidecar, "--rect", "0.1,0.1,0.9,0.9", "--angle", "5", "--no-backup")
	cropCLI(t, "--angle", "0", frame.Image, "--no-backup")
	var rows []struct {
		Image string       `json:"image"`
		Crop  cropMetadata `json:"crop"`
	}
	if err := json.Unmarshal([]byte(cropCLI(t, frame.Image, "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Crop.Angle != 0 || rows[0].Crop.Left != .1 || !rows[0].Crop.Active {
		t.Fatalf("partial update/JSON=%+v", rows)
	}
	cropCLI(t, frame.Image, "--clear", "--no-backup")
	cropCLI(t, frame.Image, "--enable", "--no-backup")
	if !strings.Contains(cropCLI(t, frame.Image), "active=true rect=0,0,1,1 angle=0") {
		t.Fatal("enable changed full-frame defaults")
	}
	backups, _ := filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 0 {
		t.Fatal("--no-backup ignored")
	}
}

func TestCropCLIRejectsInvalidEditsWithoutMutation(t *testing.T) {
	frame := cropFixture(t, 1)[0]
	original, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		{"--rect", ""}, {"--rect", "0,0,1"}, {"--rect", "0,0,1,1,1"}, {"--rect", "0,0,x,1"},
		{"--rect", "NaN,0,1,1"}, {"--rect", "0,0,Inf,1"}, {"--rect", "-.1,0,1,1"}, {"--rect", "0,0,1.1,1"},
		{"--rect", ".5,0,.5,1"}, {"--rect", ".7,0,.3,1"}, {"--rect", "0,.9,1,.1"},
		{"--angle", "NaN"}, {"--angle", "Inf"}, {"--angle", "45.01"}, {"--angle", "-45.01"},
		{"--clear", "--angle", "0"}, {"--clear", "--enable"}, {"--clear", "--mark"}, {"--clear", "--rect", "0,0,1,1"},
		{"--json", "--enable"}, {"--no-backup"}, {"--dry-run"}, {"--unknown"},
	}
	for _, args := range cases {
		var out, stderr bytes.Buffer
		if code := Main(append([]string{"crop", frame.Image}, args...), &out, &stderr); code == 0 {
			t.Errorf("accepted %v: %s", args, &out)
		}
		got, err := os.ReadFile(frame.Sidecar)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatalf("invalid edit %v mutated XMP", args)
		}
	}
	backups, _ := filepath.Glob(frame.Sidecar + "*.bak")
	if len(backups) != 0 {
		t.Fatal("invalid input created backups")
	}
}

func TestCropDirectoryPreflightAndHolyGrailMarkProtection(t *testing.T) {
	frames := cropFixture(t, 2)
	before, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frames[1].Sidecar, []byte("<broken>"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"crop", filepath.Dir(frames[0].Image), "--enable"}, &out, &stderr); code == 0 {
		t.Fatal("accepted malformed later packet")
	}
	after, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preflight failure wrote an earlier sidecar")
	}
	writeTestXMP(t, frames[1].Image, map[string]string{"xmp:Rating": "2"})
	if code := Main([]string{"crop", frames[1].Image, "--rect", "0,0,.5,1", "--mark"}, &out, &stderr); code == 0 {
		t.Fatal("overwrote Holy Grail keyframe")
	}
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := packet.GetScalar("crs:CropLeft"); found {
		t.Fatal("failed marking saved partial crop")
	}
	if code := Main([]string{"crop", filepath.Dir(frames[0].Image), "--mark"}, &out, &stderr); code == 0 {
		t.Fatal("marked every frame")
	}
}

func TestCropCLIAnimationToActualAVIPixels(t *testing.T) {
	frames := cropFixture(t, 3)
	for _, frame := range frames {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), A: 255})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(frame.Image, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(frames[0].Image)
	// Explicitly enable crops on packets that already say HasCrop=False.
	// The interpolation engine transitions numeric controls; it retains flags.
	cropCLI(t, dir, "--enable", "--no-backup")
	cropCLI(t, frames[0].Image, "--rect", "0,0,.5,1", "--angle", "0", "--mark", "--no-backup")
	cropCLI(t, frames[2].Image, "--rect", ".5,0,1,1", "--angle", "0", "--mark", "--no-backup")
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir, "--smoothness", "0", "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("interpolation: %s", &stderr)
	}
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	crop, err := readCropMetadata(packet)
	if err != nil || math.Abs(crop.Left-.25) > 1e-6 || math.Abs(crop.Right-.75) > 1e-6 || !crop.Active {
		t.Fatalf("midpoint=%+v %v", crop, err)
	}
	if rating, _ := packet.Rating(); rating != 1 {
		t.Fatal("intermediate frame was marked")
	}
	output := filepath.Join(dir, "animation.avi")
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 100, ApplyXMPCrop: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var centers []uint8
	for {
		start := bytes.Index(data, []byte{0xff, 0xd8})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{0xff, 0xd9})
		if end < 0 {
			t.Fatal("truncated frame")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		pixel := color.NRGBAModel.Convert(img.At(32, 16)).(color.NRGBA)
		centers = append(centers, pixel.R)
		data = data[end+2:]
	}
	if len(centers) != 3 {
		t.Fatalf("frame count=%d", len(centers))
	}
	for i, want := range []int{64, 128, 192} {
		if math.Abs(float64(int(centers[i])-want)) > 10 {
			t.Fatalf("frame %d red=%d want about %d", i, centers[i], want)
		}
	}
}

func TestCropMetadataLegacyInferenceAndInvalidValues(t *testing.T) {
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:CropLeft", ".1"); err != nil {
		t.Fatal(err)
	}
	crop, err := readCropMetadata(packet)
	if err != nil || !crop.Active {
		t.Fatalf("legacy=%+v %v", crop, err)
	}
	for _, c := range []struct{ property, value string }{{"CropLeft", "NaN"}, {"CropTop", "-.1"}, {"CropBottom", "0"}, {"HasCrop", "yes"}} {
		p := EmptyXMPPacket()
		if err := p.SetScalar("crs:"+c.property, c.value); err != nil {
			t.Fatal(err)
		}
		if _, err := readCropMetadata(p); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestCropCLIEmbeddedXMPAndConventionalSidecar(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "embedded.jpg")
	packet := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:vendor="urn:lrtl:crop-test" crs:Exposure2012="1.5"><vendor:Note>keep this structure</vendor:Note></rdf:Description></rdf:RDF></x:xmpmeta>`)
	original := writeEmbeddedTransitionJPEG(t, imagePath, packet)
	cropCLI(t, imagePath, "--rect", "0,.1,1,.9", "--angle", "-45")
	sidecar := imagePath + ".xmp"
	data, err := os.ReadFile(sidecar)
	if err != nil || !bytes.Contains(data, []byte("keep this structure")) {
		t.Fatal("embedded vendor metadata discarded")
	}
	after, err := os.ReadFile(imagePath)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("embedded image changed")
	}
	// Prefer a conventional basename.xmp sidecar when one is already present.
	conventional := filepath.Join(dir, "conventional.xmp")
	path := filepath.Join(dir, "conventional.nef")
	if err := os.WriteFile(path, []byte("source raw"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conventional, packet, 0600); err != nil {
		t.Fatal(err)
	}
	cropCLI(t, path, "--angle", "45", "--no-backup")
	if _, err := os.Stat(path + ".xmp"); !os.IsNotExist(err) {
		t.Fatal("created competing double-extension sidecar")
	}
	// Sidecar-only operation requires neither the original RAW nor a decoder.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cropCLI(t, conventional, "--clear", "--no-backup")
}

func TestCropCLIInputAndHelp(t *testing.T) {
	dir := t.TempDir()
	unsupported := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(unsupported, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {dir}, {filepath.Join(dir, "missing.jpg")}, {unsupported}} {
		var out, stderr bytes.Buffer
		if code := Main(append([]string{"crop"}, args...), &out, &stderr); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if output := cropCLI(t, "--help"); output != "" {
		t.Log(output)
	}
}
