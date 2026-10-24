package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type xmpSourceObservation struct {
	Name, Filename  string
	Data            []byte
	Sidecar, Result *string
	ImageTimeMS     int64  `json:"image_time_ms"`
	SidecarTimeMS   *int64 `json:"sidecar_time_ms"`
	ImageTimeNS     int64  `json:"image_time_ns"`
	SidecarTimeNS   *int64 `json:"sidecar_time_ns"`
}
type xmpSourceReference struct {
	Completed bool `json:"file_reader_completed"`
	Cases     []xmpSourceObservation
}

func xmpSourceObservations(t *testing.T) xmpSourceReference {
	t.Helper()
	var reference xmpSourceReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-xmp-source-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 76 {
		t.Fatal("invalid native file-reader observations")
	}
	seen := map[string]bool{}
	for _, row := range reference.Cases {
		if seen[row.Name] || filepath.Base(row.Name) != row.Name || filepath.Base(row.Filename) != row.Filename || len(row.Data) == 0 {
			t.Fatal("invalid source fixture", row.Name)
		}
		seen[row.Name] = true
		if (row.Sidecar == nil) != (row.SidecarTimeNS == nil) || (row.Sidecar == nil) != (row.SidecarTimeMS == nil) {
			t.Fatal("missing sidecar time", row.Name)
		}
	}
	return reference
}

func writeXMPSourceObservation(t *testing.T, dir string, row xmpSourceObservation) Frame {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	frame := Frame{Image: filepath.Join(dir, row.Filename)}
	if err := os.WriteFile(frame.Image, row.Data, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(0, row.ImageTimeNS)
	if err := os.Chtimes(frame.Image, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if row.Sidecar != nil {
		frame.Sidecar = filepath.Join(dir, "frame.xmp")
		if err := os.WriteFile(frame.Sidecar, []byte(*row.Sidecar), 0600); err != nil {
			t.Fatal(err)
		}
		stamp = time.Unix(0, *row.SidecarTimeNS)
		if err := os.Chtimes(frame.Sidecar, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return frame
}

func ensureTestSidecarNewer(t *testing.T, image, sidecar string) {
	t.Helper()
	info, err := os.Stat(image)
	if err != nil {
		t.Fatal(err)
	}
	stamp := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(sidecar, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestXMPSourceNativeFileReader(t *testing.T) {
	strict := 0
	for _, row := range xmpSourceObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			dir := t.TempDir()
			frame := writeXMPSourceObservation(t, dir, row)
			before := batchFileSnapshot(t, dir)
			packet, found, _, err := loadFramePacketSource(frame, true)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("source read changed files")
			}
			if err != nil {
				if row.Sidecar != nil && *row.Sidecar == "<invalid" && row.Result == nil {
					strict++
					return
				}
				t.Fatal(err)
			}
			if row.Result == nil {
				if found {
					t.Fatal("found XMP where native returned none")
				}
				return
			}
			if !found || packet == nil {
				t.Fatal("missing native packet")
			}
			assertPastedMetadata(t, packet, *row.Result)
		})
	}
	if strict != 18 {
		t.Fatal("invalid selected sidecar strictness count", strict)
	}
}

func TestXMPSourceInstalledFileReader(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for complete native XMP source reader replay")
	}
	dir := t.TempDir()
	inputs := filepath.Join(dir, "inputs")
	for _, row := range xmpSourceObservations(t).Cases {
		writeXMPSourceObservation(t, filepath.Join(inputs, row.Name), row)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(root, "MetadataPasteOracle.java"), filepath.Join(root, "XMPSourceOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.XMPSourceOracle", path, inputs).CombinedOutput(); err != nil {
		t.Fatalf("native reader: %v\n%s", err, out)
	}
	var replay xmpSourceReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, xmpSourceObservations(t)) {
		t.Fatal("native XMP source results changed")
	}
}

func TestXMPSourceInitializationFutureImageAndPreserve(t *testing.T) {
	var row xmpSourceObservation
	for _, candidate := range xmpSourceObservations(t).Cases {
		if candidate.Name == "jpg-embedded1-older" {
			row = candidate
		}
	}
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			frame := writeXMPSourceObservation(t, dir, row)
			future := time.Now().Add(24 * time.Hour)
			if err := os.Chtimes(frame.Image, future, future); err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"init", dir, "--frame-state", profile}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			if profile == nativeSaveStateProfile && !strings.Contains(out.String(), "XMP source: image") {
				t.Fatal("source decision was hidden", &out)
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			want := "sidecar"
			if profile == nativeSaveStateProfile {
				want = "embedded"
			}
			// f is a fixture namespace; compare its expanded name via semantic nodes.
			nodes, nodeErr := pastePacketNodes(packet)
			if nodeErr != nil {
				t.Fatal(nodeErr)
			}
			node := selectedField(nodes, pasteName{"urn:lrtl:xmp-source", "Origin"})
			if node == nil || node.Value != want {
				t.Fatal("wrong source", node, want)
			}
			if profile == nativeSaveStateProfile {
				info, err := os.Stat(frame.Sidecar)
				if err != nil {
					t.Fatal(err)
				}
				if info.ModTime().UnixMilli() <= future.UnixMilli() {
					t.Fatal("saved sidecar cannot be reread ahead of future image")
				}
				_, found, source, err := loadFramePacketSource(frame, true)
				if err != nil || !found || source != "sidecar" {
					t.Fatal(found, source, err)
				}
			}
			assertPreviewEVFileBytes(t, frame.Image, row.Data)
			backups, err := filepath.Glob(frame.Sidecar + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal(backups, err)
			}
			assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			after := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat wrote sidecars/backups")
			}
		})
	}
}

