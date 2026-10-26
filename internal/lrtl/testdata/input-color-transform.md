# Explicit RGB input-profile conversion

`render --input-color` accepts `adobe-rgb`, `display-p3`, and `prophoto-rgb`.
These values are explicit instructions for interpreting the encoded RGB samples;
they are not profile-name aliases and do not inspect the image's ICC profile.
They work for supported raster images and decoded RAW/LRT previews. The default
`auto` behavior remains unchanged: native output can infer BT.2020/PQ from the
first selected image's ICC description, while other paths retain their existing
input interpretation.

The explicit transforms use the standard transfer curves and RGB primaries for
the selected spaces, convert through CIE XYZ, and encode the result as sRGB.
ProPhoto RGB uses Bradford chromatic adaptation from D50 to D65. Out-of-sRGB
colors clip to the sRGB range. The conversion preserves 16-bit samples and
alpha. After conversion, the normal render pipeline treats the resulting sRGB
samples as BT.709 input; named-profile conversion cannot be combined with
BT.2020 output because the intermediate result is sRGB.

The transform runs before orientation, crop, XMP adjustments, deflicker,
spatial filters, and temporal blending. It routes frames through RGB48 and
disables the native YUV input path so those stages receive converted pixels.
Without an explicit profile option, pixels are not transformed based on Adobe
RGB, Display P3, or ProPhoto RGB labels.

This is a pure-Go convenience for three standard RGB spaces. It is not a general
ICC engine: arbitrary matrix/TRC profiles, LUT profiles, CMYK, custom transfer
curves, automatic profile parsing, RAW camera profiles, and HDR color
management remain unsupported. The transform is not an LRT pixel-parity claim;
the current reference values are standard color-space conversion samples, not
renders measured from the installed LRT application.

## Tests

- Unit tests check reference RGB values for each profile, non-zero image bounds,
  alpha preservation, clipping, and rejection of unknown profile names.
- CLI tests embed an ICC label that says Display P3, select each explicit input
  profile, and verify RGB48 samples from the fake FFmpeg input. They cover the
  Deflicker analysis/render route, Motion Blur temporal preparation, and the
  unchanged untransformed `auto` path.
- Run the focused tests with:

```powershell
go test ./internal/lrtl -run '^(TestICC|TestRenderInputProfile)' -count=1
```
