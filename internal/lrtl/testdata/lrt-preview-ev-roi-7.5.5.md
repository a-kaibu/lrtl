# Native Preview EV and reference-area observations

`lrt-preview-ev-roi-7.5.5.json` records 59 synthetic image/ROI measurements
from installed LRTimelapse 7.5.5 build 1059 on 2026-10-17. No vendor image
or executable code is included.

An independent Java reflection probe initialized `tQa`, constructed a `Rqa`
histogram component without showing a window, and selected the static method
returning double with parameters `(Rqa, BufferedImage, double, double, double,
double, boolean)`. It passed a `TYPE_INT_RGB` image, the four listed normalized
box coordinates (parsed as float and widened to double), and false. The result
was divided by 256 to record Preview EV.

Patterns 0–3 are solid RGB(0,128,255), white, black and red. Patterns 4–47
use these original deterministic pixels at zero-based image coordinates:

```
R = (37*x + 11*y + 17*pattern) & 255
G = (13*x + 41*y + 29*pattern) & 255
B = (23*x +  7*y + 43*pattern) & 255
```

Patterns 48, 51, 54 and 58 are solid RGB(128,128,128), 49/52 are RGB(64,128,192),
50 is black, 53 is RGB(254,254,254), 55 is RGB(64,64,64), 56 has six black
pixels followed by six white pixels, and 57 is RGB(254,128,0). Dimensions and
normalized rectangles are stored in every JSON record.

The observations exposed errors in the previous Go histogram implementation:
255 is excluded by the native histogram's upper bound; weighted radius-2
smoothing rounds counts and renormalizes each band; bin width is 255/256;
and the channel means are averaged separately. An empty band's mean makes
the native measurement fall back to 255/256. The Go tests compare against
these native values with absolute tolerance 1e-14 and also use nonzero origins.

This verifies the histogram path on matching RGB pixels and reference areas.
It does not verify Adobe RAW development, decoder/color-management equality,
long-term metric mode or native Visual Deflicker as a complete workflow.