func TestXMPSourceSelectedInvalidSidecarPreflight(t *testing.T) {
	dir := t.TempDir()
	rows := xmpSourceObservations(t).Cases
	for i, name := range []string{"jpg-embedded1-older", "tif-embedded1-bad-newer"} {
		for _, row := range rows {
			if row.Name == name {
				frame := writeXMPSourceObservation(t, t.TempDir(), row)
				image := filepath.Join(dir, fmt.Sprintf("frame%d%s", i, filepath.Ext(frame.Image)))
				sidecar := filepath.Join(dir, fmt.Sprintf("frame%d.xmp", i))
				if err := os.Rename(frame.Image, image); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(frame.Sidecar, sidecar); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted selected malformed sidecar")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed source preflight wrote files")
	}
}

func FuzzXMPSourceTimestamps(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-xmp-source-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference xmpSourceReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	directory := f.TempDir()
	var frames [8]Frame
	var rows [8]xmpSourceObservation
	for index, name := range []string{"jpg-embedded1-older", "dng-embedded1-older", "tif-embedded1-older", "png-embedded1-older", "jpg-embedded0-older", "dng-embedded0-older", "tif-embedded0-older", "png-embedded0-older"} {
		for _, row := range reference.Cases {
			if row.Name == name {
				rows[index] = row
			}
		}
		frames[index] = Frame{Image: filepath.Join(directory, fmt.Sprintf("frame%d%s", index, filepath.Ext(rows[index].Filename))), Sidecar: filepath.Join(directory, "frame.xmp")}
		if err := os.WriteFile(frames[index].Image, rows[index].Data, 0600); err != nil {
			f.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		f.Add(int64(100000), int64(900000), uint8(i), false)
		f.Add(int64(900000), int64(1100000), uint8(i), true)
	}
	f.Fuzz(func(t *testing.T, imageOffset, sidecarOffset int64, kind uint8, invalid bool) {
		index := int(kind) % len(frames)
		frame := frames[index]
		row := rows[index]
		base := time.Unix(1700000000, 0)
		imageTime := base.Add(time.Duration(imageOffset % int64(time.Hour)))
		sidecarTime := base.Add(time.Duration(sidecarOffset % int64(time.Hour)))
		sidecar := []byte(*row.Sidecar)
		if invalid {
			sidecar = []byte("<invalid")
		}
		if err := os.WriteFile(frame.Sidecar, sidecar, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(frame.Image, imageTime, imageTime); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(frame.Sidecar, sidecarTime, sidecarTime); err != nil {
			t.Fatal(err)
		}
		imageInfo, err := os.Stat(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		sideInfo, err := os.Stat(frame.Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		preferImage := index%4 < 2 && imageInfo.ModTime().UnixMilli() >= sideInfo.ModTime().UnixMilli()
		packet, found, source, err := loadFramePacketSource(frame, true)
		if invalid && !preferImage {
			if err == nil {
				t.Fatal("accepted selected malformed sidecar")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			want := "sidecar"
			if preferImage {
				want = "image"
			}
			if source != want {
				t.Fatal("wrong source", source, want)
			}
			if preferImage && index >= 4 {
				if found {
					t.Fatal("invented embedded metadata")
				}
			} else if !found || packet == nil {
				t.Fatal("lost selected XMP")
			}
		}
		assertPreviewEVFileBytes(t, frame.Image, row.Data)
		assertPreviewEVFileBytes(t, frame.Sidecar, sidecar)
		imageAfter, _ := os.Stat(frame.Image)
		sideAfter, _ := os.Stat(frame.Sidecar)
		if !imageAfter.ModTime().Equal(imageInfo.ModTime()) || !sideAfter.ModTime().Equal(sideInfo.ModTime()) {
			t.Fatal("source reader changed modification times")
		}
	})
}

func TestXMPSourceDefaultReadersRemainSidecarFirst(t *testing.T) {
	for _, row := range xmpSourceObservations(t).Cases {
		if row.Name == "jpg-embedded1-older" {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			packet, found, source, err := loadFramePacketSource(frame, false)
			if err != nil || !found || source != "sidecar" {
				t.Fatal(found, source, err)
			}
			assertPastedMetadata(t, packet, *row.Sidecar)
		}
	}
}

func TestXMPSourceAlreadyInitializedImageReplacesStaleSidecar(t *testing.T) {
	var embedded, plain xmpSourceObservation
	for _, row := range xmpSourceObservations(t).Cases {
		if row.Name == "jpg-embedded1-none" {
			embedded = row
		}
		if row.Name == "jpg-embedded0-none" {
			plain = row
		}
	}
	dir := t.TempDir()
	frame := writeXMPSourceObservation(t, dir, embedded)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := frames[0].Sidecar
	ready := denoiseRead(t, sidecar)
	payload := append([]byte(jpegXMPHeader), ready...)
	if len(payload)+2 > 65535 {
		t.Fatal("synthetic APP1 packet too large")
	}
	image := append([]byte(nil), plain.Data[:2]...)
	image = append(image, 255, 225, 0, 0)
	binary.BigEndian.PutUint16(image[4:6], uint16(len(payload)+2))
	image = append(image, payload...)
	image = append(image, plain.Data[2:]...)
	if err := os.WriteFile(frame.Image, image, 0600); err != nil {
		t.Fatal(err)
	}
	stale := []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" crs:Exposure2012="9"/></rdf:RDF>`)
	if err := os.WriteFile(sidecar, stale, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(frame.Image, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildInitializationPlan(frames, "auto")
	if err != nil || len(plan) != 1 {
		t.Fatal("ignored sidecar was not scheduled for reconciliation", len(plan), err)
	}
	// The embedded packet itself needs no further preparation.
	packet, found, _, err := loadFramePacketSource(frames[0], true)
	if err != nil || !found {
		t.Fatal(err)
	}
	beforePacket, _ := packet.Serialize()
	plannedPacket, _ := plan[0].Packet.Serialize()
	if !bytes.Equal(beforePacket, plannedPacket) {
		t.Fatal("test image was not already initialized")
	}
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	backups, err := filepath.Glob(sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	assertPreviewEVFileBytes(t, backups[0], stale)
	snapshot := batchFileSnapshot(t, dir)
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(snapshot, batchFileSnapshot(t, dir)) {
		t.Fatal("reconciled source did not stabilize")
	}
	assertPreviewEVFileBytes(t, frame.Image, image)
}

func TestWritePacketNewerThanPreservesFailureAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.xmp")
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:Exposure2012", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(path, packet, false); err != nil {
		t.Fatal(err)
	}
	before := denoiseRead(t, path)
	threshold := time.Now().Add(48 * time.Hour)
	if err := packet.SetScalar("crs:Exposure2012", "2"); err != nil {
		t.Fatal(err)
	}
	backup, err := writePacketNewerThan(path, packet, true, threshold)
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewEVFileBytes(t, backup, before)
	info, err := os.Stat(path)
	if err != nil || info.ModTime().UnixMilli() <= threshold.UnixMilli() {
		t.Fatal(info, err)
	}
	if bytes.Equal(before, denoiseRead(t, path)) {
		t.Fatal("new packet was not written")
	}
	previous := info.ModTime()
	if _, err := writePacketNewerThan(path, packet, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil || !info.ModTime().After(previous) {
		t.Fatal("future active timestamp moved backward", info, err)
	}
	block := filepath.Join(dir, "not-directory")
	if err := os.WriteFile(block, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := batchFileSnapshot(t, dir)
	if _, err := writePacketNewerThan(filepath.Join(block, "frame.xmp"), packet, true, threshold); err == nil {
		t.Fatal("accepted non-directory parent")
	}
	if !reflect.DeepEqual(snapshot, batchFileSnapshot(t, dir)) {
		t.Fatal("failed save changed existing files")
	}
}
