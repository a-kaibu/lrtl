package lrtl

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readPreviewManifest(t *testing.T, directory string) PreviewImageManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m PreviewImageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || m.BitDepth != 8 || m.ColorSpace != "sRGB" || m.Width < 1 || m.Height < 1 {
		t.Fatalf("invalid preview manifest: %+v", m)
	}
	return m
}

func TestPreviewImagesMatchTIFFRendererAndMeasurements(t *testing.T) {
	frames := cropFixture(t, 3)
	for i, frame := range frames {
		writeTestXMP(t, frame.Image, map[string]string{"crs:Exposure2012": ".5", "crs:Contrast2012": "20", "crs:HasCrop": "True", "crs:CropTop": ".1", "crs:CropLeft": ".1", "crs:CropBottom": ".9", "crs:CropRight": ".9", "crs:CropAngle": "3", "crs:CameraProfile": "preserve"})
		frames[i].Index = 10 + i*10
	}
	source := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, source)
	output, reference := filepath.Join(t.TempDir(), "previews"), filepath.Join(t.TempDir(), "tiff")
	options := RenderOptions{Quality: 93, LongEdge: 16, ApplyXMPBasic: true, ApplyXMPCrop: true, AutoOrient: true}
	manifest, err := GeneratePreviewImages(frames, output, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExportTIFFSequence(frames, reference, options); err != nil {
		t.Fatal(err)
	}
	stored := readPreviewManifest(t, output)
	if !reflect.DeepEqual(*manifest, stored) || len(stored.Frames) != 3 {
		t.Fatal("manifest differs from generated metadata")
	}
	previewFrames, err := DiscoverSequence(output)
	if err != nil {
		t.Fatal(err)
	}
	values, err := analyzePreviewEVSequence(previewFrames, imageInputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i, frame := range stored.Frames {
		if frame.Source != filepath.Base(frames[i].Image) || frame.SourceIndex != frames[i].Index {
			t.Fatal("source mapping lost")
		}
		img := readExportedTIFF(t, filepath.Join(reference, strings.TrimSuffix(frame.File, ".jpg")+".tif"))
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 93}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(output, frame.File))
		if err != nil || !bytes.Equal(got, encoded.Bytes()) {
			t.Fatalf("preview differs from TIFF/video pipeline: %v", err)
		}
		if frame.PreviewEV != values[i] || frame.ExternalExposureDefault != 4*values[i] || math.IsNaN(frame.Luminance) {
			t.Fatal("measurements do not match the visible JPEG")
		}
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", output, "--output", filepath.Join(t.TempDir(), "preview.avi")}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, source)) {
		t.Fatal("preview generation changed source files")
	}
}

func TestPreviewImagesCLIExposureAndWriteEV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gray.png")
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	xmp := writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "0", "crs:CameraProfile": "keep"})
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "previews")
	var stdout, stderr bytes.Buffer
	if Main([]string{"preview-images", dir, "--output", output, "--quality", "100", "--write-preview-ev"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	m := readPreviewManifest(t, output)
	if m.Width != 64 || m.Height != 32 || len(m.Frames) != 1 || m.Frames[0].PreviewEV != .5 || m.Frames[0].ExternalExposureDefault != 2 {
		t.Fatalf("neutral histogram differs: %+v", m)
	}
	packet, err := PacketForImage(path, xmp)
	if err != nil {
		t.Fatal(err)
	}
	text, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
	if err != nil || !found || text != "2" {
		t.Fatalf("stored Preview EV differs: %s %t %v", text, found, err)
	}
	profile, found, err := packet.GetScalar("crs:CameraProfile")
	if err != nil || !found || profile != "keep" {
		t.Fatal("measurement write changed camera profile")
	}
	backups, err := filepath.Glob(xmp + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatal("missing preview measurement backup")
	}
	assertPreviewEVFileBytes(t, backups[0], before[xmp])
	assertPreviewEVFileBytes(t, path, before[path])
	// Exposure-only selects that mode without conflicting with default basic development.
	writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "1"})
	bright := filepath.Join(t.TempDir(), "bright")
	if Main([]string{"preview-images", dir, "--output", bright, "--apply-xmp-exposure", "--output-height", "16", "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	bm := readPreviewManifest(t, bright)
	if bm.Width != 32 || bm.Height != 16 || bm.Frames[0].PreviewEV <= .5 {
		t.Fatal("exposure or output height ignored")
	}
	file, err := os.Open(filepath.Join(bright, bm.Frames[0].File))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("decode preview: %v %v", err, closeErr)
	}
	r, g, b, _ := decoded.At(8, 4).RGBA()
	linear := math.Pow((128.0/255+.055)/1.055, 2.4) * 2
	want := 255 * (1.055*math.Pow(linear, 1/2.4) - .055)
	if math.Abs(float64(r>>8)-want) > 2 || r != g || r != b {
		t.Fatalf("preview exposure pixel %d,%d,%d want %g", r, g, b, want)
	}
}

