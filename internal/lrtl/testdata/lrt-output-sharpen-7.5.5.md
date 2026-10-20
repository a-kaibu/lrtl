# Output sharpening observations

Observed on 2026-10-20 on Windows, LRTimelapse 7.5.5 build 1059, Java 25.
`lrt-output-sharpen-7.5.5.json` contains four original observations from an
isolated renderer filter builder: a constructor-default RenderPreset with
`sharpen=false/true` and `videoRange=0/1`, synthetic BT709 source, 64x32 geometry
and 12 placeholder frame names. No native image export was performed and no
license/development state was changed. The off cases contain no unsharp
fragment; both on cases emit the identical 7x7 luma amount=0.4 fragment.

The normal native graph converts to yuv444p10le before unsharp, after resizing
and before overlays/fade. `lrt-unsharp-ffmpeg-8.0.1.json` is a separate pixel
observation: six synthetic 32x16 yuv444p10le frames passed through that fragment
using `C:\Program Files\LRTimelapse 7\ffmpeg.exe` (8.0.1 essentials gyan build).
This is the bundled filter component, not a complete LRT render. Both chroma
planes are constant 512 and remain unchanged. The JSON stores original input
and output luma samples, not values computed by the Go implementation.

Patterns use integer arithmetic, max=1023, background=max/3:

- constant: background throughout;
- center/corner: replace one pixel at (16,8)/(0,0) with 2*max/3;
- edge: x>=16 becomes 2*max/3;
- ramp: max*x/31;
- noise: ((13*x+23*y+7*x*y)%101)*max/100.

Every one of the 3072 stored luma samples matches the independently implemented
separable [1,6,15,20,15,6,1] convolution with replicated edges, rounded once
after both axes (add 2048, shift 12), followed by input +
((input-blur)*26214 >> 24), clamped to [0,1023]. The shift is arithmetic.
Separate exploratory 8/16-bit observations showed shift16 for 8-bit and shift24
for higher depth. They are not included as supported output modes. Widths below
32 and alternative FFmpeg versions are not claimed equivalent.

Normal tests compare Go with the stored observations without Java/FFmpeg.
When FFmpeg is on PATH, a separate optional test replays the synthetic planes
through its actual unsharp filter and checks luma and chroma. To select the
installed native bundle explicitly:

```powershell
$env:LRTL_SHARPEN_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestOutputSharpenActualFFmpegPlaneReference$' -count=1 -v
```

The runtime stays pure Go for this effect, including AVI. RGB48->10-bit BT.709
luma and additive RGB reconstruction are independent approximations; luma-plane
matches do not demonstrate whole-frame color-managed native video equivalence.
