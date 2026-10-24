package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type sequenceCompanionReference struct {
	Completed bool `json:"path_helpers_completed"`
	Cases     []struct {
		Name, Stem string
		Paired     bool `json:"paired_jpeg"`
		Cache      []string
	}
}

func sequenceCompanionObservations(t testing.TB) sequenceCompanionReference {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-sequence-companions-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var r sequenceCompanionReference
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Completed || len(r.Cases) != 24 {
		t.Fatal("invalid companion path observations")
	}
	return r
}

func TestSequenceCompanionsNativePaths(t *testing.T) {
	for _, row := range sequenceCompanionObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			stem, paired, cache := nativeSequenceCompanionNames(filepath.Join("synthetic", "sequence", row.Name))
			for i := range cache {
				cache[i] = filepath.ToSlash(cache[i])
			}
			if stem != row.Stem || paired != row.Paired || !reflect.DeepEqual(cache, row.Cache) {
				t.Fatal(stem, paired, cache, row)
			}
		})
	}
}

func TestSequenceCompanionsInstalledPaths(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for genuine companion path helper replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(root, "MetadataPasteOracle.java"), filepath.Join(root, "SequenceCompanionsOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v %s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.SequenceCompanionsOracle", path, dir).CombinedOutput(); err != nil {
		t.Fatalf("native path helpers: %v %s", err, out)
	}
	var r sequenceCompanionReference
	if err := json.Unmarshal(denoiseRead(t, path), &r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, sequenceCompanionObservations(t)) {
		t.Fatal("native companion paths changed")
	}
}

func companionWrite(t *testing.T, root, name string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSequenceCompanionsCopySharedCacheAndActiveSidecars(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"same.nef", "same.jpg", "same.xmp", ".lrt/previews/same.lrtpreview", ".lrt/visual/same.lrtpreview", ".lrt/proxy/same.proxy", ".lrt/sequence.properties", ".lrt/visual/unselected.lrtpreview"} {
		companionWrite(t, dir, name, []byte("opaque "+name))
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, include := range []bool{false, true} {
		t.Run(fmt.Sprint(include), func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "copy")
			plan, err := planSequenceCopy(dir, dest, frames, include)
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if include {
				want = 6
			}
			if len(plan.files) != want || plan.frames != 2 || plan.sidecars != 1 {
				t.Fatal("bundle counts", plan)
			}
			if include && plan.caches != 3 {
				t.Fatal("missing cache files", plan.caches)
			}
			before := renameTreeSnapshot(t, dir)
			if err := executeSequenceCopy(plan, nil); err != nil {
				t.Fatal(err)
			}
			for _, file := range plan.files {
				assertPreviewEVFileBytes(t, filepath.Join(dest, file.name), denoiseRead(t, file.source))
				a, _ := os.Stat(file.source)
				b, _ := os.Stat(filepath.Join(dest, file.name))
				if !a.ModTime().Equal(b.ModTime()) {
					t.Fatal("cache time changed", file.name)
				}
			}
			if _, err := os.Stat(filepath.Join(dest, ".lrt", "sequence.properties")); !os.IsNotExist(err) {
				t.Fatal("copied whole-sequence settings")
			}
			if !reflect.DeepEqual(before, renameTreeSnapshot(t, dir)) {
				t.Fatal("copy changed source bundle")
			}
		})
	}
}

func TestSequenceCompanionsPairedJPEGSelection(t *testing.T) {
	for _, extension := range []string{".nef", ".png", ".jpg", ".jpeg"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			selected := filepath.Join(dir, "frame"+extension)
			companionWrite(t, dir, "frame"+extension, []byte("selected"))
			if extension != ".jpg" {
				companionWrite(t, dir, "frame.jpg", []byte("paired JPEG"))
			}
			companionWrite(t, dir, "frame.jpg.xmp", []byte("JPEG-only metadata"))
			companionWrite(t, dir, "frame"+extension+".xmp", []byte("selected metadata"))
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			var frame Frame
			for _, f := range frames {
				if f.Image == selected {
					frame = f
				}
			}
			plan, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), []Frame{frame}, true)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if extension == ".nef" || extension == ".png" {
				want = 2
			}
			if plan.frames != want || plan.sidecars != want {
				t.Fatal("paired-image eligibility", plan.frames, plan.sidecars, want)
			}
		})
	}
}

func TestSequenceCompanionsWindowsJPEGSpelling(t *testing.T) {
	if !samePath("A", "a") {
		t.Skip("case-insensitive source lookup")
	}
	dir := t.TempDir()
	companionWrite(t, dir, "frame.NEF", []byte("raw"))
	companionWrite(t, dir, "frame.JPG", []byte("jpeg"))
	plan, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), []Frame{{Image: filepath.Join(dir, "frame.NEF")}}, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range plan.files {
		if file.name == "frame.JPG" {
			found = true
		}
	}
	if !found {
		t.Fatal("paired image spelling changed", plan.files)
	}
}

