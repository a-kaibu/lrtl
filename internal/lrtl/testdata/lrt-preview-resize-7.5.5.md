# LRT 7.5.5 camera preview resize observations

`PreviewResizeOracle.java` invokes the installed LRT 7.5.5 frame resizer with
`RenderingHints.VALUE_INTERPOLATION_BILINEAR`, then measures the result with
the native Preview EV histogram. Each source is a generated 8-bit RGB pattern;
no vendor photographs or product code are stored here.

The three cases cover landscape, wide and portrait inputs. All are resized to
1024×682 and include the native measurement of the unscaled source for
comparison. The Go histogram values after resize are checked within `1e-5`.

`PreviewResizePixelOracle.java` invokes the same resizer for six generated
opaque RGB inputs spanning upscaling, downscaling, odd dimensions, and edge
sampling. One case uses a non-default 6-pixel target width. The JSON records
every destination pixel: all 82 RGB pixels (246 channel values) match exactly.
The Go path reproduces Java2D's 32.32 coordinate stepping and its 8-bit
bilinear fractions.
These are synthetic raster checks, not source decoder or color-management
parity checks. Alpha interpolation has an independent Go regression test but
has not been compared against the installed LRT oracle.

`lrtl preview-ev --preview-width-camera N` exposes this setting for custom LRT
configurations. It defaults to 1024 and does not read LRT's configuration file.
