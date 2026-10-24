package lrtl

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type reloadRatingReference struct {
	Completed bool `json:"reload_completed"`
	Stop      string
	Cases     []struct {
		Parent, Form, Input string
		Value               *string
		AfterDefaults       *string `json:"after_defaults"`
		Expected            int32
	}
}

func reloadRatingObservations(t testing.TB) reloadRatingReference {
	t.Helper()
	file, err := os.Open("testdata/lrt-reload-rating-7.5.5.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var r reloadRatingReference
	if err := json.NewDecoder(reader).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.Completed || !strings.Contains(r.Stop, "after rating/date cache") || len(r.Cases) != 3440 {
		t.Fatal("incomplete native rating cache observations")
	}
	return r
}

func TestReloadRatingNativeCacheReference(t *testing.T) {
	for i, row := range reloadRatingObservations(t).Cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			packet := pasteTestPacket(t, row.Input)
			image := filepath.Join("synthetic", filepath.FromSlash(row.Parent), "frame.nef")
			if _, err := prepareFullCopyDefaults(packet, image); err != nil {
				t.Fatal(err)
			}
			value, found, err := captureTimestampScalar(packet, "xmp:Rating")
			if err != nil || !found || row.AfterDefaults == nil || value != *row.AfterDefaults {
				t.Fatal("default rating input differs", value, err)
			}
			context, err := readRegisteredSaveContext(packet)
			if err != nil {
				t.Fatal(err)
			}
			got := nativeFrameRating(context.Rating, image)
			if got != row.Expected {
				t.Fatalf("cache rating got %d want %d; input %q parent %s", got, row.Expected, value, row.Parent)
			}
		})
	}
}

func TestReloadRatingFullSaveReference(t *testing.T) {
	for i, row := range reloadRatingObservations(t).Cases {
		if i%29 != 0 {
			continue
		}
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			packet := pasteTestPacket(t, row.Input)
			image := filepath.Join("synthetic", filepath.FromSlash(row.Parent), "frame.nef")
			if _, err := applyCopySettingsProfile(packet, EmptyXMPPacket(), nil, nativeSaveStateProfile, image); err != nil {
				t.Fatal(err)
			}
			value, present, err := captureTimestampScalar(packet, "xmp:Rating")
			want := row.Expected
			if want < 0 {
				want = 0
			}
			if err != nil || !present || value != fmt.Sprint(want) {
				t.Fatal("saved rating differs from cached native value", value, want, err)
			}
		})
	}
}

func TestReloadRatingInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for actual rating cache reload")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "ReloadRatingOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-XX:-OmitStackTraceInFastThrow", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.ReloadRatingOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native cache loader: %v\n%s", err, out)
	}
	var replay reloadRatingReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, reloadRatingObservations(t)) {
		t.Fatal("native reload rating observations changed")
	}
}

func TestReloadRatingCLIProfilesAndCacheRules(t *testing.T) {
	for _, parent := range []string{"sequence", "LRT_export", "lrt_export", "LRT_export/inner"} {
		for _, rating := range []string{"2", "3", " 4 ", "++1", "+1", "４"} {
			for _, mode := range []string{"full", "selected-active", "selected-inactive", "preserve"} {
				t.Run(parent+"/"+rating+"/"+mode, func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), filepath.FromSlash(parent))
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					active := "True"
					if mode == "selected-inactive" {
						active = "False"
					}
					frame := metadataDateFrame(t, dir, "frame.jpg", active, "qualified")
					packet, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					if err := packet.SetScalar("xmp:Rating", rating); err != nil {
						t.Fatal(err)
					}
					if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
						t.Fatal(err)
					}
					source := EmptyXMPPacket()
					for key, value := range map[string]string{"crs:Exposure2012": ".75", "xmp:Rating": "5"} {
						if err := source.SetScalar(key, value); err != nil {
							t.Fatal(err)
						}
					}
					path := filepath.Join(t.TempDir(), "source.xmp")
					if _, err := WritePacket(path, source, false); err != nil {
						t.Fatal(err)
					}
					sourceBefore := denoiseRead(t, path)
					before := batchFileSnapshot(t, dir)
					args := []string{"copy-settings", path, dir}
					if strings.HasPrefix(mode, "selected-") {
						args = append(args, "--properties", "Exposure2012")
					}
					if mode == "preserve" {
						args = append(args, "--save-state", "preserve")
					}
					var out, stderr bytes.Buffer
					if Main(args, &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
						t.Fatal("rating preview changed files")
					}
					if Main(append(args, "--write"), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					saved, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					got, found, err := captureTimestampScalar(saved, "xmp:Rating")
					if err != nil || !found {
						t.Fatal(got, found, err)
					}
					cache := nativeFrameRating(nativeReloadRating(rating), frame.Image)
					want := fmt.Sprint(cache)
					if cache < 0 {
						want = "0"
					}
					if mode == "preserve" || mode == "selected-inactive" && cache <= 0 {
						want = rating
					}
					if got != want {
						t.Fatal("saved cache rating differs", got, want)
					}
					backups, _ := filepath.Glob(frame.Sidecar + ".lrtl-*.bak")
					if len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
						t.Fatal("missing exact rating backup")
					}
					after := batchFileSnapshot(t, dir)
					if Main(append(args, "--write"), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
						t.Fatal("repeated cache save changed files")
					}
					if !bytes.Equal(sourceBefore, denoiseRead(t, path)) {
						t.Fatal("rating reference changed")
					}
					assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
				})
			}
		}
	}
}

func FuzzReloadRating(f *testing.F) {
	for _, row := range reloadRatingObservations(f).Cases {
		value := ""
		if row.AfterDefaults != nil {
			value = *row.AfterDefaults
		}
		f.Add(value, row.Parent)
	}
	f.Fuzz(func(t *testing.T, value, parent string) {
		if len(value) > 4096 || len(parent) > 4096 {
			t.Skip()
		}
		parsed, valid := nativeReloadInteger(value)
		if valid {
			unsigned := value
			if strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
				unsigned = unsigned[1:]
			}
			if unsigned == "" || unsigned != selectedJavaTrim(unsigned) || strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
				t.Fatal("accepted loose integer")
			}
		} else if parsed != 0 {
			t.Fatal("invalid rating retained numeric value")
		}
		rating := nativeReloadRating(value)
		if parsed == 1 {
			if rating != 4 {
				t.Fatal("one-star mapping lost")
			}
		} else if rating != parsed {
			t.Fatal("unexpected cache rating mapping")
		}
		image := filepath.Join("synthetic", parent, "frame.nef")
		got := nativeFrameRating(rating, image)
		if absolute, err := filepath.Abs(image); err == nil {
			image = absolute
		}
		reset := (rating == 2 || rating == 3) && strings.HasPrefix(filepath.Base(filepath.Dir(image)), "LRT_")
		if reset {
			if got != 0 {
				t.Fatal("export folder did not reset rating")
			}
		} else if got != rating {
			t.Fatal("unrelated parent reset rating")
		}
	})
}

func TestReloadRatingCLIRelativeDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "LRT_export")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	frame := metadataDateFrame(t, dir, "frame.jpg", "True", "qualified")
	packet, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.SetScalar("xmp:Rating", "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source.xmp")
	if _, err := WritePacket(source, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	}()
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", source, ".", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	saved, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if value, _, err := captureTimestampScalar(saved, "xmp:Rating"); err != nil || value != "0" {
		t.Fatal("relative input lost immediate parent cache rule", value, err)
	}
}
