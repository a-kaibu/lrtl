# Native crop-position observations

Recorded2026-10-20, Windows, installed LRTimelapse7.5.5 build1059/Java25.
An isolated ordinary renderer receives default RenderPreset, synthetic BT709,
one placeholder frame and sQa geometry. Its explicit position argument is
varied without GUI/export or license/development changes. Original scale/crop
fragments are stored; crop is post-scale width:height:x:y.

The288 cases cross six source sizes33x25,63x31,32x64,64x32,101x100,100x101;
resolutions0/32; aspects.5/1/1.7777778; positions0/1/25/33/50/67/99/100.
All288 rectangles match the independent Go planner using single-precision
excess*(position/100) and integer truncation. Position50 is central;0/100 use
opposite ends. Odd coordinates remain valid in the native444 YUV path.

Inspection of the settings model did not identify a persisted position field,
and the inspected batch-render setup passes literal50 to the render builder.
The implementation therefore does not invent a JSON field. Go exposes an
optional CropPosition pointer, with nil=center and explicit0=left/top; CLI
render accepts integer0..100 and batch forwards it. Native preset geometry
crops after scaling. Standalone CLI aspect cropping retains its prior ordering
before scaling and now uses the selected position. This is a deliberate CLI
extension of the existing geometry interface, not a saved-state parity claim.

Tests additionally verify native/standalone RGB48 pixel preparation at
positions0/33/50/100, Motion Blur order,16-bit/alpha/source-coordinate retention,
AVI JPEG pixels, batch/default preservation, failed output retention and input
retention. Actual H.264 MP4 decoding checks left/right reference ramp pixels.
These are separate Go pipeline tests, not outputs of the complete LRT encoder.
No RAW/Camera Raw/color-management or XMP crop-keyframe equivalence is claimed.
