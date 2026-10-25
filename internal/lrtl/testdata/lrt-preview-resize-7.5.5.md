# LRT 7.5.5 camera preview resize observations

`PreviewResizeOracle.java` invokes the installed LRT 7.5.5 frame resizer with
`RenderingHints.VALUE_INTERPOLATION_BILINEAR`, then measures the result with
the native Preview EV histogram. Each source is a generated 8-bit RGB pattern;
no vendor photographs or product code are stored here.

The three cases cover landscape, wide and portrait inputs. All are resized to
1024×682 and include the native measurement of the unscaled source for
comparison. The Go histogram values after resize are checked within `1e-5`.
The probe also records a 5×3 to 3×2 resize; Go's single-pass interpolation
matches every channel within one 8-bit code. Java2D's exact integer rounding
is not reproduced bit for bit.
