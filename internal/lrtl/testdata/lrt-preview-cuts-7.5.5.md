# Native black/white histogram exclusion

`lrt-preview-cuts-7.5.5.json` records 96 original synthetic measurements from
installed LRTimelapse 7.5.5 build 1059, obtained on 2026-10-17.

A Java reflection probe selected static `tQa.A(Rqa, BufferedImage, double,
double, double, double, boolean)` returning double. It passed a full-frame
rectangle and true to select exclusion metering, then divided the return by
256. Each case set the temporary Java process's `LRTSettings` fields
`defaultDeflickerIgnoreBlacks`, `defaultDeflickerIgnoreWhites`, and
`deflickerIgnoreBlacksWhitesHardCut`. The cached edge-weight array `tQa.Da`
was cleared before each measurement so each configuration was independent.
No application preference file, image, sidecar or license state was modified.

Every image was 32x16 `TYPE_INT_RGB`. Patterns were:

0. Solid black.
1. Solid white.
2. Solid RGB(0,128,255).
3. Grayscale `8*x`.
4. Grayscale 16 for `x<16`, otherwise 240.
5. RGB channels `(37*x+11*y+17)&255`, `(13*x+41*y+29)&255`,
   `(23*x+7*y+43)&255`.

Each pattern was measured in both soft and hard modes with black/white
percentages (0,0), (10,0), (0,10), (10,10), (25,40), (50,50), (90,90),
and (100,100). The Go tests compare to the native results with absolute
tolerance 1e-14. The inputs and observations are original test data; no
vendor executable code or photographs are included.
