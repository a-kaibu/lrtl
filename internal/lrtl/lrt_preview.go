package lrtl

import (
	"path/filepath"
	"strings"
)

// LRT 7.5.5 stores ordinary JPEG data in both .lrtpreview cache locations.
// Select the cache explicitly: neither timestamps nor XMP edits prove freshness.
func lrtPreviewPath(source, kind string) string {
	folder := "previews"
	if kind == "visual" {
		folder = "visual"
	}
	stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	return filepath.Join(filepath.Dir(source), ".lrt", folder, stem+".lrtpreview")
}
