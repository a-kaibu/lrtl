# Native-compatible JPEG decoding

The entropy/marker/progressive decoding files (`reader.go`, `scan.go`,
`huffman.go`) are adapted from Go 1.23's `image/jpeg`, under the included
Go Authors BSD license. Their copyright notices are retained.

Changes: private package name, no global image-format registration, public
`image/draw` replacing the standard library's internal drawing helper, and
a 64-megapixel allocation limit matching the renderer's input limit.
Non-interleaved progressive scans also count restart intervals per coded
block, rather than per group of blocks from the frame's sampling factors.

`idct.go` is an independent separable fixed-point cosine-matrix implementation
for the rounding convention measured in the installed LRT FFmpeg 8.0.1 MJPEG
decoder. The row precision, column precision, signed intermediate storage and
level shift are explicit; no native application code is included.

References used to identify numerical conventions:

- [Go 1.23 JPEG decoder](https://github.com/golang/go/tree/go1.23.0/src/image/jpeg)
- [FFmpeg 8.0.1 IDCT constants and shifts](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavcodec/simple_idct_template.c)
- [FFmpeg 8.0.1 MJPEG decoder](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavcodec/mjpegdec.c)

This is a pure Go package. Standard image decoding elsewhere in lrtl is not
registered or replaced by importing it.
