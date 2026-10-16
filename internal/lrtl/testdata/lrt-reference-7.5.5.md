# LRT 7.5.5 reference-box observations

`lrt-reference-7.5.5.json` contains 64 original synthetic reference-box inputs
and outputs from installed LRTimelapse 7.5.5 build 1059, observed 2026-10-17.
It contains numeric observations, not vendor code or images.

The independent Java reflection probe initialized `tQa`, `Rsa` and `Nsa`.
It constructed a `Rsa(-1)` containing `tQa(Object[4096])` frames. For each
frame it installed a `Nsa(String, boolean)` using the saved-row format
`index;x1;y1;x2;y2`. Unmarked frames received `index;0;0;1;1` and false.
It invoked the zero-argument `Rsa.j()` returning void, then retrieved each
frame's `Nsa` and its `Float[]` coordinates. No GUI or license state was changed.

Inputs used Java `Random(755)`. Cases cover one-frame and one-anchor copying,
multiple anchors, nonuniform anchor positions, virtual first/last endpoints,
cubic overshoot and normalized-coordinate clipping. Output JSON preserves
Java's float decimal strings, which round-trip to the original float32 bits.

The Go test compares every output coordinate as float32, without a tolerance.
The native interpolation uses a separate natural cubic from Auto-Transition's
Bezier interpolation. These observations establish coordinate behavior;
they do not establish equivalence of image decoding, color management,
Deflicker metering or iterative exposure correction.
