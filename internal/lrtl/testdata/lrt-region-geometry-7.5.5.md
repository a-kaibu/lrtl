# Large virtual scale canvas observations

Recorded2026-10-20, Windows, installed LRTimelapse7.5.5 build1059/Java25. An
isolated ordinary renderer with null UI/default RenderPreset receives sQa
geometry, synthetic BT709, one placeholder filename, center50. Original native
scale and crop fragments are retained. No native export/GUI or license/gate
changes are involved. Four cases record:

| Source | Resolution | Aspect | Scale canvas | Final crop | Offset |
| --- | --- | --- | --- | --- | --- |
|32x64|384|16|6144x12288|6144x384|0,5952|
|6016x4016|4320|2.5|10800x7200|10800x4320|0,1440|
|6016x4016|1080|.0625|25920x17280|1080x17280|12420,0|
|32x512|4320|1|4320x69120|4320x4320|0,32400|

These plans were previously refused by Go's intermediate64MP/per-axis limit.
The new implementation computes only the final crop while keeping full-scale
pixel centers, kernel support, folded edges and normalization. It does not
crop input first. Full resize/copy and regional calculations match bitwise
for the tested8/16-bit/color-model/alpha/coordinate combinations. The original
24 independent FFmpeg scale-reference tests still check the full resampler.
Final output retains the32768-side/64MP limit; virtual canvases are not allocated.

Large-canvas tests use virtual bounded sources for small windows; sample reads
stay below20,000 per window. They validate native sizes/positions without a
multi-gigabyte bitmap. Separate CLI and actual MP4 tests render the6144x384
case from a constant16-bit32x64 PNG, inspect frame dimensions/count and decoded
pixels, and verify input retention. Optional FFmpeg runs its original large
scale/crop chain on a constant gray16le plane. These component/Go encoder tests
do not establish full native color-managed export parity.

On Windows/Go1.26.9, one benchmark iteration allocates21,709,096 bytes for the
6144x384 region. The full RGBA64 canvas alone would require603,979,776 bytes.
A64x32 window on the10800x7200 canvas allocates46,320 bytes. These are total
allocations of the resampling call, excluding input creation/decoding, XMP,
RGB48 conversion, encoding and process overhead. No portable memory/time
threshold is inferred from one machine. Reproduce with:

```powershell
go test ./internal/lrtl -run '^TestResizeRegion' -count=1 -v
go test ./internal/lrtl -run '^$' -bench '^BenchmarkResizeRegion' -benchtime=1x -benchmem
```
