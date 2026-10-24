package lrtl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const absentRating = "__absent__"

func PacketForSidecar(path string) (*XMPPacket, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return EmptyXMPPacket(), nil
	}
	if err != nil {
		return nil, err
	}
	return parseXMP(data)
}

func PacketForFrame(frame Frame) (*XMPPacket, error) {
	packet, found, err := loadFramePacket(frame)
	if err != nil {
		return nil, err
	}
	if found {
		return packet, nil
	}
	return EmptyXMPPacket(), nil
}

func PacketForImage(imagePath, sidecarPath string) (*XMPPacket, error) {
	if _, err := os.Stat(sidecarPath); err == nil {
		return PacketForSidecar(sidecarPath)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	data, found, err := readEmbeddedImageXMP(imagePath)
	if err != nil {
		return nil, err
	}
	if found {
		return parseXMP(data)
	}
	return EmptyXMPPacket(), nil
}

func MarkKeyframe(packet *XMPPacket, kind string) (int, error) {
	if kind != "default" && kind != "crop" {
		return 0, fmt.Errorf("unsupported keyframe kind: %s", kind)
	}
	rating := 4
	if kind == "crop" {
		rating = 5
	}
	return MarkKeyframeWithRating(packet, kind, rating)
}

func MarkKeyframeWithRating(packet *XMPPacket, kind string, rating int) (int, error) {
	if kind != "default" && kind != "crop" {
		return 0, fmt.Errorf("unsupported keyframe kind: %s", kind)
	}
	if rating != 4 && rating != 5 {
		return 0, fmt.Errorf("keyframe rating must be 4 or 5: %d", rating)
	}
	if _, hgMarked, err := packet.GetScalar(holyGrailKeyframeProperty); err != nil {
		return 0, err
	} else if hgMarked {
		return 0, fmt.Errorf("this image is an internal Holy Grail keyframe; reset Holy Grail marks before assigning a regular keyframe")
	}
	if rating, valid := packet.Rating(); valid && (rating == 2 || rating == 3) {
		return 0, fmt.Errorf("this image has an LRTimelapse 2- or 3-star Holy Grail keyframe rating; reset Holy Grail marks before assigning a regular keyframe")
	}
	marked, err := packet.HasProperty("ol:KeyframeKind")
	if err != nil {
		return 0, err
	}
	if !marked {
		previousRating, found, err := packet.GetScalar("xmp:Rating")
		if err != nil {
			return 0, err
		}
		if !found {
			previousRating = absentRating
		}
		if err := packet.SetScalar("ol:PreviousRating", previousRating); err != nil {
			return 0, err
		}
	}
	if err := packet.SetScalar("xmp:Rating", strconv.Itoa(rating)); err != nil {
		return 0, err
	}
	if err := packet.SetScalar("ol:KeyframeKind", kind); err != nil {
		return 0, err
	}
	return rating, nil
}

func UnmarkKeyframe(packet *XMPPacket, forceRating bool) error {
	marked, err := packet.HasProperty("ol:KeyframeKind")
	if err != nil {
		return err
	}
	if marked {
		previousRating, found, err := packet.GetScalar("ol:PreviousRating")
		if err != nil {
			return err
		}
		if found && previousRating == absentRating {
			if err := packet.RemoveProperty("xmp:Rating"); err != nil {
				return err
			}
		} else if found {
			if err := packet.SetScalar("xmp:Rating", previousRating); err != nil {
				return err
			}
		}
		if err := packet.RemoveProperty("ol:KeyframeKind"); err != nil {
			return err
		}
		return packet.RemoveProperty("ol:PreviousRating")
	}
	if forceRating {
		if rating, valid := packet.Rating(); valid && (rating == 4 || rating == 5) {
			return packet.RemoveProperty("xmp:Rating")
		}
	}
	return fmt.Errorf("this marker was not created by lrtl; pass --force-rating to clear an existing 4- or 5-star rating")
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	if info, err := os.Stat(source); err == nil {
		_ = os.Chmod(destination, info.Mode().Perm())
	}
	return nil
}

func WritePacket(path string, packet *XMPPacket, backup bool) (string, error) {
	return writePacketNewerThan(path, packet, backup, time.Time{})
}

func writePacketNewerThan(path string, packet *XMPPacket, backup bool, minimum time.Time) (string, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	backupPath := ""
	info, statErr := os.Stat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	if statErr == nil && backup {
		stamp := time.Now().Format("20060102-150405.000000")
		backupPath = fmt.Sprintf("%s.lrtl-%s.bak", path, stamp)
		if err := copyFile(path, backupPath); err != nil {
			return "", err
		}
	}

	temp, err := os.CreateTemp(parent, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return backupPath, err
	}
	tempPath := temp.Name()
	cleanup := func() { _ = os.Remove(tempPath) }
	data, err := packet.Serialize()
	if err != nil {
		_ = temp.Close()
		cleanup()
		return backupPath, err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		cleanup()
		return backupPath, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		cleanup()
		return backupPath, err
	}
	mode := os.FileMode(0644)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		cleanup()
		return backupPath, err
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return backupPath, err
	}
	// Discovery chooses the newest sidecar variant. Do not make an edited
	// future-dated active file older than its stale companion. Two seconds
	// also advances timestamps on filesystems with coarse time resolution.
	stamp := time.Time{}
	if statErr == nil && info.ModTime().After(time.Now()) {
		stamp = info.ModTime().Add(2 * time.Second)
	}
	if !minimum.IsZero() {
		// JPEG/DNG initialization must remain newer than the image at native
		// millisecond resolution, including future-dated source images.
		actual := stamp
		if actual.IsZero() {
			temporary, err := os.Stat(tempPath)
			if err != nil {
				cleanup()
				return backupPath, err
			}
			actual = temporary.ModTime()
		}
		if actual.UnixMilli() <= minimum.UnixMilli() {
			stamp = minimum.Add(2 * time.Second)
		}
	}
	if !stamp.IsZero() {
		if err := os.Chtimes(tempPath, stamp, stamp); err != nil {
			cleanup()
			return backupPath, err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		cleanup()
		return backupPath, err
	}
	return backupPath, nil
}