func TestPreviewImagesFailureKeepsSourcesAndDestination(t *testing.T) {
	for _, kind := range []string{"bad-image", "bad-xmp", "invalid-quality", "existing-output", "no-backup"} {
		t.Run(kind, func(t *testing.T) {
			frames := cropFixture(t, 2)
			dir := filepath.Dir(frames[0].Image)
			for _, frame := range frames {
				writeTestXMP(t, frame.Image, map[string]string{"crs:Exposure2012": ".5"})
			}
			parent := t.TempDir()
			output := filepath.Join(parent, "previews")
			args := []string{"preview-images", dir, "--output", output, "--write-preview-ev"}
			switch kind {
			case "bad-image":
				if err := os.WriteFile(frames[1].Image, []byte("bad image"), 0600); err != nil {
					t.Fatal(err)
				}
			case "bad-xmp":
				if err := os.WriteFile(frames[1].OutputSidecar(), []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-quality":
				args = append(args, "--quality", "0")
			case "existing-output":
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "no-backup":
				args = []string{"preview-images", dir, "--output", output, "--no-backup"}
			}
			before := batchFileSnapshot(t, dir)
			destination := batchFileSnapshot(t, parent)
			var stdout, stderr bytes.Buffer
			if Main(args, &stdout, &stderr) == 0 {
				t.Fatal("invalid preview succeeded")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) || !reflect.DeepEqual(destination, batchFileSnapshot(t, parent)) {
				t.Fatalf("failed preview wrote source, backup or output: %s", &stderr)
			}
		})
	}
}

func TestPreviewImagesRAWAndOptions(t *testing.T) {
	executable, _, data := fakeDNGConverter(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, source, map[string]string{"crs:Exposure2012": ".5"})
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "previews")
	var stdout, stderr bytes.Buffer
	if Main([]string{"preview-images", dir, "--output", output, "--raw-develop", "--dng-converter", executable, "--long-edge", "16"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	m := readPreviewManifest(t, output)
	if m.Width != 16 || m.Height != 16 || len(m.Frames) != 1 {
		t.Fatal("RAW preview dimensions differ")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("default preview wrote source XMP")
	}
	stdout.Reset()
	stderr.Reset()
	if Main([]string{"preview-images", "--help"}, &stdout, &stderr) != 0 || !strings.Contains(stderr.String(), "quality") {
		t.Fatal("missing preview help")
	}
	// Small constant fields exercise the same neutral measurement after JPEG decode.
	if math.Abs(m.Frames[0].Luminance-.3535533905932738) > .03 {
		t.Fatal("RAW + XMP exposure did not reach preview pixels")
	}
}

func TestPreviewImagesSelectedWritesAndFeedback(t *testing.T) {
	frames := feedbackFrames(t)
	dir := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "selected")
	var stdout, stderr bytes.Buffer
	if Main([]string{"preview-images", dir, "--output", output, "--filename-contains", filepath.Base(frames[1].Image), "--write-preview-ev", "--no-backup"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	m := readPreviewManifest(t, output)
	if len(m.Frames) != 1 || m.Frames[0].Source != filepath.Base(frames[1].Image) || m.Frames[0].SourceIndex != 1 {
		t.Fatal("filtered preview source mapping differs")
	}
	for i, frame := range frames {
		assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
		if i != 1 {
			assertPreviewEVFileBytes(t, frame.OutputSidecar(), before[frame.OutputSidecar()])
		}
	}
	for path := range batchFileSnapshot(t, dir) {
		if strings.HasSuffix(path, ".bak") {
			t.Fatal("--no-backup created a backup")
		}
	}
	options := RenderOptions{Quality: 91, ApplyXMPBasic: true, Deflicker: true, Constant: true, SmoothRadius: 2, DeflickerPasses: 3, DeflickerSmoother: "lowess", MotionBlurFrames: 2}
	previews, intermediate := filepath.Join(t.TempDir(), "feedback"), filepath.Join(t.TempDir(), "tiff")
	manifest, err := GeneratePreviewImages(frames, previews, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExportTIFFSequence(frames, intermediate, options); err != nil {
		t.Fatal(err)
	}
	for _, frame := range manifest.Frames {
		img := readExportedTIFF(t, filepath.Join(intermediate, strings.TrimSuffix(frame.File, ".jpg")+".tif"))
		var reference bytes.Buffer
		if err := jpeg.Encode(&reference, img, &jpeg.Options{Quality: 91}); err != nil {
			t.Fatal(err)
		}
		assertPreviewEVFileBytes(t, filepath.Join(previews, frame.File), reference.Bytes())
	}
}

func TestPreviewImagesRealRAW(t *testing.T) {
	source := os.Getenv("LRTL_RAW_SAMPLE")
	if source == "" {
		t.Skip("set LRTL_RAW_SAMPLE to check installed Adobe DNG Converter and preview generation")
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "previews")
	m, err := GeneratePreviewImages([]Frame{{Image: source}}, output, RenderOptions{Quality: 90, LongEdge: 256, RawDevelop: true, ApplyXMPBasic: true, AutoOrient: true, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER")})
	if err != nil {
		t.Fatal(err)
	}
	if max(m.Width, m.Height) > 256 || len(m.Frames) != 1 || m.Frames[0].Luminance <= 0 || m.Frames[0].PreviewEV <= 0 {
		t.Fatalf("invalid real RAW preview: %+v", m)
	}
	frames, err := DiscoverSequence(output)
	if err != nil {
		t.Fatal(err)
	}
	measured, err := analyzePreviewEVSequence(frames, imageInputOptions{})
	if err != nil || measured[0] != m.Frames[0].PreviewEV {
		t.Fatalf("real RAW preview measurement differs: %v", err)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("real RAW source changed")
	}
}

func TestPreviewImagesXMPWriteFailureKeepsPublishedImages(t *testing.T) {
	frames := cropFixture(t, 2)
	dir := filepath.Dir(frames[0].Image)
	for _, frame := range frames {
		writeTestXMP(t, frame.Image, map[string]string{"crs:Exposure2012": "0"})
	}
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "previews")
	writer := &splitImportFaultWriter{action: func() {
		// The first backup log is emitted after the first XMP is saved.
		// Make the next target unwritable without modifying input images.
		path := frames[1].OutputSidecar()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}}
	var stderr bytes.Buffer
	if Main([]string{"preview-images", dir, "--output", output, "--write-preview-ev"}, writer, &stderr) == 0 || !strings.Contains(stderr.String(), "stopped after 1 saved XMP files") {
		t.Fatalf("XMP failure not reported accurately: %s", &stderr)
	}
	m := readPreviewManifest(t, output)
	if len(m.Frames) != 2 {
		t.Fatal("published previews were lost")
	}
	for i, frame := range frames {
		assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
		if _, err := os.Stat(filepath.Join(output, m.Frames[i].File)); err != nil {
			t.Fatal("published JPEG missing", err)
		}
	}
	backups, err := filepath.Glob(frames[0].OutputSidecar() + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatal("first saved XMP lost its backup")
	}
	assertPreviewEVFileBytes(t, backups[0], before[frames[0].OutputSidecar()])
}
