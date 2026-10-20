# Native render geometry observations

Recorded2026-10-20 on Windows, LRTimelapse7.5.5 build1059, Java25. An independent
reflection probe constructs an isolated ordinary renderer with null UI and
sQa(width,height,resolution,aspect). It asks the default RenderPreset graph
builder for its filter string using synthetic BT709 labels and one placeholder
filename. It records original scale/crop fragments. No native image export,
GUI or license/development gate changes were involved.

480 cases cross:

- source32x24,33x25,63x31,32x64,64x32,1920x1080,6016x4016,4016x6016,
  400x401,401x400,101x100,100x101;
- resolution0,16,32,720,1080;
- aspect0,.5,1,1.5,1.7777778,2.5,.0625,16.

Aspect0 disables ratio constraint. Resolution0 chooses the original canvas.
Crop placement argument is50 (center). Orientation is the default1; odd-edge
orientation equivalence is tested independently in Go for all eight values.
Stored crop strings are width:height:x:y after scaling, not input coordinates.

Observed geometry uses float32 arithmetic and standard aspect snapping within
one percent for3,2.5,2,16/9,1.5,1,2/3,9/16,.5,.4. Aspect comparisons truncate
hundredths. Dimensions within four pixels of the current canvas revert to it;
width rounds down to a multiple of4 and height to a multiple of2. The normal
graph removes odd input edges before scale and crops to the target afterward.
These observations were used to design an independent Go planner, which
matches all480 scale sizes and crop rectangles. No native implementation is
included. Some observed extreme Original crops have zero width/height; Go
rejects them rather than producing an invalid file. Go's final-output limit
of32768 per side and64MP is an implementation limit, not a native behavior
claim. The former intermediate scale limit is removed by regional resampling;
see lrt-region-geometry-7.5.5.md for additional large-canvas observations.

Regular tests compare stored observations without Java or FFmpeg. Integration
tests separately verify raw RGB48 preparation with native recorded rectangles,
Motion Blur order, CLI preset/explicit/batch behavior, input retention and
failure retention. Optional actual MP4 tests inspect encoded dimensions using
FFmpeg/ffprobe. RGB/YUV rounding, RAW/Camera Raw development, arbitrary crop
position and the complete LRT encoder remain outside this comparison.
