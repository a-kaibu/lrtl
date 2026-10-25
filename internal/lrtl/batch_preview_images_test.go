package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readBatchPreviewManifest(t *testing.T, root string) batchPreviewManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "sequences.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m batchPreviewManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || len(m.Sequences) == 0 {
		t.Fatal("invalid batch preview manifest")
	}
	return m
}

func TestBatchPreviewImagesMatchIndividualAndParallel(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	for _, name := range []string{".", "a", "a/b", "nested/a"} {
		batchDeflickerFixture(t, root, name)
	}
	before := batchFileSnapshot(t, root)
	copyBatchDeflickerTree(t, root, reference)
	serial, parallel := filepath.Join(t.TempDir(), "serial"), filepath.Join(t.TempDir(), "parallel")
	options := []string{"--quality", "94", "--long-edge", "16", "--constant", "--passes", "2", "--deflicker-smoother", "lowess", "--motion-blur", "2"}
	var out, stderr bytes.Buffer
	if Main(append([]string{"batch-preview-images", root, "--output", serial, "--recursive", "--workers", "1", "--"}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("default batch preview changed sources")
	}
	if Main(append([]string{"batch-preview-images", root, "--output", parallel, "--recursive", "--workers", "4", "--write-preview-ev", "--write-lrt-visual-cache", "--"}, options...), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	sm, pm := readBatchPreviewManifest(t, serial), readBatchPreviewManifest(t, parallel)
	if !reflect.DeepEqual(sm, pm) || len(pm.Sequences) != 4 {
		t.Fatal("serial and parallel source mapping differs")
	}
	for _, entry := range pm.Sequences {
		if entry.Frames != 3 || entry.Directory != filepath.Base(entry.Directory) {
			t.Fatal("wrong job mapping")
		}
		batchPreviews := readPreviewManifest(t, filepath.Join(parallel, entry.Directory))
		individual := filepath.Join(t.TempDir(), "individual")
		input := filepath.Join(reference, filepath.FromSlash(entry.Source))
		if Main(append([]string{"preview-images", input, "--output", individual, "--write-preview-ev"}, options...), &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		for rel, data := range relativeBatchPreviewFiles(t, individual) {
			assertPreviewEVFileBytes(t, filepath.Join(parallel, entry.Directory, rel), data)
			assertPreviewEVFileBytes(t, filepath.Join(serial, entry.Directory, rel), data)
		}
		for _, measurement := range batchPreviews.Frames {
			source := filepath.Join(root, filepath.FromSlash(entry.Source), measurement.Source)
			cache := lrtPreviewPath(source, "visual")
			generated, err := os.ReadFile(filepath.Join(parallel, entry.Directory, measurement.File))
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewEVFileBytes(t, cache, generated)
		}
		frames, err := DiscoverSequence(filepath.Join(root, filepath.FromSlash(entry.Source)))
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
			path := frame.OutputSidecar()
			want, err := os.ReadFile(filepath.Join(input, filepath.Base(path)))
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewEVFileBytes(t, path, want)
			backups, err := filepath.Glob(path + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal("missing original XMP backup")
			}
			assertPreviewEVFileBytes(t, backups[0], before[path])
		}
	}
	if Main([]string{"render", filepath.Join(parallel, pm.Sequences[0].Directory), "--output", filepath.Join(t.TempDir(), "preview.avi")}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
}

func relativeBatchPreviewFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for path, data := range batchFileSnapshot(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		result[rel] = data
	}
	return result
}

func TestBatchPreviewImagesFailurePublishesNothing(t *testing.T) {
	for _, kind := range []string{"image", "xmp", "quality", "late-xmp", "cache-conflict"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			batchDeflickerFixture(t, root, "a")
			dir := batchDeflickerFixture(t, root, "z")
			parent := t.TempDir()
			output := filepath.Join(parent, "previews")
			args := []string{"batch-preview-images", root, "--output", output, "--workers", "4", "--write-preview-ev", "--"}
			switch kind {
			case "cache-conflict":
				cache := lrtPreviewPath(filepath.Join(dir, "frame1.png"), "visual")
				if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cache, []byte("preserve existing cache"), 0600); err != nil {
					t.Fatal(err)
				}
				args = []string{"batch-preview-images", root, "--output", output, "--workers", "4", "--write-lrt-visual-cache", "--"}
			case "image":
				if err := os.WriteFile(filepath.Join(dir, "frame1.png"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			case "xmp", "late-xmp":
				if err := os.WriteFile(filepath.Join(dir, "frame1.png.xmp"), []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "late-xmp" {
					args = append(args, "--apply-xmp-basic=false", "--apply-xmp-crop=false")
				}
			case "quality":
				args = append(args, "--quality", "0")
			}
			before := batchFileSnapshot(t, root)
			var out, stderr bytes.Buffer
			if Main(args, &out, &stderr) == 0 {
				t.Fatal("invalid batch succeeded")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("batch failure wrote source XMP or backups")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed batch left output or staging: %v %v", entries, err)
			}
		})
	}
}

func TestBatchPreviewImagesArgumentsAndNestedOutput(t *testing.T) {
	root := t.TempDir()
	batchDeflickerFixture(t, root, ".")
	batchDeflickerFixture(t, root, "nested/a")
	before := batchFileSnapshot(t, root)
	for _, args := range [][]string{
		nil, {root}, {root, "--output", root}, {root, "--output", filepath.Dir(root)},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--workers", "0"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--workers", "65"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--no-backup"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--", "--output=bad"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--", "--write-preview-ev=false"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--", "--no-backup"},
		{root, "--output", filepath.Join(t.TempDir(), "out"), "--", "--help"},
	} {
		var out, stderr bytes.Buffer
		if Main(append([]string{"batch-preview-images"}, args...), &out, &stderr) == 0 {
			t.Fatalf("invalid flags accepted: %v", args)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("invalid flags changed sources")
	}
	var out, stderr bytes.Buffer
	if Main([]string{"batch-preview-images", "--help"}, &out, &stderr) != 0 || !strings.Contains(stderr.String(), "workers") || !strings.Contains(stderr.String(), "write-lrt-visual-cache") {
		t.Fatal("missing help")
	}
	output := filepath.Join(root, "previews")
	if Main([]string{"batch-preview-images", root, "--output", output, "--recursive", "--workers", "2"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	m := readBatchPreviewManifest(t, output)
	if len(m.Sequences) != 2 {
		t.Fatal("nested output was discovered as input")
	}
	saved := batchFileSnapshot(t, root)
	if Main([]string{"batch-preview-images", root, "--output", output}, &out, &stderr) == 0 || !reflect.DeepEqual(saved, batchFileSnapshot(t, root)) {
		t.Fatal("existing output was overwritten")
	}
	if Main([]string{"batch-preview-images", t.TempDir(), "--output", filepath.Join(t.TempDir(), "empty")}, &out, &stderr) == 0 {
		t.Fatal("empty root accepted")
	}
}

func TestBatchPreviewImagesSharedXMPIsRejected(t *testing.T) {
	root := t.TempDir()
	writeRenderFrame(t, filepath.Join(root, "frame.png"), "png", 32, 16)
	writeRenderFrame(t, filepath.Join(root, "frame.jpg"), "jpeg", 32, 16)
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:Exposure2012", ".5"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(filepath.Join(root, "frame.xmp"), packet, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	parent := t.TempDir()
	var out, stderr bytes.Buffer
	if Main([]string{"batch-preview-images", root, "--output", filepath.Join(parent, "previews"), "--write-preview-ev"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "share XMP") {
		t.Fatalf("shared XMP was accepted: %s", &stderr)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("shared-XMP failure published files")
	}
}

func TestBatchPreviewImagesRAWAndNoBackup(t *testing.T) {
	executable, _, raw := fakeDNGConverter(t)
	t.Setenv("LRTL_TEST_CONVERTER_LOG", "")
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "frame.nef")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"crs:Exposure2012": ".5"})
	}
	output := filepath.Join(t.TempDir(), "previews")
	var out, stderr bytes.Buffer
	if Main([]string{"batch-preview-images", root, "--output", output, "--workers", "2", "--write-preview-ev", "--no-backup", "--", "--raw-develop", "--dng-converter", executable, "--long-edge", "16"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, entry := range readBatchPreviewManifest(t, output).Sequences {
		m := readPreviewManifest(t, filepath.Join(output, entry.Directory))
		if m.Width != 16 || m.Height != 16 || entry.Frames != 1 {
			t.Fatal("RAW preview dimensions differ")
		}
	}
	for path := range batchFileSnapshot(t, root) {
		if strings.HasSuffix(path, ".bak") {
			t.Fatal("--no-backup was ignored")
		}
	}
}

func TestBatchPreviewImagesSelectedWrites(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		batchDeflickerFixture(t, root, name)
	}
	before := batchFileSnapshot(t, root)
	output := filepath.Join(t.TempDir(), "previews")
	var stdout, stderr bytes.Buffer
	if Main([]string{"batch-preview-images", root, "--output", output, "--workers", "2", "--write-preview-ev", "--", "--filename-contains", "frame1.png"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, entry := range readBatchPreviewManifest(t, output).Sequences {
		manifest := readPreviewManifest(t, filepath.Join(output, entry.Directory))
		if entry.Frames != 1 || len(manifest.Frames) != 1 || manifest.Frames[0].SourceIndex != 1 || manifest.Frames[0].Source != "frame1.png" {
			t.Fatal("selected preview lost its original source index")
		}
		frames, err := DiscoverSequence(filepath.Join(root, entry.Source))
		if err != nil {
			t.Fatal(err)
		}
		for i, frame := range frames {
			assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			value, exists, err := packet.GetScalar("lrt:ExternalExposureDefault")
			if err != nil || exists != (i == 1) {
				t.Fatalf("wrong frame updated: index=%d exists=%t error=%v", i, exists, err)
			}
			if i == 1 {
				if value != formatNumber(manifest.Frames[0].ExternalExposureDefault) {
					t.Fatal("saved measurement differs from selected JPEG")
				}
			} else {
				assertPreviewEVFileBytes(t, frame.OutputSidecar(), before[frame.OutputSidecar()])
				backups, err := filepath.Glob(frame.OutputSidecar() + "*.bak")
				if err != nil || len(backups) != 0 {
					t.Fatal("unselected source got a backup")
				}
			}
		}
	}
}

type batchPreviewFaultWriter func([]byte) (int, error)

func (writer batchPreviewFaultWriter) Write(data []byte) (int, error) { return writer(data) }

func TestBatchPreviewImagesWriteErrorKeepsCompleteOutput(t *testing.T) {
	root := t.TempDir()
	dir := batchDeflickerFixture(t, root, "a")
	batchDeflickerFixture(t, root, "b")
	output := filepath.Join(t.TempDir(), "previews")
	injected := false
	writer := batchPreviewFaultWriter(func(data []byte) (int, error) {
		if !injected && strings.HasPrefix(string(data), "Backup:") {
			injected = true
			path := filepath.Join(dir, "frame1.png.xmp")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return len(data), nil
	})
	var stderr bytes.Buffer
	if Main([]string{"batch-preview-images", root, "--output", output, "--write-preview-ev"}, writer, &stderr) == 0 || !strings.Contains(stderr.String(), "stopped after 1 saved XMP files") {
		t.Fatalf("write error not reported: %s", &stderr)
	}
	m := readBatchPreviewManifest(t, output)
	if len(m.Sequences) != 2 {
		t.Fatal("write failure discarded complete previews")
	}
	for _, entry := range m.Sequences {
		manifest := readPreviewManifest(t, filepath.Join(output, entry.Directory))
		if len(manifest.Frames) != 3 {
			t.Fatal("incomplete published previews")
		}
	}
	if !injected {
		t.Fatal("write failure was not injected")
	}
}