func TestSequenceCompanionsFailures(t *testing.T) {
	for _, kind := range []string{"invalid-companion", "cache-copy-failure", "cache-changed", "destination-collision"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			companionWrite(t, dir, "frame.nef", []byte("raw"))
			cache := ".lrt/previews/frame.lrtpreview"
			companionWrite(t, dir, cache, []byte("cache"))
			dest := filepath.Join(t.TempDir(), "copy")
			if kind == "invalid-companion" {
				if err := os.Mkdir(filepath.Join(dir, "frame.jpg"), 0755); err != nil {
					t.Fatal(err)
				}
				if _, err := planSequenceCopy(dir, dest, []Frame{{Image: filepath.Join(dir, "frame.nef")}}, true); err == nil {
					t.Fatal("accepted non-file JPEG")
				}
				return
			}
			plan, err := planSequenceCopy(dir, dest, []Frame{{Image: filepath.Join(dir, "frame.nef")}}, true)
			if err != nil {
				t.Fatal(err)
			}
			before := renameTreeSnapshot(t, dir)
			copyOne := func(source, target string) error {
				if kind == "cache-copy-failure" && strings.HasSuffix(source, ".lrtpreview") {
					return fmt.Errorf("injected cache copy failure")
				}
				if err := copyFile(source, target); err != nil {
					return err
				}
				if kind == "cache-changed" && strings.HasSuffix(source, ".lrtpreview") {
					return os.WriteFile(source, []byte("changed cache"), 0600)
				}
				if kind == "destination-collision" {
					if _, err := os.Stat(dest); os.IsNotExist(err) {
						if err := os.Mkdir(dest, 0755); err != nil {
							return err
						}
						return os.WriteFile(filepath.Join(dest, "keep"), []byte("existing"), 0600)
					}
				}
				return nil
			}
			if err := executeSequenceCopy(plan, copyOne); err == nil {
				t.Fatal("missing injected failure")
			}
			if kind == "destination-collision" {
				assertPreviewEVFileBytes(t, filepath.Join(dest, "keep"), []byte("existing"))
			} else if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("published incomplete bundle")
			}
			if kind != "cache-changed" && !reflect.DeepEqual(before, renameTreeSnapshot(t, dir)) {
				t.Fatal("failed copy changed source")
			}
			staged, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".lrtl-copy-*"))
			if len(staged) != 0 {
				t.Fatal("staging leaked", staged)
			}
		})
	}
}

func TestSequenceCompanionsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	companionWrite(t, dir, "frame.nef", []byte("raw"))
	companionWrite(t, outside, "previews/frame.lrtpreview", []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(dir, ".lrt")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	if _, err := planSequenceCopy(dir, filepath.Join(t.TempDir(), "copy"), []Frame{{Image: filepath.Join(dir, "frame.nef")}}, true); err == nil {
		t.Fatal("accepted cache outside sequence")
	}
}

func TestSequenceCompanionsCLIAndAVIPixels(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	selected := frames[0]
	stem, _, cache := nativeSequenceCompanionNames(selected.Image)
	// Original JPEG bytes from a generated fixture; companion is independently
	// discoverable and must not silently disappear from the copy preview.
	imageData := iiqDimensionInput(t, "le-preview.iiq")
	jpeg, err := extractRawPreview("frame.iiq", imageData)
	if err != nil {
		t.Fatal(err)
	}
	companionWrite(t, dir, stem+".jpg", jpeg)
	for _, name := range cache {
		companionWrite(t, dir, name, []byte("opaque cache "+name))
	}
	before := renameTreeSnapshot(t, dir)
	dest := filepath.Join(t.TempDir(), "copy")
	var out, stderr bytes.Buffer
	args := []string{"copy-sequence", dir, "--output", dest, "--filename-contains", filepath.Base(selected.Image), "--include-lrt-companions"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("preview wrote output")
	}
	if !strings.Contains(out.String(), "2 images") || !strings.Contains(out.String(), "3 cache files") {
		t.Fatal("companion counts missing", &out)
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, renameTreeSnapshot(t, dir)) {
		t.Fatal("CLI changed original sequence")
	}
	for _, name := range cache {
		assertPreviewEVFileBytes(t, filepath.Join(dest, name), []byte("opaque cache "+name))
	}
	render := func(d string) []byte {
		path := filepath.Join(t.TempDir(), "video.avi")
		if Main([]string{"render", d, "--output", path, "--filename-contains", filepath.Base(selected.Image), "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI payload")
		}
		return data[offset:]
	}
	if !bytes.Equal(render(dir), render(dest)) {
		t.Fatal("companion copy changed selected frame pixels")
	}
}

func FuzzSequenceCompanionNames(f *testing.F) {
	for _, row := range sequenceCompanionObservations(f).Cases {
		f.Add(row.Name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 2048 {
			return
		}
		_, _, cache := nativeSequenceCompanionNames(filepath.Join("synthetic", name))
		for _, path := range cache {
			if filepath.IsAbs(path) || !pathContains(".lrt", path) {
				t.Fatal("cache name escaped LRT directory", name, path)
			}
		}
	})
}
