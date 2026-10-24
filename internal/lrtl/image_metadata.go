package lrtl

import (
	"fmt"
	"io"
	"runtime"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/exif2"
)

// The third-party decoder can panic on unsupported TIFF layouts inside CR3.
// Treat its runtime bounds faults as decode errors at the input boundary, so
// callers can retain their existing metadata fallback/error behavior.
func decodeImageMetadata(input io.ReadSeeker) (metadata exif2.Exif, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(runtime.Error); !ok {
				panic(failure)
			}
			metadata = exif2.Exif{}
			err = fmt.Errorf("image metadata decoder rejected input: %v", failure)
		}
	}()
	return imagemeta.Decode(input)
}
